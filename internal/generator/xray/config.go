package xray

import (
	"encoding/json"
	"fmt"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
)

const outboundTagPrefix = "egress"

// GeneratorError 表示 Xray 配置生成失败。
type GeneratorError struct {
	Message string
}

// Error 返回生成失败原因。
func (e GeneratorError) Error() string {
	return e.Message
}

// Config 是稳定 JSON 输出的顶层结构。
type Config struct {
	Log       LogConfig     `json:"log"`
	API       *APIConfig    `json:"api,omitempty"`
	Stats     *emptyObject  `json:"stats,omitempty"`
	Policy    *PolicyConfig `json:"policy,omitempty"`
	Inbounds  []any         `json:"inbounds"`
	Outbounds []any         `json:"outbounds"`
}

// LogConfig 保存 Xray 日志级别。
type LogConfig struct {
	LogLevel string `json:"loglevel"`
}

// APIConfig 保存 Xray API 输出配置。
type APIConfig struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen"`
	Services []string `json:"services"`
}

// PolicyConfig 保存 Xray policy 输出配置。
type PolicyConfig struct {
	Levels map[string]PolicyLevel `json:"levels,omitempty"`
	System PolicySystem           `json:"system"`
}

// PolicyLevel 保存单个 level 的用户统计开关。
type PolicyLevel struct {
	StatsUserUplink   *bool `json:"statsUserUplink,omitempty"`
	StatsUserDownlink *bool `json:"statsUserDownlink,omitempty"`
}

// PolicySystem 保存 system policy 的全局统计开关。
type PolicySystem struct {
	StatsInboundUplink    bool `json:"statsInboundUplink"`
	StatsInboundDownlink  bool `json:"statsInboundDownlink"`
	StatsOutboundUplink   bool `json:"statsOutboundUplink"`
	StatsOutboundDownlink bool `json:"statsOutboundDownlink"`
}

type emptyObject struct{}

type vmessInbound struct {
	Tag            string         `json:"tag"`
	Listen         string         `json:"listen"`
	Port           int            `json:"port"`
	Protocol       string         `json:"protocol"`
	Settings       vmessSettings  `json:"settings"`
	StreamSettings streamSettings `json:"streamSettings"`
}

type vmessSettings struct {
	Clients []vmessClient `json:"clients"`
}

type vmessClient struct {
	ID      string `json:"id"`
	AlterID int    `json:"alterId"`
	Email   string `json:"email"`
}

type streamSettings struct {
	Network string `json:"network"`
}

type shadowsocksInbound struct {
	Tag      string              `json:"tag"`
	Listen   string              `json:"listen"`
	Port     int                 `json:"port"`
	Protocol string              `json:"protocol"`
	Settings shadowsocksSettings `json:"settings"`
}

type shadowsocksSettings struct {
	Method   string              `json:"method"`
	Password string              `json:"password"`
	Network  string              `json:"network"`
	Clients  []shadowsocksClient `json:"clients,omitempty"`
}

type shadowsocksClient struct {
	Password string `json:"password"`
	Email    string `json:"email"`
	Method   string `json:"method,omitempty"`
}

type socksInbound struct {
	Tag      string        `json:"tag"`
	Listen   string        `json:"listen"`
	Port     int           `json:"port"`
	Protocol string        `json:"protocol"`
	Settings socksSettings `json:"settings"`
}

type socksSettings struct {
	Auth     string    `json:"auth"`
	UDP      bool      `json:"udp"`
	Accounts []account `json:"accounts,omitempty"`
}

type httpInbound struct {
	Tag      string       `json:"tag"`
	Listen   string       `json:"listen"`
	Port     int          `json:"port"`
	Protocol string       `json:"protocol"`
	Settings httpSettings `json:"settings"`
}

type httpSettings struct {
	Accounts []account `json:"accounts,omitempty"`
}

type account struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

type proxyOutbound struct {
	Tag      string                `json:"tag"`
	Protocol string                `json:"protocol"`
	Settings proxyOutboundSettings `json:"settings"`
}

type proxyOutboundSettings struct {
	Servers []proxyServer `json:"servers"`
}

type proxyServer struct {
	Address string    `json:"address"`
	Port    int       `json:"port"`
	Users   []account `json:"users,omitempty"`
}

type directOutbound struct {
	Tag      string      `json:"tag"`
	Protocol string      `json:"protocol"`
	Settings emptyObject `json:"settings"`
}

// RenderConfig 生成指定启用 stack 的 Xray 配置结构。
func RenderConfig(stackSet domain.StackSet, stackName string) (Config, error) {
	stack, err := enabledXrelayStack(stackSet, stackName)
	if err != nil {
		return Config{}, err
	}
	defaults := stackSet.Config.Defaults.Xrelay
	apiConfig := domain.ResolveXrelayAPIConfig(defaults, stack.Xrelay)
	statsConfig := domain.ResolveXrelayStatsConfig(defaults, stack.Xrelay)
	policyConfig := domain.ResolveXrelayPolicyConfig(defaults, stack.Xrelay)
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		Log: LogConfig{LogLevel: domain.ResolveXrelayLogLevel(defaults, stack.Xrelay)},
	}
	if apiConfig.Enabled {
		config.API = RenderAPI(apiConfig)
	}
	if statsConfig.Enabled {
		config.Stats = &emptyObject{}
	}
	if policyConfig.Enabled || statsConfig.Enabled {
		config.Policy = RenderPolicy(policyConfig, statsConfig.Enabled)
	}
	config.Inbounds = make([]any, 0, len(stack.Xrelay.Inbounds))
	for _, inbound := range stack.Xrelay.Inbounds {
		rendered, err := RenderInbound(inbound)
		if err != nil {
			return Config{}, err
		}
		config.Inbounds = append(config.Inbounds, rendered)
	}
	outbound, err := RenderOutbound(stack.Xrelay.Outbound, referenceGraph, stack.Name)
	if err != nil {
		return Config{}, err
	}
	config.Outbounds = []any{outbound}
	return config, nil
}

// DumpsConfig 将指定 stack 的 Xray 配置编码为稳定、可读的 JSON 文本。
func DumpsConfig(stackSet domain.StackSet, stackName string) (string, error) {
	config, err := RenderConfig(stackSet, stackName)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// RenderAPI 生成 Xray API 配置。
func RenderAPI(apiConfig domain.XrelayAPIConfig) *APIConfig {
	return &APIConfig{
		Tag:      apiConfig.Tag,
		Listen:   apiConfig.Listen,
		Services: append([]string(nil), apiConfig.Services...),
	}
}

// RenderPolicy 生成 Xray policy 配置，stats 开启时默认启用全局统计项。
func RenderPolicy(policyConfig domain.XrelayPolicyConfig, statsEnabled bool) *PolicyConfig {
	policy := &PolicyConfig{
		Levels: RenderPolicyLevels(policyConfig.Levels),
		System: RenderPolicySystem(policyConfig.System, statsEnabled),
	}
	return policy
}

// RenderPolicyLevels 生成 policy levels 的用户流量统计开关。
func RenderPolicyLevels(levelsConfig map[string]domain.XrelayPolicyLevelConfig) map[string]PolicyLevel {
	levels := make(map[string]PolicyLevel)
	for level, levelConfig := range levelsConfig {
		rendered := PolicyLevel{
			StatsUserUplink:   levelConfig.StatsUserUplink,
			StatsUserDownlink: levelConfig.StatsUserDownlink,
		}
		if rendered.StatsUserUplink != nil || rendered.StatsUserDownlink != nil {
			levels[level] = rendered
		}
	}
	if len(levels) == 0 {
		return nil
	}
	return levels
}

// RenderPolicySystem 生成 system policy 的四个全局流量统计开关。
func RenderPolicySystem(systemConfig domain.XrelayPolicySystemConfig, statsEnabled bool) PolicySystem {
	return PolicySystem{
		StatsInboundUplink:    boolOrDefault(systemConfig.StatsInboundUplink, statsEnabled),
		StatsInboundDownlink:  boolOrDefault(systemConfig.StatsInboundDownlink, statsEnabled),
		StatsOutboundUplink:   boolOrDefault(systemConfig.StatsOutboundUplink, statsEnabled),
		StatsOutboundDownlink: boolOrDefault(systemConfig.StatsOutboundDownlink, statsEnabled),
	}
}

// RenderInbound 按 inbound 协议分发生成 Xray inbound 配置。
func RenderInbound(inbound domain.Inbound) (any, error) {
	switch inbound.Protocol {
	case "vmess":
		return renderVmessInbound(inbound), nil
	case "shadowsocks":
		return renderShadowsocksInbound(inbound), nil
	case "socks5":
		return renderSocksInbound(inbound)
	case "http":
		return renderHTTPInbound(inbound)
	default:
		return nil, GeneratorError{Message: "unsupported xray inbound protocol: " + inbound.Protocol}
	}
}

// RenderOutbound 按 xrelay outbound 类型生成 Xray outbound 配置。
func RenderOutbound(outbound domain.XrelayOutbound, referenceGraph graph.ReferenceGraph, stackName string) (any, error) {
	outboundTag := OutboundTag(stackName)
	switch outbound.Type {
	case "clash":
		return renderClashOutbound(outbound, referenceGraph, outboundTag)
	case "socks5":
		return renderProxyOutbound("socks", outbound.Server, outbound.Port, outbound.Username, outbound.Password, outboundTag)
	case "http":
		return renderProxyOutbound("http", outbound.Server, outbound.Port, outbound.Username, outbound.Password, outboundTag)
	case "direct":
		return directOutbound{Tag: outboundTag, Protocol: "freedom", Settings: emptyObject{}}, nil
	default:
		return nil, GeneratorError{Message: "unsupported xray outbound type: " + outbound.Type}
	}
}

// OutboundTag 生成包含 stack 名的 Xray 出口 tag。
func OutboundTag(stackName string) string {
	return outboundTagPrefix + "-" + stackName
}

// NormalizeInternalEndpointAddress 把本机 wildcard listener 地址归一为 loopback。
func NormalizeInternalEndpointAddress(address string) string {
	return domain.NormalizeInternalEndpointAddress(address)
}

func enabledXrelayStack(stackSet domain.StackSet, stackName string) (domain.Stack, error) {
	stack, ok := stackSet.ByName()[stackName]
	if !ok {
		return domain.Stack{}, GeneratorError{Message: "stack does not exist: " + stackName}
	}
	if !stack.Enabled {
		return domain.Stack{}, GeneratorError{Message: "stack is disabled: " + stackName}
	}
	if !stack.Xrelay.Enabled {
		return domain.Stack{}, GeneratorError{Message: "xrelay is disabled: " + stackName}
	}
	return *stack, nil
}

func renderVmessInbound(inbound domain.Inbound) vmessInbound {
	clients := make([]vmessClient, 0, len(inbound.Users))
	for _, user := range inbound.Users {
		clients = append(clients, vmessClient{ID: user.UUID, AlterID: 0, Email: user.EmailOrUser()})
	}
	return vmessInbound{
		Tag:      inbound.TagOrDefault(),
		Listen:   inbound.Listen,
		Port:     inbound.Port,
		Protocol: "vmess",
		Settings: vmessSettings{
			Clients: clients,
		},
		StreamSettings: streamSettings{
			Network: inbound.Network,
		},
	}
}

func renderShadowsocksInbound(inbound domain.Inbound) shadowsocksInbound {
	network := "tcp"
	if inbound.UDP {
		network = "tcp,udp"
	}
	settings := shadowsocksSettings{
		Method:   inbound.MethodOrCipher(),
		Password: inbound.Password,
		Network:  network,
	}
	if len(inbound.Users) > 0 {
		settings.Clients = make([]shadowsocksClient, 0, len(inbound.Users))
		for _, user := range inbound.Users {
			client := shadowsocksClient{
				Password: user.Password,
				Email:    user.EmailOrUser(),
			}
			if !domain.IsShadowsocks2022Method(inbound.MethodOrCipher()) {
				client.Method = firstNonEmpty(user.Method, user.Cipher, inbound.MethodOrCipher())
			}
			settings.Clients = append(settings.Clients, client)
		}
	}
	return shadowsocksInbound{
		Tag:      inbound.TagOrDefault(),
		Listen:   inbound.Listen,
		Port:     inbound.Port,
		Protocol: "shadowsocks",
		Settings: settings,
	}
}

func renderSocksInbound(inbound domain.Inbound) (socksInbound, error) {
	authType := inboundAuthType(inbound)
	settings := socksSettings{Auth: authType, UDP: inbound.UDP}
	if authType == "password" {
		acct, err := inboundAccount(inbound)
		if err != nil {
			return socksInbound{}, err
		}
		settings.Accounts = []account{acct}
	}
	return socksInbound{
		Tag:      inbound.TagOrDefault(),
		Listen:   inbound.Listen,
		Port:     inbound.Port,
		Protocol: "socks",
		Settings: settings,
	}, nil
}

func renderHTTPInbound(inbound domain.Inbound) (httpInbound, error) {
	settings := httpSettings{}
	if inboundAuthType(inbound) == "password" {
		acct, err := inboundAccount(inbound)
		if err != nil {
			return httpInbound{}, err
		}
		settings.Accounts = []account{acct}
	}
	return httpInbound{
		Tag:      inbound.TagOrDefault(),
		Listen:   inbound.Listen,
		Port:     inbound.Port,
		Protocol: "http",
		Settings: settings,
	}, nil
}

func renderClashOutbound(outbound domain.XrelayOutbound, referenceGraph graph.ReferenceGraph, outboundTag string) (proxyOutbound, error) {
	endpoint, ok := referenceGraph.Index.ResolveClashListener(outbound.Ref)
	if !ok {
		return proxyOutbound{}, GeneratorError{Message: "clash listener ref does not exist: " + outbound.Ref}
	}
	if endpoint.Kind != "socks" {
		return proxyOutbound{}, GeneratorError{Message: "clash listener ref must target socks listener: " + outbound.Ref}
	}
	username, password := firstEndpointUser(endpoint)
	return renderProxyOutbound("socks", domain.NormalizeInternalEndpointAddress(endpoint.Listen), endpoint.Port, username, password, outboundTag)
}

func renderProxyOutbound(protocol string, address string, port int, username string, password string, outboundTag string) (proxyOutbound, error) {
	if address == "" || port == 0 {
		return proxyOutbound{}, GeneratorError{Message: fmt.Sprintf("%s outbound requires address and port", protocol)}
	}
	server := proxyServer{Address: address, Port: port}
	if username != "" || password != "" {
		if username == "" || password == "" {
			return proxyOutbound{}, GeneratorError{Message: fmt.Sprintf("%s outbound username and password must be provided together", protocol)}
		}
		server.Users = []account{{User: username, Pass: password}}
	}
	return proxyOutbound{
		Tag:      outboundTag,
		Protocol: protocol,
		Settings: proxyOutboundSettings{
			Servers: []proxyServer{server},
		},
	}, nil
}

func inboundAuthType(inbound domain.Inbound) string {
	if inbound.Auth == nil {
		return "noauth"
	}
	return inbound.Auth.Type
}

func inboundAccount(inbound domain.Inbound) (account, error) {
	if inbound.Auth == nil || inbound.Auth.Username == "" || inbound.Auth.Password == "" {
		return account{}, GeneratorError{Message: "password auth account is incomplete: " + inbound.Name}
	}
	return account{User: inbound.Auth.Username, Pass: inbound.Auth.Password}, nil
}

func firstEndpointUser(endpoint graph.Endpoint) (string, string) {
	if len(endpoint.Users) == 0 {
		return "", ""
	}
	return endpoint.Users[0].Username, endpoint.Users[0].Password
}

func boolOrDefault(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
