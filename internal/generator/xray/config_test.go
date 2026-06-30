package xray_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/generator/xray"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	ss2022ServerKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	ss2022AliceKey  = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	ss2022BobKey    = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
)

// TestRenderXrayExamplesMatchGolden 验证示例 stack 生成稳定 Xray JSON。
func TestRenderXrayExamplesMatchGolden(t *testing.T) {
	tests := []struct {
		stackName  string
		goldenName string
	}{
		{stackName: "usa1", goldenName: "usa1.json"},
		{stackName: "usa2", goldenName: "usa2.json"},
		{stackName: "auto", goldenName: "auto.json"},
	}

	for _, tt := range tests {
		t.Run(tt.stackName, func(t *testing.T) {
			stackSet := loadExampleStackSet(t)

			output, err := xray.DumpsConfig(stackSet, tt.stackName)

			require.NoError(t, err)
			require.Equal(t, readGolden(t, tt.goldenName), output)
		})
	}
}

// TestRenderXrayInboundMatrixMatchesGolden 验证所有 inbound 类型和 direct outbound 的生成结果。
func TestRenderXrayInboundMatrixMatchesGolden(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "matrix", "type: direct", inboundMatrixYAML(), ""))

	output, err := xray.DumpsConfig(stackSet, "matrix")

	require.NoError(t, err)
	require.Equal(t, readGolden(t, "matrix.json"), output)
}

// TestRenderXrayVmessTransportOptions 验证 vmess websocket/grpc 参数会生成到 streamSettings。
func TestRenderXrayVmessTransportOptions(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "transport", "type: direct", `- name: vmess-ws
  protocol: vmess
  listen: 127.0.0.1
  port: 26011
  network: ws
  ws_opts:
    path: /vmess
    headers:
      Host: edge.example.com
  sub: true
  users:
    - user: alice
      uuid: 22222222-2222-4222-8222-222222222222
- name: vmess-grpc
  protocol: vmess
  listen: 127.0.0.1
  port: 26012
  network: grpc
  grpc_opts:
    grpc_service_name: vmess
  sub: true
  users:
    - user: alice
      uuid: 33333333-3333-4333-8333-333333333333`, ""))

	output, err := xray.DumpsConfig(stackSet, "transport")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &parsed))
	inbounds := parsed["inbounds"].([]any)
	wsStream := inbounds[0].(map[string]any)["streamSettings"].(map[string]any)
	require.Equal(t, "ws", wsStream["network"])
	wsSettings := wsStream["wsSettings"].(map[string]any)
	require.Equal(t, "/vmess", wsSettings["path"])
	require.Equal(t, "edge.example.com", wsSettings["headers"].(map[string]any)["Host"])
	grpcStream := inbounds[1].(map[string]any)["streamSettings"].(map[string]any)
	require.Equal(t, "grpc", grpcStream["network"])
	require.Equal(t, "vmess", grpcStream["grpcSettings"].(map[string]any)["serviceName"])
}

// TestRenderXrayProxyOutboundsMatchGolden 验证外部 socks/http outbound 生成结果。
func TestRenderXrayProxyOutboundsMatchGolden(t *testing.T) {
	tests := []struct {
		name       string
		outbound   string
		goldenName string
	}{
		{
			name: "socksout",
			outbound: `type: socks5
server: socks.example.com
port: 1080
username: up-user
password: up-pass`,
			goldenName: "socks-outbound.json",
		},
		{
			name: "httpout",
			outbound: `type: http
server: http.example.com
port: 8080
username: http-up-user
password: http-up-pass`,
			goldenName: "http-outbound.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stackSet := makeStackSet(t, makeStack(t, tt.name, tt.outbound, asListItem(socksNoAuthInboundYAML()), ""))

			output, err := xray.DumpsConfig(stackSet, tt.name)

			require.NoError(t, err)
			require.Equal(t, readGolden(t, tt.goldenName), output)
		})
	}
}

// TestRenderXrayWildcardClashMatchesGolden 验证 wildcard clash listener 会生成 loopback socks outbound。
func TestRenderXrayWildcardClashMatchesGolden(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "wildcard", "type: clash\nref: wildcard.clash.socks", asListItem(socksNoAuthInboundYAML()), `
  listeners:
    socks:
      - name: local
        listen: 0.0.0.0
        port: 17001`))

	output, err := xray.DumpsConfig(stackSet, "wildcard")

	require.NoError(t, err)
	require.Equal(t, readGolden(t, "wildcard-clash.json"), output)
}

// TestRenderXrayShadowsocks2022Users 验证 SS2022 users 会生成共享 method 的多用户 clients。
func TestRenderXrayShadowsocks2022Users(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "ss2022", "type: direct", `- name: ss2022
  protocol: shadowsocks
  listen: 0.0.0.0
  port: 26001
  method: 2022-blake3-aes-256-gcm
  password: `+ss2022ServerKey+`
  sub: true
  users:
    - user: alice
      password: `+ss2022AliceKey+`
    - user: bob
      password: `+ss2022BobKey, ""))

	output, err := xray.DumpsConfig(stackSet, "ss2022")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &parsed))
	settings := parsed["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)
	require.Equal(t, "2022-blake3-aes-256-gcm", settings["method"])
	require.Len(t, settings["clients"], 2)
}

// TestXrayAPIListenRejectsPublicHost 验证 API listen 显式公网地址在模型校验阶段失败。
func TestXrayAPIListenRejectsPublicHost(t *testing.T) {
	_, err := parseStack(`name: public-api
enabled: true
role: edge
xrelay:
  enabled: true
  api:
    enabled: true
    listen: 0.0.0.0:10085
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
` + indentLines(asListItem(socksNoAuthInboundYAML()), 4) + `
clash:
  enabled: false
  controller:
    listen: 127.0.0.1:19001
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17001
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`)

	require.Error(t, err)
	require.Contains(t, err.Error(), "xray api listen must use loopback host")
}

func loadExampleStackSet(t *testing.T) domain.StackSet {
	t.Helper()
	testutil.ChdirRepo(t)
	globalConfig, err := config.LoadConfig("tests/fixtures/example-project/config.yaml")
	require.NoError(t, err)
	stackSet, err := config.LoadStacks(globalConfig, false)
	require.NoError(t, err)
	return stackSet
}

func makeStackSet(t *testing.T, stack domain.Stack) domain.StackSet {
	t.Helper()
	testutil.ChdirRepo(t)
	globalConfig, err := config.LoadConfig("tests/fixtures/example-project/config.yaml")
	require.NoError(t, err)
	return domain.StackSet{Config: globalConfig, Stacks: []domain.Stack{stack}}
}

func makeStack(t *testing.T, name string, outboundYAML string, inboundsYAML string, clashOverride string) domain.Stack {
	t.Helper()
	listeners := `
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17001`
	if clashOverride != "" {
		listeners = clashOverride
	}
	stack, err := parseStack(`name: ` + name + `
enabled: true
role: edge
labels: [test]
xrelay:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
` + indentLines(outboundYAML, 4) + `
  inbounds:
` + indentLines(inboundsYAML, 4) + `
clash:
  enabled: true
  mode: Rule
  controller:
    listen: 127.0.0.1:19001
    secret: demo-secret
` + listeners + `
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`)
	require.NoError(t, err)
	return stack
}

func parseStack(data string) (domain.Stack, error) {
	var stack domain.Stack
	if err := yaml.Unmarshal([]byte(data), &stack); err != nil {
		return domain.Stack{}, err
	}
	if err := stack.Validate(); err != nil {
		return domain.Stack{}, err
	}
	return stack, nil
}

func inboundMatrixYAML() string {
	return `- name: vmess-in
  protocol: vmess
  listen: 127.0.0.1
  port: 26001
  network: raw
  tag: custom-vmess
  sub: true
  users:
    - user: alice
      uuid: 22222222-2222-4222-8222-222222222222
- name: ss-in
  protocol: shadowsocks
  listen: 127.0.0.1
  port: 26002
  password: ss-password
  method: chacha20-ietf-poly1305
  udp: true
  sub: true
` + asListItem(socksNoAuthInboundYAML()) + `
- name: socks-password
  protocol: socks5
  listen: 127.0.0.1
  port: 26004
  auth:
    type: password
    username: sock-user
    password: sock-pass
  udp: true
  sub: true
- name: http-noauth
  protocol: http
  listen: 127.0.0.1
  port: 26005
  auth:
    type: noauth
  sub: false
- name: http-password
  protocol: http
  listen: 127.0.0.1
  port: 26006
  auth:
    type: password
    username: http-user
    password: http-pass
  sub: true`
}

func socksNoAuthInboundYAML() string {
	return `name: socks-noauth
protocol: socks5
listen: 127.0.0.1
port: 26003
auth:
  type: noauth
udp: false
sub: false`
}

func indentLines(value string, spaces int) string {
	prefix := ""
	for i := 0; i < spaces; i++ {
		prefix += " "
	}
	output := ""
	for index, line := range strings.Split(value, "\n") {
		if index > 0 {
			output += "\n"
		}
		output += prefix + line
	}
	return output
}

func asListItem(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if index == 0 {
			lines[index] = "- " + line
			continue
		}
		lines[index] = "  " + line
	}
	return strings.Join(lines, "\n")
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("tests/golden/xray/" + name)
	require.NoError(t, err)
	return string(data)
}
