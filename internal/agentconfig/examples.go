package agentconfig

// StackExampleSnippet 描述一个可复制的 stack 配置片段。
type StackExampleSnippet struct {
	Area        string
	Section     string
	Type        string
	Description string
	Content     string
}

// StackExampleSnippets 返回 ps-agent example 支持的全部 stack 配置片段。
func StackExampleSnippets() []StackExampleSnippet {
	snippets := []StackExampleSnippet{
		{
			Area:        "stack",
			Section:     "role",
			Type:        "edge",
			Description: "普通边缘代理 stack 角色",
			Content: `role: edge
`,
		},
		{
			Area:        "stack",
			Section:     "role",
			Type:        "auto",
			Description: "聚合或自动选择节点 stack 角色",
			Content: `role: auto
`,
		},
		{
			Area:        "xrelay",
			Section:     "api",
			Type:        "default",
			Description: "Xray API 配置片段，只允许监听 loopback 地址",
			Content: `api:
  enabled: true
  tag: api
  listen: 127.0.0.1:10085
  services: [StatsService]
`,
		},
		{
			Area:        "xrelay",
			Section:     "stats",
			Type:        "enabled",
			Description: "Xray stats 开关片段",
			Content: `stats:
  enabled: true
`,
		},
		{
			Area:        "xrelay",
			Section:     "policy",
			Type:        "enabled",
			Description: "Xray policy 开关片段",
			Content: `policy:
  enabled: true
`,
		},
		{
			Area:        "xrelay",
			Section:     "loglevel",
			Type:        "debug",
			Description: "Xray debug 日志级别",
			Content: `loglevel: debug
`,
		},
		{
			Area:        "xrelay",
			Section:     "loglevel",
			Type:        "info",
			Description: "Xray info 日志级别",
			Content: `loglevel: info
`,
		},
		{
			Area:        "xrelay",
			Section:     "loglevel",
			Type:        "warning",
			Description: "Xray warning 日志级别",
			Content: `loglevel: warning
`,
		},
		{
			Area:        "xrelay",
			Section:     "loglevel",
			Type:        "error",
			Description: "Xray error 日志级别",
			Content: `loglevel: error
`,
		},
		{
			Area:        "xrelay",
			Section:     "loglevel",
			Type:        "none",
			Description: "Xray none 日志级别",
			Content: `loglevel: none
`,
		},
		{
			Area:        "xrelay",
			Section:     "auth",
			Type:        "noauth",
			Description: "socks/http inbound 本地无认证片段",
			Content: `auth:
  type: noauth
`,
		},
		{
			Area:        "xrelay",
			Section:     "auth",
			Type:        "password",
			Description: "socks/http inbound 用户名密码认证片段",
			Content: `auth:
  type: password
  username: relay-user
  password: change-me-relay-password
`,
		},
		{
			Area:        "xrelay",
			Section:     "inbound",
			Type:        "vmess",
			Description: "vmess inbound，适合对外发布到订阅",
			Content: `- name: vmess
  protocol: vmess
  listen: 0.0.0.0
  port: 24100
  udp: true
  server: edge.example.com
  region: US
  network: raw
  sub: true
  users:
    - user: user1
      uuid: 11111111-1111-4111-8111-111111111111
      email: user1@example.com
      remark: example vmess
`,
		},
		{
			Area:        "xrelay",
			Section:     "inbound",
			Type:        "shadowsocks",
			Description: "shadowsocks inbound，示例使用传统单用户写法",
			Content: `- name: shadowsocks
  protocol: shadowsocks
  listen: 0.0.0.0
  port: 24101
  method: aes-256-gcm
  password: change-me-ss-password
  udp: true
  user: user1
  remark: example shadowsocks
  region: US
  sub: true
`,
		},
		{
			Area:        "xrelay",
			Section:     "inbound",
			Type:        "socks5",
			Description: "socks5 inbound，公开发布时必须使用 password 认证",
			Content: `- name: relay
  protocol: socks5
  listen: 0.0.0.0
  port: 24000
  udp: true
  auth:
    type: password
    username: relay-user
    password: change-me-relay-password
  user: user1
  remark: example socks5
  region: US
  sub: true
`,
		},
		{
			Area:        "xrelay",
			Section:     "inbound",
			Type:        "http",
			Description: "HTTP inbound，公开发布时必须使用 password 认证",
			Content: `- name: http
  protocol: http
  listen: 0.0.0.0
  port: 24001
  auth:
    type: password
    username: http-user
    password: change-me-http-password
  user: user1
  remark: example http
  region: US
  sub: true
`,
		},
		{
			Area:        "xrelay",
			Section:     "outbound",
			Type:        "clash",
			Description: "转发到本 stack 或其他 stack 的 Clash socks listener",
			Content: `outbound:
  type: clash
  ref: example.clash.socks
`,
		},
		{
			Area:        "xrelay",
			Section:     "outbound",
			Type:        "socks5",
			Description: "转发到外部 SOCKS5 上游",
			Content: `outbound:
  type: socks5
  server: 127.0.0.1
  port: 1080
  username: up-user
  password: up-pass
`,
		},
		{
			Area:        "xrelay",
			Section:     "outbound",
			Type:        "http",
			Description: "转发到外部 HTTP 上游",
			Content: `outbound:
  type: http
  server: 127.0.0.1
  port: 8080
  username: up-user
  password: up-pass
`,
		},
		{
			Area:        "xrelay",
			Section:     "outbound",
			Type:        "direct",
			Description: "直连出站",
			Content: `outbound:
  type: direct
`,
		},
		{
			Area:        "clash",
			Section:     "mode",
			Type:        "rule",
			Description: "mihomo Rule 运行模式",
			Content: `mode: Rule
`,
		},
		{
			Area:        "clash",
			Section:     "mode",
			Type:        "global",
			Description: "mihomo Global 运行模式",
			Content: `mode: Global
`,
		},
		{
			Area:        "clash",
			Section:     "mode",
			Type:        "direct",
			Description: "mihomo Direct 运行模式",
			Content: `mode: Direct
`,
		},
		{
			Area:        "clash",
			Section:     "loglevel",
			Type:        "debug",
			Description: "mihomo debug 日志级别",
			Content: `loglevel: debug
`,
		},
		{
			Area:        "clash",
			Section:     "loglevel",
			Type:        "info",
			Description: "mihomo info 日志级别",
			Content: `loglevel: info
`,
		},
		{
			Area:        "clash",
			Section:     "loglevel",
			Type:        "warning",
			Description: "mihomo warning 日志级别",
			Content: `loglevel: warning
`,
		},
		{
			Area:        "clash",
			Section:     "loglevel",
			Type:        "error",
			Description: "mihomo error 日志级别",
			Content: `loglevel: error
`,
		},
		{
			Area:        "clash",
			Section:     "loglevel",
			Type:        "silent",
			Description: "mihomo silent 日志级别",
			Content: `loglevel: silent
`,
		},
		{
			Area:        "clash",
			Section:     "controller",
			Type:        "default",
			Description: "mihomo external-controller 配置片段",
			Content: `controller:
  listen: 127.0.0.1:19090
  secret: change-me-clash-api-secret
`,
		},
		{
			Area:        "clash",
			Section:     "listener",
			Type:        "socks",
			Description: "Clash socks listener 片段",
			Content: `- name: socks
  listen: 127.0.0.1
  port: 17090
  users:
    - username: local-user
      password: local-pass
`,
		},
		{
			Area:        "clash",
			Section:     "listener",
			Type:        "http",
			Description: "Clash HTTP listener 片段",
			Content: `- name: http
  listen: 127.0.0.1
  port: 18090
  users: []
`,
		},
		{
			Area:        "clash",
			Section:     "upstream",
			Type:        "xrelay-socks5",
			Description: "引用其他 stack 的 xrelay socks5 inbound",
			Content: `- name: usa1-relay
  type: xrelay-socks5
  ref: usa1.relay
`,
		},
		{
			Area:        "clash",
			Section:     "upstream",
			Type:        "raw",
			Description: "mihomo 原生 proxy 透传片段，示例使用 vmess",
			Content: `- name: server-vmess
  type: raw
  config:
    type: vmess
    server: server.example.com
    port: 443
    uuid: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa
    network: ws
    tls: true
    ws_opts:
      headers:
        Host: server.example.com
      path: /vmess
`,
		},
		{
			Area:        "clash",
			Section:     "upstream",
			Type:        "raw-shadowsocks",
			Description: "mihomo 原生 shadowsocks proxy 片段",
			Content: `- name: server-shadowsocks
  type: raw
  config:
    type: shadowsocks
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: change-me-ss-password
`,
		},
		{
			Area:        "clash",
			Section:     "upstream",
			Type:        "raw-socks5",
			Description: "mihomo 原生 socks5 proxy 片段",
			Content: `- name: server-socks5
  type: raw
  config:
    type: socks5
    server: socks.example.com
    port: 1080
    username: up-user
    password: up-pass
`,
		},
		{
			Area:        "clash",
			Section:     "upstream",
			Type:        "raw-http",
			Description: "mihomo 原生 HTTP proxy 片段",
			Content: `- name: server-http
  type: raw
  config:
    type: http
    server: http.example.com
    port: 8080
    username: up-user
    password: up-pass
`,
		},
		{
			Area:        "clash",
			Section:     "group",
			Type:        "select",
			Description: "手动选择代理组",
			Content: `- name: AllProxy
  type: select
  proxies: [server-a, DIRECT]
`,
		},
		{
			Area:        "clash",
			Section:     "group",
			Type:        "url-test",
			Description: "按延迟自动选择代理组",
			Content: `- name: AutoProxy
  type: url-test
  proxies: [server-a, server-b]
  url: http://www.gstatic.com/generate_204
  interval: 120
`,
		},
		{
			Area:        "clash",
			Section:     "group",
			Type:        "load-balance",
			Description: "负载均衡代理组",
			Content: `- name: BalanceProxy
  type: load-balance
  proxies: [server-a, server-b]
  url: http://www.gstatic.com/generate_204
  interval: 120
  strategy: consistent-hashing
`,
		},
		{
			Area:        "clash",
			Section:     "group",
			Type:        "fallback",
			Description: "故障切换代理组",
			Content: `- name: FallbackProxy
  type: fallback
  proxies: [server-a, server-b]
  url: http://www.gstatic.com/generate_204
  interval: 120
`,
		},
		{
			Area:        "clash",
			Section:     "rules",
			Type:        "default",
			Description: "默认规则 profile 和额外规则片段",
			Content: `rules:
  profile: default
  final: AllProxy
  extra:
    - DOMAIN-SUFFIX,example.com,DIRECT
`,
		},
	}
	return append([]StackExampleSnippet(nil), snippets...)
}
