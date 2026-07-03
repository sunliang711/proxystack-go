package graph

import (
	"fmt"
	"strings"

	"github.com/eagle/proxystack-go/internal/domain"
)

// RefFormatError 表示 ref 基础格式错误，保留字段路径便于聚合展示。
type RefFormatError struct {
	Path    string
	Message string
}

// Error 输出包含字段路径的错误文本。
func (e RefFormatError) Error() string {
	return e.Path + ": " + e.Message
}

// ParsedRef 是结构化 ref，避免业务逻辑反复 split 字符串。
type ParsedRef struct {
	Raw       string
	Stack     string
	Component string
	Kind      string
	Name      string
}

// ParseXrayInboundRef 解析 <stack>.<inbound_name> 形式的 xray inbound ref。
func ParseXrayInboundRef(value string, path string) (ParsedRef, error) {
	parts, err := splitRef(value, 2, path)
	if err != nil {
		return ParsedRef{}, err
	}
	return ParsedRef{Raw: value, Stack: parts[0], Component: "xray", Name: parts[1]}, nil
}

// ParseComponentRef 解析 <stack>.<component>.<kind> 形式的组件 ref。
func ParseComponentRef(value string, path string) (ParsedRef, error) {
	parts, err := splitRef(value, 3, path)
	if err != nil {
		return ParsedRef{}, err
	}
	return ParsedRef{Raw: value, Stack: parts[0], Component: parts[1], Kind: parts[2]}, nil
}

// Endpoint 表示可被 ref 指向的本地 endpoint。
type Endpoint struct {
	Ref       string
	Stack     string
	Component string
	Kind      string
	Name      string
	Listen    string
	Port      int
	Path      string
	Users     []EndpointUser
}

// EndpointUser 表示 clash listener 可复用的认证用户。
type EndpointUser struct {
	Username string
	Password string
}

// ReferenceIndex 是跨 stack endpoint 索引，供校验、生成器和 check 复用。
type ReferenceIndex struct {
	XrayInbounds map[string]Endpoint
	ClashListeners map[string]Endpoint
}

// NewReferenceIndex 从所有启用的 stack 组件中建立 endpoint 索引。
func NewReferenceIndex(stacks []domain.Stack) ReferenceIndex {
	index := ReferenceIndex{
		XrayInbounds: make(map[string]Endpoint),
		ClashListeners: make(map[string]Endpoint),
	}
	for _, stack := range stacks {
		if !stack.Enabled {
			continue
		}
		if stack.Xray.Enabled {
			for ref, endpoint := range IndexXrayInbounds(stack) {
				index.XrayInbounds[ref] = endpoint
			}
		}
		if stack.Clash.Enabled {
			for ref, endpoint := range IndexClashListeners(stack) {
				index.ClashListeners[ref] = endpoint
			}
		}
	}
	return index
}

// ResolveXrayInbound 按两段 ref 查询 xray inbound endpoint。
func (r ReferenceIndex) ResolveXrayInbound(ref string) (Endpoint, bool) {
	endpoint, ok := r.XrayInbounds[ref]
	return endpoint, ok
}

// ResolveClashListener 按三段 ref 查询 clash listener endpoint。
func (r ReferenceIndex) ResolveClashListener(ref string) (Endpoint, bool) {
	endpoint, ok := r.ClashListeners[ref]
	return endpoint, ok
}

// IndexXrayInbounds 建立单个 stack 的 xray inbound 两段 ref 索引。
func IndexXrayInbounds(stack domain.Stack) map[string]Endpoint {
	endpoints := make(map[string]Endpoint, len(stack.Xray.Inbounds))
	for index, inbound := range stack.Xray.Inbounds {
		ref := stack.Name + "." + inbound.Name
		endpoints[ref] = Endpoint{
			Ref:       ref,
			Stack:     stack.Name,
			Component: "xray",
			Kind:      inbound.Protocol,
			Name:      inbound.Name,
			Listen:    inbound.Listen,
			Port:      inbound.Port,
			Path:      fmt.Sprintf("stacks.%s.xray.inbounds[%d]", stack.Name, index),
		}
	}
	return endpoints
}

// IndexClashListeners 建立单个 stack 的 clash listener 三段 ref 索引。
func IndexClashListeners(stack domain.Stack) map[string]Endpoint {
	endpoints := make(map[string]Endpoint, len(stack.Clash.Listeners.Socks)+len(stack.Clash.Listeners.HTTP))
	for index, listener := range stack.Clash.Listeners.Socks {
		ref := stack.Name + ".clash.socks"
		endpoints[ref] = Endpoint{
			Ref:       ref,
			Stack:     stack.Name,
			Component: "clash",
			Kind:      "socks",
			Name:      listener.Name,
			Listen:    listener.Listen,
			Port:      listener.Port,
			Path:      fmt.Sprintf("stacks.%s.clash.listeners.socks[%d]", stack.Name, index),
			Users:     endpointUsers(listener.Users),
		}
	}
	for index, listener := range stack.Clash.Listeners.HTTP {
		ref := stack.Name + ".clash.http"
		endpoints[ref] = Endpoint{
			Ref:       ref,
			Stack:     stack.Name,
			Component: "clash",
			Kind:      "http",
			Name:      listener.Name,
			Listen:    listener.Listen,
			Port:      listener.Port,
			Path:      fmt.Sprintf("stacks.%s.clash.listeners.http[%d]", stack.Name, index),
			Users:     endpointUsers(listener.Users),
		}
	}
	return endpoints
}

func splitRef(value string, segmentCount int, path string) ([]string, error) {
	if value == "" {
		return nil, RefFormatError{Path: path, Message: "ref is required"}
	}
	parts := strings.Split(value, ".")
	if len(parts) != segmentCount {
		return nil, RefFormatError{Path: path, Message: fmt.Sprintf("ref must contain %d dot-separated segments", segmentCount)}
	}
	for _, part := range parts {
		if part == "" {
			return nil, RefFormatError{Path: path, Message: "ref must not contain empty segments"}
		}
	}
	return parts, nil
}

func endpointUsers(users []domain.ClashListenerUser) []EndpointUser {
	if len(users) == 0 {
		return nil
	}
	endpointUsers := make([]EndpointUser, 0, len(users))
	for _, user := range users {
		endpointUsers = append(endpointUsers, EndpointUser{Username: user.Username, Password: user.Password})
	}
	return endpointUsers
}
