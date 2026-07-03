package agentconfig

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

// stackSnippetFiles 保存 psctl example 和内置 stack 模板共享的 YAML 片段。
//
//go:embed templates/snippets
var stackSnippetFiles embed.FS

// stackSnippetDefinition 描述一个可被 example 输出或 stack 模板 include 的配置片段。
type stackSnippetDefinition struct {
	ID          string
	Area        string
	Section     string
	Type        string
	Description string
	Path        string
}

// stackSnippetContext 是片段渲染时允许替换的少量上下文。
type stackSnippetContext struct {
	StackName           string
	XrayAPIPort         int
	RelayPort           int
	RelayUsername       string
	RelayRemark         string
	MemberName          string
	MemberInboundName   string
	GroupProxies        string
	SelectProxies       string
	SocksListenerPort   int
	HTTPListenerPort    int
	ClashControllerPort int
}

var stackSnippetDefinitions = []stackSnippetDefinition{
	{ID: "config.users.default", Area: "config", Section: "users", Type: "default", Description: "config.yaml 全局订阅用户档案", Path: "templates/snippets/config/users/default.yaml.tmpl"},

	{ID: "stack.role.edge", Area: "stack", Section: "role", Type: "edge", Description: "普通边缘代理 stack 角色", Path: "templates/snippets/stack/role/edge.yaml.tmpl"},
	{ID: "stack.role.auto", Area: "stack", Section: "role", Type: "auto", Description: "聚合或自动选择节点 stack 角色", Path: "templates/snippets/stack/role/auto.yaml.tmpl"},

	{ID: "xray.api.default", Area: "xray", Section: "api", Type: "default", Description: "Xray API 配置片段，只允许监听 loopback 地址", Path: "templates/snippets/xray/api/default.yaml.tmpl"},
	{ID: "xray.stats.enabled", Area: "xray", Section: "stats", Type: "enabled", Description: "Xray stats 开关片段", Path: "templates/snippets/xray/stats/enabled.yaml.tmpl"},
	{ID: "xray.policy.enabled", Area: "xray", Section: "policy", Type: "enabled", Description: "Xray policy 开关片段", Path: "templates/snippets/xray/policy/enabled.yaml.tmpl"},
	{ID: "xray.loglevel.debug", Area: "xray", Section: "loglevel", Type: "debug", Description: "Xray debug 日志级别", Path: "templates/snippets/xray/loglevel/debug.yaml.tmpl"},
	{ID: "xray.loglevel.info", Area: "xray", Section: "loglevel", Type: "info", Description: "Xray info 日志级别", Path: "templates/snippets/xray/loglevel/info.yaml.tmpl"},
	{ID: "xray.loglevel.warning", Area: "xray", Section: "loglevel", Type: "warning", Description: "Xray warning 日志级别", Path: "templates/snippets/xray/loglevel/warning.yaml.tmpl"},
	{ID: "xray.loglevel.error", Area: "xray", Section: "loglevel", Type: "error", Description: "Xray error 日志级别", Path: "templates/snippets/xray/loglevel/error.yaml.tmpl"},
	{ID: "xray.loglevel.none", Area: "xray", Section: "loglevel", Type: "none", Description: "Xray none 日志级别", Path: "templates/snippets/xray/loglevel/none.yaml.tmpl"},
	{ID: "xray.auth.noauth", Area: "xray", Section: "auth", Type: "noauth", Description: "socks/http inbound 本地无认证片段", Path: "templates/snippets/xray/auth/noauth.yaml.tmpl"},
	{ID: "xray.auth.password", Area: "xray", Section: "auth", Type: "password", Description: "socks/http inbound 用户名密码认证片段", Path: "templates/snippets/xray/auth/password.yaml.tmpl"},

	{ID: "xray.inbound.vmess-raw", Area: "xray", Section: "inbound", Type: "vmess-raw", Description: "vmess raw inbound，适合对外发布到订阅", Path: "templates/snippets/xray/inbound/vmess-raw.yaml.tmpl"},
	{ID: "xray.inbound.vmess-websocket", Area: "xray", Section: "inbound", Type: "vmess-websocket", Description: "vmess WebSocket inbound", Path: "templates/snippets/xray/inbound/vmess-websocket.yaml.tmpl"},
	{ID: "xray.inbound.vmess-grpc", Area: "xray", Section: "inbound", Type: "vmess-grpc", Description: "vmess gRPC inbound", Path: "templates/snippets/xray/inbound/vmess-grpc.yaml.tmpl"},
	{ID: "xray.inbound.shadowsocks", Area: "xray", Section: "inbound", Type: "shadowsocks", Description: "shadowsocks inbound，示例使用全局 users 和 user_refs", Path: "templates/snippets/xray/inbound/shadowsocks.yaml.tmpl"},
	{ID: "xray.inbound.socks5", Area: "xray", Section: "inbound", Type: "socks5", Description: "socks5 inbound，公开发布时必须使用 password 认证", Path: "templates/snippets/xray/inbound/socks5.yaml.tmpl"},
	{ID: "xray.inbound.socks5-local", Area: "xray", Section: "inbound", Type: "socks5-local", Description: "仅本机可访问的 socks5 relay inbound", Path: "templates/snippets/xray/inbound/socks5-local.yaml.tmpl"},
	{ID: "xray.inbound.http", Area: "xray", Section: "inbound", Type: "http", Description: "HTTP inbound，公开发布时必须使用 password 认证", Path: "templates/snippets/xray/inbound/http.yaml.tmpl"},

	{ID: "xray.outbound.clash", Area: "xray", Section: "outbound", Type: "clash", Description: "转发到本 stack 或其他 stack 的 Clash socks listener", Path: "templates/snippets/xray/outbound/clash.yaml.tmpl"},
	{ID: "xray.outbound.socks5", Area: "xray", Section: "outbound", Type: "socks5", Description: "转发到外部 SOCKS5 上游", Path: "templates/snippets/xray/outbound/socks5.yaml.tmpl"},
	{ID: "xray.outbound.http", Area: "xray", Section: "outbound", Type: "http", Description: "转发到外部 HTTP 上游", Path: "templates/snippets/xray/outbound/http.yaml.tmpl"},
	{ID: "xray.outbound.direct", Area: "xray", Section: "outbound", Type: "direct", Description: "直连出站", Path: "templates/snippets/xray/outbound/direct.yaml.tmpl"},

	{ID: "clash.mode.rule", Area: "clash", Section: "mode", Type: "rule", Description: "mihomo Rule 运行模式", Path: "templates/snippets/clash/mode/rule.yaml.tmpl"},
	{ID: "clash.mode.global", Area: "clash", Section: "mode", Type: "global", Description: "mihomo Global 运行模式", Path: "templates/snippets/clash/mode/global.yaml.tmpl"},
	{ID: "clash.mode.direct", Area: "clash", Section: "mode", Type: "direct", Description: "mihomo Direct 运行模式", Path: "templates/snippets/clash/mode/direct.yaml.tmpl"},
	{ID: "clash.loglevel.debug", Area: "clash", Section: "loglevel", Type: "debug", Description: "mihomo debug 日志级别", Path: "templates/snippets/clash/loglevel/debug.yaml.tmpl"},
	{ID: "clash.loglevel.info", Area: "clash", Section: "loglevel", Type: "info", Description: "mihomo info 日志级别", Path: "templates/snippets/clash/loglevel/info.yaml.tmpl"},
	{ID: "clash.loglevel.warning", Area: "clash", Section: "loglevel", Type: "warning", Description: "mihomo warning 日志级别", Path: "templates/snippets/clash/loglevel/warning.yaml.tmpl"},
	{ID: "clash.loglevel.error", Area: "clash", Section: "loglevel", Type: "error", Description: "mihomo error 日志级别", Path: "templates/snippets/clash/loglevel/error.yaml.tmpl"},
	{ID: "clash.loglevel.silent", Area: "clash", Section: "loglevel", Type: "silent", Description: "mihomo silent 日志级别", Path: "templates/snippets/clash/loglevel/silent.yaml.tmpl"},
	{ID: "clash.controller.default", Area: "clash", Section: "controller", Type: "default", Description: "mihomo external-controller 配置片段", Path: "templates/snippets/clash/controller/default.yaml.tmpl"},
	{ID: "clash.listener.socks", Area: "clash", Section: "listener", Type: "socks", Description: "Clash socks listener 片段", Path: "templates/snippets/clash/listener/socks.yaml.tmpl"},
	{ID: "clash.listener.http", Area: "clash", Section: "listener", Type: "http", Description: "Clash HTTP listener 片段", Path: "templates/snippets/clash/listener/http.yaml.tmpl"},

	{ID: "clash.upstream.xray-socks5", Area: "clash", Section: "upstream", Type: "xray-socks5", Description: "引用其他 stack 的 xray socks5 inbound", Path: "templates/snippets/clash/upstream/xray-socks5.yaml.tmpl"},
	{ID: "clash.upstream.vmess-raw", Area: "clash", Section: "upstream", Type: "vmess-raw", Description: "mihomo 原生 vmess raw proxy 片段", Path: "templates/snippets/clash/upstream/vmess-raw.yaml.tmpl"},
	{ID: "clash.upstream.vmess-websocket", Area: "clash", Section: "upstream", Type: "vmess-websocket", Description: "mihomo 原生 vmess WebSocket proxy 片段", Path: "templates/snippets/clash/upstream/vmess-websocket.yaml.tmpl"},
	{ID: "clash.upstream.vmess-grpc", Area: "clash", Section: "upstream", Type: "vmess-grpc", Description: "mihomo 原生 vmess gRPC proxy 片段", Path: "templates/snippets/clash/upstream/vmess-grpc.yaml.tmpl"},
	{ID: "clash.upstream.vless-websocket", Area: "clash", Section: "upstream", Type: "vless-websocket", Description: "mihomo 原生 vless WebSocket proxy 片段", Path: "templates/snippets/clash/upstream/vless-websocket.yaml.tmpl"},
	{ID: "clash.upstream.raw", Area: "clash", Section: "upstream", Type: "raw", Description: "mihomo 原生 proxy 透传片段，示例使用 vmess WebSocket", Path: "templates/snippets/clash/upstream/vmess-websocket.yaml.tmpl"},
	{ID: "clash.upstream.raw-shadowsocks", Area: "clash", Section: "upstream", Type: "raw-shadowsocks", Description: "mihomo 原生 shadowsocks proxy 片段", Path: "templates/snippets/clash/upstream/shadowsocks.yaml.tmpl"},
	{ID: "clash.upstream.raw-socks5", Area: "clash", Section: "upstream", Type: "raw-socks5", Description: "mihomo 原生 socks5 proxy 片段", Path: "templates/snippets/clash/upstream/socks5.yaml.tmpl"},
	{ID: "clash.upstream.raw-http", Area: "clash", Section: "upstream", Type: "raw-http", Description: "mihomo 原生 HTTP proxy 片段", Path: "templates/snippets/clash/upstream/http.yaml.tmpl"},

	{ID: "clash.group.select", Area: "clash", Section: "group", Type: "select", Description: "手动选择代理组", Path: "templates/snippets/clash/group/select.yaml.tmpl"},
	{ID: "clash.group.url-test", Area: "clash", Section: "group", Type: "url-test", Description: "按延迟自动选择代理组", Path: "templates/snippets/clash/group/url-test.yaml.tmpl"},
	{ID: "clash.group.load-balance", Area: "clash", Section: "group", Type: "load-balance", Description: "负载均衡代理组", Path: "templates/snippets/clash/group/load-balance.yaml.tmpl"},
	{ID: "clash.group.fallback", Area: "clash", Section: "group", Type: "fallback", Description: "故障切换代理组", Path: "templates/snippets/clash/group/fallback.yaml.tmpl"},
	{ID: "clash.rules.default", Area: "clash", Section: "rules", Type: "default", Description: "默认规则 profile 和额外规则片段", Path: "templates/snippets/clash/rules/default.yaml.tmpl"},
}

// stackSnippetExamples 渲染 example 命令可输出的全部片段。
func stackSnippetExamples() ([]StackExampleSnippet, error) {
	snippets := make([]StackExampleSnippet, 0, len(stackSnippetDefinitions))
	context := defaultStackSnippetContext()
	for _, definition := range stackSnippetDefinitions {
		content, err := renderStackSnippet(definition.ID, context)
		if err != nil {
			return nil, err
		}
		snippets = append(snippets, StackExampleSnippet{
			Area:        definition.Area,
			Section:     definition.Section,
			Type:        definition.Type,
			Description: definition.Description,
			Content:     content,
		})
	}
	return snippets, nil
}

// renderStackSnippet 按片段 ID 渲染单个共享 YAML 片段。
func renderStackSnippet(id string, context stackSnippetContext) (string, error) {
	definition, ok := stackSnippetDefinitionByID(id)
	if !ok {
		return "", fmt.Errorf("unknown stack snippet: %s", id)
	}
	return renderStackSnippetFile(definition.Path, id, context)
}

// renderStackTemplate 展开内置 stack 模板中的 stackSnippet include 指令。
func renderStackTemplate(templateName string, source []byte, context stackSnippetContext) ([]byte, error) {
	funcs := template.FuncMap{
		"stackSnippet": func(id string, indent int) (string, error) {
			content, err := renderStackSnippet(id, context)
			if err != nil {
				return "", err
			}
			return indentSnippet(content, indent), nil
		},
		"stackMemberSnippet": func(id string, memberName string, indent int) (string, error) {
			memberContext := context
			memberContext.MemberName = memberName
			content, err := renderStackSnippet(id, memberContext)
			if err != nil {
				return "", err
			}
			return indentSnippet(content, indent), nil
		},
	}
	tpl, err := template.New("stack." + templateName).Option("missingkey=error").Funcs(funcs).Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("stack template could not be parsed: %s (%w)", templateName, err)
	}
	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, context); err != nil {
		return nil, fmt.Errorf("stack template could not be rendered: %s (%w)", templateName, err)
	}
	return buffer.Bytes(), nil
}

// renderStackSnippetFile 读取并渲染片段文件，允许片段中使用 StackName 等上下文变量。
func renderStackSnippetFile(path string, name string, context stackSnippetContext) (string, error) {
	data, err := stackSnippetFiles.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("stack snippet could not be read: %s (%w)", name, err)
	}
	tpl, err := template.New(name).Option("missingkey=error").Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("stack snippet could not be parsed: %s (%w)", name, err)
	}
	var buffer bytes.Buffer
	if err := tpl.Execute(&buffer, context); err != nil {
		return "", fmt.Errorf("stack snippet could not be rendered: %s (%w)", name, err)
	}
	return buffer.String(), nil
}

// stackSnippetDefinitionByID 查找片段定义，供 example 和 stack 模板 include 共用。
func stackSnippetDefinitionByID(id string) (stackSnippetDefinition, bool) {
	for _, definition := range stackSnippetDefinitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return stackSnippetDefinition{}, false
}

// defaultStackSnippetContext 返回 example 命令使用的默认片段上下文。
func defaultStackSnippetContext() stackSnippetContext {
	return stackSnippetContext{
		StackName:           "example",
		XrayAPIPort:         10085,
		RelayPort:           24000,
		RelayUsername:       "relay-user",
		RelayRemark:         "example socks5",
		MemberName:          "usa1",
		MemberInboundName:   "relay",
		GroupProxies:        "server-a, server-b",
		SelectProxies:       "server-a, DIRECT",
		SocksListenerPort:   17090,
		HTTPListenerPort:    18090,
		ClashControllerPort: 19090,
	}
}

// stackTemplateSnippetContext 返回不同内置 stack 模板使用的片段默认值。
func stackTemplateSnippetContext(templateName string) stackSnippetContext {
	context := defaultStackSnippetContext()
	switch templateName {
	case "auto-url-test":
		context.StackName = "auto"
		context.XrayAPIPort = 10150
		context.RelayPort = 25000
		context.RelayUsername = "auto"
		context.RelayRemark = "auto relay socks"
		context.GroupProxies = "usa1-local, usa2-local"
		context.SelectProxies = "AutoProxy, usa1-local, usa2-local, DIRECT"
		context.SocksListenerPort = 17150
		context.HTTPListenerPort = 18150
		context.ClashControllerPort = 19150
	case "load-balance":
		context.StackName = "auto-balance"
		context.XrayAPIPort = 10160
		context.RelayPort = 26000
		context.RelayUsername = "balance"
		context.RelayRemark = "balance relay socks"
		context.GroupProxies = "usa1-local, usa2-local"
		context.SelectProxies = "BalanceProxy, usa1-local, usa2-local, DIRECT"
		context.SocksListenerPort = 17160
		context.HTTPListenerPort = 18160
		context.ClashControllerPort = 19160
	}
	return context
}

// indentSnippet 为 include 到 stack 模板中的片段补齐 YAML 缩进。
func indentSnippet(content string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.SplitAfter(content, "\n")
	var builder strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) == "" {
			builder.WriteString(line)
			continue
		}
		builder.WriteString(prefix)
		builder.WriteString(line)
	}
	return builder.String()
}
