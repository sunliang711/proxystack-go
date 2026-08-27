package xray

import (
	"encoding/json"

	"github.com/eagle/proxystack-go/internal/domain"
)

// SupportsUserToggle 判断某 inbound 协议是否支持按用户启停。
//
// 只有 vmess 和 shadowsocks 会渲染成多用户 clients 列表，也只有它们能被
// Xray HandlerService 的 adu/rmu 操作。socks5 和 http 是单账号 inbound，
// 把账号摘掉会退化成免认证入口，因此一律不参与启停。
func SupportsUserToggle(protocol string) bool {
	return protocol == "vmess" || protocol == "shadowsocks"
}

// enabledInboundUsers 返回过滤掉禁用用户后的 inbound 用户列表。
func enabledInboundUsers(inbound domain.Inbound, disabled domain.DisabledUserSet, stackName string) []domain.InboundUser {
	if !SupportsUserToggle(inbound.Protocol) || len(disabled) == 0 {
		return inbound.Users
	}
	users := make([]domain.InboundUser, 0, len(inbound.Users))
	for _, user := range inbound.Users {
		if disabled.Has(stackName, user.User) {
			continue
		}
		users = append(users, user)
	}
	return users
}

// applyDisabledUsers 过滤 inbound 上被禁用的用户；全部被禁用时报错而不是生成空 inbound。
//
// 空的 clients 列表会被 Xray 直接拒绝（vmess），或让 shadowsocks 退回到 inbound
// 级共享密码，两种都不是“禁用一个用户”该有的结果，所以这里 fail loud。
func applyDisabledUsers(inbound domain.Inbound, disabled domain.DisabledUserSet, stackName string) (domain.Inbound, error) {
	if !SupportsUserToggle(inbound.Protocol) || len(disabled) == 0 || len(inbound.Users) == 0 {
		return inbound, nil
	}
	users := enabledInboundUsers(inbound, disabled, stackName)
	if len(users) == len(inbound.Users) {
		return inbound, nil
	}
	if len(users) == 0 {
		return domain.Inbound{}, GeneratorError{Message: "all users of inbound are disabled: " + stackName + "/" + inbound.TagOrDefault() + " (edit runtime/disabled.json or disable the inbound in the stack file)"}
	}
	inbound.Users = users
	return inbound, nil
}

// DumpsInboundUserPatch 渲染只含单个用户的 inbound 片段，供 xray api adu 使用。
//
// 复用主生成路径的 inbound 渲染，保证热添加的用户和写盘配置里的形状一致。
func DumpsInboundUserPatch(inbound domain.Inbound, user domain.InboundUser) (string, error) {
	if !SupportsUserToggle(inbound.Protocol) {
		return "", GeneratorError{Message: "inbound protocol does not support user toggle: " + inbound.Protocol}
	}
	inbound.Users = []domain.InboundUser{user}
	rendered, err := RenderInbound(inbound)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(map[string]any{"inbounds": []any{rendered}}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
