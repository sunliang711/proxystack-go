package sub_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

const fixedGeneratedAt = "2026-06-05T12:00:00+08:00"

// TestInputYAMLMatchesGolden 验证 input 传输契约输出稳定。
func TestInputYAMLMatchesGolden(t *testing.T) {
	input := loadManualInput(t)

	output := subgen.InputToYAML(input)

	require.Equal(t, readSubGolden(t, "input.yaml"), output)
}

// TestLoadInputFileKeepsExistingServer 验证旧 input 文件继续保留节点 server。
func TestLoadInputFileKeepsExistingServer(t *testing.T) {
	input := loadManualInput(t)

	require.Empty(t, input.ExternalHost)
	require.Equal(t, "proxy.example.com", input.Nodes[0].Server)
}

// TestLoadInputContentAppliesExternalHostDefault 验证文件级 external_host 可补齐缺失或空 server。
func TestLoadInputContentAppliesExternalHostDefault(t *testing.T) {
	tests := []struct {
		name string
		node string
	}{
		{
			name: "missing server",
			node: `  - id: manual:relay
    user: alice
    protocol: socks5
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
`,
		},
		{
			name: "empty server",
			node: `  - id: manual:relay
    user: alice
    protocol: socks5
    server: ""
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := subgen.LoadInputContent("manual.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
external_host: default.example.com
nodes:
`+tt.node))

			require.NoError(t, err)
			require.Equal(t, "default.example.com", input.Nodes[0].Server)
		})
	}
}

// TestLoadInputContentKeepsNodeServer 验证局部 server 优先于文件级 external_host。
func TestLoadInputContentKeepsNodeServer(t *testing.T) {
	input, err := subgen.LoadInputContent("manual.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
external_host: default.example.com
nodes:
  - id: manual:relay
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
`))

	require.NoError(t, err)
	require.Equal(t, "proxy.example.com", input.Nodes[0].Server)
}

// TestLoadInputContentRejectsMissingServerWithoutExternalHost 验证无局部 server 且无文件级 external_host 时失败。
func TestLoadInputContentRejectsMissingServerWithoutExternalHost(t *testing.T) {
	_, err := subgen.LoadInputContent("manual.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: manual:relay
    user: alice
    protocol: socks5
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "node.server is required")
}

// TestLoadInputContentRejectsUnknownNodeFieldWithoutDirect 验证默认节点仍严格拒绝未知字段。
func TestLoadInputContentRejectsUnknownNodeFieldWithoutDirect(t *testing.T) {
	_, err := subgen.LoadInputContent("manual.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: manual:vmess
    user: alice
    protocol: vmess
    server: proxy.example.com
    port: 443
    tag: vmess:443
    remark: Manual Vmess
    uuid: 11111111-1111-4111-8111-111111111111
    network: ws
    tls: true
`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "field tls not found")
}

// TestLoadInputContentRejectsUnknownAuthFieldWithoutDirect 验证默认节点仍严格拒绝 auth 内未知字段。
func TestLoadInputContentRejectsUnknownAuthFieldWithoutDirect(t *testing.T) {
	_, err := subgen.LoadInputContent("manual.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: manual:socks5
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
    auth:
      type: password
      username: demo-user
      password: demo-pass
      extra: demo
`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "field extra not found in type sub.Auth")
}

// TestLoadJSONInputContentRejectsUnknownAuthFieldWithoutDirect 验证 JSON 默认节点仍严格拒绝 auth 内未知字段。
func TestLoadJSONInputContentRejectsUnknownAuthFieldWithoutDirect(t *testing.T) {
	_, err := subgen.LoadInputContent("manual.json", []byte(`{
  "input_schema": "proxystack.subscription-input",
  "input_version": 1,
  "source": "manual",
  "generated_at": "2026-06-05T12:00:00+08:00",
  "nodes": [
    {
      "id": "manual:socks5",
      "user": "alice",
      "protocol": "socks5",
      "server": "proxy.example.com",
      "port": 24001,
      "tag": "socks5:24001:relay",
      "remark": "Manual Relay",
      "auth": {
        "type": "password",
        "username": "demo-user",
        "password": "demo-pass",
        "extra": "demo"
      }
    }
  ]
}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), `json: unknown field "extra"`)
}

// TestDirectNodeRendersCustomClashFields 验证 direct 节点保留并输出 Clash 自定义字段。
func TestDirectNodeRendersCustomClashFields(t *testing.T) {
	input := loadDirectVmessInput(t)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	clashOutput, err := subgen.RenderClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Contains(t, clashOutput, "name: Remote Vmess\n    type: vmess\n    server: remote.example.com\n    port: 443\n")
	require.Contains(t, clashOutput, "tls: true\n")
	require.Contains(t, clashOutput, "skip-cert-verify: true\n")
	require.Contains(t, clashOutput, "servername: remote.example.com\n")
	require.Contains(t, clashOutput, "ws-opts:\n      path: /vmess\n      headers:\n        Host: remote.example.com\n")
	require.Contains(t, clashOutput, "alterId: 0\n")
	require.Contains(t, clashOutput, "cipher: auto\n")
}

// TestDirectNodeRendersSurgeVmessExtensions 验证 direct vmess 节点会映射常见 TLS/WS Surge 参数。
func TestDirectNodeRendersSurgeVmessExtensions(t *testing.T) {
	input := loadDirectVmessInput(t)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)
	require.NoError(t, err)
	require.Contains(t, surgeOutput, "Remote Vmess = vmess, remote.example.com, 443, username=11111111-1111-4111-8111-111111111111, network=ws, vmess-aead=true, tls=true, skip-cert-verify=true, sni=remote.example.com, ws=true, ws-path=/vmess, ws-headers=Host:remote.example.com|X-Test:demo")
}

// TestDirectNodeRendersSurgeWebSocketAlias 验证 direct vmess 兼容 websocket 网络别名。
func TestDirectNodeRendersSurgeWebSocketAlias(t *testing.T) {
	input := loadDirectVmessInputWithNetwork(t, "websocket")
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)
	require.NoError(t, err)
	require.Contains(t, surgeOutput, "network=ws, vmess-aead=true")
	require.Contains(t, surgeOutput, "ws=true")
}

// TestDirectNodeSkipsUnsupportedSurgeNodes 验证 Surge 会跳过不支持或不确认的 direct 节点并记录 warning。
func TestDirectNodeSkipsUnsupportedSurgeNodes(t *testing.T) {
	var logOutput bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&logOutput)
	defer func() {
		log.Logger = originalLogger
	}()
	input, err := subgen.LoadInputContent("direct.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: direct
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: remote:raw-vmess
    user: alice
    direct: true
    type: vmess
    server: raw.example.com
    port: 443
    remark: Raw Vmess
    uuid: 11111111-1111-4111-8111-111111111111
    network: raw
  - id: remote:vless
    user: alice
    direct: true
    type: vless
    server: vless.example.com
    port: 443
    remark: Remote Vless
    uuid: 22222222-2222-4222-8222-222222222222
  - id: remote:grpc-vmess
    user: alice
    direct: true
    type: vmess
    server: grpc.example.com
    port: 443
    remark: Grpc Vmess
    uuid: 33333333-3333-4333-8333-333333333333
    network: grpc
`))
	require.NoError(t, err)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	clashOutput, err := subgen.RenderClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Contains(t, clashOutput, "name: Remote Vless\n    type: vless\n")
	require.Contains(t, clashOutput, "name: Grpc Vmess\n    type: vmess\n")
	require.Empty(t, logOutput.String())

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)
	require.NoError(t, err)
	require.Contains(t, surgeOutput, "Raw Vmess = vmess, raw.example.com, 443, username=11111111-1111-4111-8111-111111111111, network=raw, vmess-aead=true")
	require.NotContains(t, surgeOutput, "Remote Vless")
	require.NotContains(t, surgeOutput, "Grpc Vmess")
	logs := logOutput.String()
	require.Contains(t, logs, "skipping unsupported surge subscription node")
	require.Contains(t, logs, "unsupported surge protocol: vless")
	require.Contains(t, logs, "unsupported vmess network: grpc")
	require.NotContains(t, logs, "vless.example.com")
	require.NotContains(t, logs, "grpc.example.com")
	require.NotContains(t, logs, "22222222-2222-4222-8222-222222222222")
	require.NotContains(t, logs, "33333333-3333-4333-8333-333333333333")
	require.NotContains(t, logs, "password")
}

// TestDirectNodeInputYAMLKeepsCustomFields 验证规范化输出不会丢失 direct 自定义字段。
func TestDirectNodeInputYAMLKeepsCustomFields(t *testing.T) {
	input := loadDirectVmessInput(t)

	output := subgen.InputToYAML(input)

	require.Contains(t, output, "direct: true\n")
	require.Contains(t, output, "protocol: vmess\n")
	require.Contains(t, output, "tls: true\n")
	require.Contains(t, output, "ws-opts:\n      path: /vmess\n")
}

// TestDirectNodeIndexJSONKeepsCustomFields 验证 index JSON 不会丢失 direct 自定义字段。
func TestDirectNodeIndexJSONKeepsCustomFields(t *testing.T) {
	input := loadDirectVmessInput(t)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	output, err := subgen.IndexToJSON(index)

	require.NoError(t, err)
	require.Contains(t, output, `"direct": true`)
	require.Contains(t, output, `"tls": true`)
	require.Contains(t, output, `"skip-cert-verify": true`)
	require.Contains(t, output, `"servername": "remote.example.com"`)
	require.Contains(t, output, `"ws-opts": {`)
	require.Contains(t, output, `"Host": "remote.example.com"`)
}

// TestDirectJSONInputKeepsCustomFields 验证 JSON input 中的 direct 节点也会保留自定义字段。
func TestDirectJSONInputKeepsCustomFields(t *testing.T) {
	input, err := subgen.LoadInputContent("direct.json", []byte(`{
  "input_schema": "proxystack.subscription-input",
  "input_version": 1,
  "source": "direct-json",
  "generated_at": "2026-06-05T12:00:00+08:00",
  "nodes": [
    {
      "id": "remote:json-vmess",
      "user": "alice",
      "direct": true,
      "type": "vmess",
      "name": "JSON Remote",
      "server": "json.example.com",
      "port": 443,
      "uuid": "22222222-2222-4222-8222-222222222222",
      "network": "websocket",
      "tls": true,
      "ws-opts": {"path": "/json"}
    }
  ]
}`))
	require.NoError(t, err)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.json", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	clashOutput, err := subgen.RenderClashSubscription(index, "alice", "", "")

	require.NoError(t, err)
	require.Contains(t, clashOutput, "name: JSON Remote\n    type: vmess\n    network: websocket\n    port: 443\n    server: json.example.com\n    tls: true\n")
	require.Contains(t, clashOutput, "uuid: 22222222-2222-4222-8222-222222222222\n")
	require.Contains(t, clashOutput, "ws-opts:\n      path: /json\n")
}

// TestDirectYAMLBoolVariants 验证 direct 开关和布尔自定义字段支持 YAML 布尔大小写。
func TestDirectYAMLBoolVariants(t *testing.T) {
	input, err := subgen.LoadInputContent("direct.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: direct
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: remote:vmess
    user: alice
    direct: True
    type: vmess
    server: remote.example.com
    port: 443
    remark: Remote Vmess
    uuid: 11111111-1111-4111-8111-111111111111
    network: ws
    tls: True
`))
	require.NoError(t, err)
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "direct.yaml", Input: input}}, subgen.Access{Type: "none"}, fixedGeneratedAt)
	require.NoError(t, err)

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)

	require.NoError(t, err)
	require.Contains(t, surgeOutput, "tls=true")
}

// TestInputYAMLIncludesExternalHost 验证 input YAML 会输出文件级 external_host 且字段顺序稳定。
func TestInputYAMLIncludesExternalHost(t *testing.T) {
	input := loadManualInput(t)
	input.ExternalHost = "default.example.com"

	output := subgen.InputToYAML(input)

	require.Contains(t, output, "generated_at: '"+fixedGeneratedAt+"'\nexternal_host: default.example.com\nnodes:\n")
}

// TestIndexJSONMatchesGolden 验证合并后的 index JSON 稳定且包含 access。
func TestIndexJSONMatchesGolden(t *testing.T) {
	index := buildManualIndex(t)

	output, err := subgen.IndexToJSON(index)

	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "index.json"), output)
}

// TestRenderSubscriptionsMatchGolden 验证默认三类订阅模板与 Python golden 对齐。
func TestRenderSubscriptionsMatchGolden(t *testing.T) {
	index := buildManualIndex(t)

	clashOutput, err := subgen.RenderClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "clash.yaml"), clashOutput)

	premiumOutput, err := subgen.RenderPremiumClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "premium-clash.yaml"), premiumOutput)

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "surge.txt"), surgeOutput)
}

// TestMergeInputsRejectsDuplicateNodeID 验证重复 node id 会 fail fast。
func TestMergeInputsRejectsDuplicateNodeID(t *testing.T) {
	input := loadManualInput(t)

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: input},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
}

// TestMergeInputsRejectsDuplicateProxyNameForSameUser 验证同用户重复代理名会失败。
func TestMergeInputsRejectsDuplicateProxyNameForSameUser(t *testing.T) {
	input := loadManualInput(t)
	other := cloneInput(input)
	other.Source = "other"
	other.Nodes[0].ID = "other:relay"

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: other},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate proxy name for user")
}

// TestMergeInputsAllowsSameProxyNameForDifferentUsers 验证不同用户可使用相同代理名。
func TestMergeInputsAllowsSameProxyNameForDifferentUsers(t *testing.T) {
	input := loadManualInput(t)
	other := cloneInput(input)
	other.Source = "other"
	other.Nodes[0].ID = "other:relay"
	other.Nodes[0].User = "bob"

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: other},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.NoError(t, err)
}

// TestTemplateOverrideAndStrictUndefined 验证模板覆盖顺序和未定义变量失败。
func TestTemplateOverrideAndStrictUndefined(t *testing.T) {
	index := buildManualIndex(t)
	templateDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(templateDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{{ user }}:{{ proxy_names | tojson }}\n"), 0o644))

	output, err := subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.NoError(t, err)
	require.Equal(t, "alice:[\"Manual Relay\"]\n", output)

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{{ missing_value }}\n"), 0o644))
	_, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "undefined variable")

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{% if not managed_config_url %}ok{% endif %}\n"), 0o644))
	output, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.NoError(t, err)
	require.Equal(t, "ok\n", output)

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{% for proxy_name in proxy_names %}{% endfor %}{{ proxy_name }}\n"), 0o644))
	_, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "undefined variable: proxy_name")
}

func loadManualInput(t *testing.T) subgen.Input {
	t.Helper()
	input, err := subgen.LoadInputFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	return input
}

func loadDirectVmessInput(t *testing.T) subgen.Input {
	t.Helper()
	return loadDirectVmessInputWithNetwork(t, "ws")
}

func loadDirectVmessInputWithNetwork(t *testing.T, network string) subgen.Input {
	t.Helper()
	input, err := subgen.LoadInputContent("direct.yaml", []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: direct
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: remote:vmess
    user: alice
    direct: true
    type: vmess
    server: remote.example.com
    port: 443
    remark: Remote Vmess
    uuid: 11111111-1111-4111-8111-111111111111
    network: `+network+`
    tls: true
    skip-cert-verify: true
    servername: remote.example.com
    ws-opts:
      path: /vmess
      headers:
        Host: remote.example.com
        X-Test: demo
`))
	require.NoError(t, err)
	return input
}

func buildManualIndex(t *testing.T) subgen.Index {
	t.Helper()
	index, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "manual.yaml", Input: loadManualInput(t)},
	}, subgen.Access{Type: "token", Token: "demo-token"}, fixedGeneratedAt)
	require.NoError(t, err)
	return index
}

func readSubGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(testutil.RepoPath(t, "tests", "golden", "sub", name))
	require.NoError(t, err)
	return string(data)
}

func cloneInput(input subgen.Input) subgen.Input {
	cloned := input
	cloned.Nodes = append([]subgen.Node(nil), input.Nodes...)
	return cloned
}
