package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestLoadConfigAndStacksAcceptExamples 验证示例项目可以加载为强类型模型。
func TestLoadConfigAndStacksAcceptExamples(t *testing.T) {
	testutil.ChdirRepo(t)
	globalConfig, err := config.LoadConfig("tests/fixtures/example-project/config.yaml")
	require.NoError(t, err)

	stackSet, err := config.LoadStacks(globalConfig, false)
	require.NoError(t, err)

	require.Equal(t, []string{"auto", "usa1", "usa2"}, stackSet.StackNames())
	require.Equal(t, "Rule", stackSet.ByName()["usa1"].Clash.Mode)
}

// TestDomainModelsAllowUnknownFields 验证领域模型允许未来扩展字段。
func TestDomainModelsAllowUnknownFields(t *testing.T) {
	tempDir := t.TempDir()
	stacksDir := filepath.Join(tempDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o755))
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+"extra_global: kept\n")
	writeFile(t, filepath.Join(stacksDir, "edge.yaml"), validStackYAML("edge")+"extra_stack: kept\n")

	globalConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	require.NoError(t, err)
	_, err = config.LoadStacks(globalConfig, false)
	require.NoError(t, err)
}

// TestLoadConfigRejectsDuplicateUserProfile 验证 config.yaml users 中 user/profile 唯一。
func TestLoadConfigRejectsDuplicateUserProfile(t *testing.T) {
	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+`users:
  - user: alice
    profile: tokyo
    uuid: 11111111-1111-4111-8111-111111111111
  - user: alice
    profile: tokyo
    uuid: 22222222-2222-4222-8222-222222222222
`)

	_, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate config user profile")
}

// TestLoadStacksExpandsGlobalUserRefs 验证 stack user_refs 会按 user/profile 展开为生成器使用的用户凭据。
func TestLoadStacksExpandsGlobalUserRefs(t *testing.T) {
	tempDir := t.TempDir()
	stacksDir := filepath.Join(tempDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o755))
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+`users:
  - user: alice
    profile: default
    uuid: 11111111-1111-4111-8111-111111111111
    password: alice-default-password
  - user: alice
    profile: tokyo
    uuid: 22222222-2222-4222-8222-222222222222
    password: alice-tokyo-password
    email: alice@example.com
    remark: Tokyo
    display_template: '{{ .stack }} {{ .profile }} {{ .remark }}'
    tag: alice-tokyo
`)
	writeFile(t, filepath.Join(stacksDir, "edge.yaml"), `name: edge
enabled: true
role: edge
xray:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
    - name: vmess
      protocol: vmess
      listen: 127.0.0.1
      port: 24001
      network: raw
      sub: true
      user_refs:
        - user: alice
          profile: tokyo
          uuid: 33333333-3333-4333-8333-333333333333
          remark: Tokyo Override
    - name: ss
      protocol: shadowsocks
      listen: 127.0.0.1
      port: 24002
      method: aes-256-gcm
      password: server-password
      sub: true
      user_refs:
        - user: alice
          profile: tokyo
          password: alice-ss-override
          remark: SS Tokyo
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`)
	globalConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	require.NoError(t, err)

	stackSet, err := config.LoadStacks(globalConfig, false)

	require.NoError(t, err)
	vmessUser := stackSet.Stacks[0].Xray.Inbounds[0].Users[0]
	require.Equal(t, "alice", vmessUser.User)
	require.Equal(t, "tokyo", vmessUser.Profile)
	require.Equal(t, "33333333-3333-4333-8333-333333333333", vmessUser.UUID)
	require.Equal(t, "alice@example.com", vmessUser.Email)
	require.Equal(t, "Tokyo Override", vmessUser.Remark)
	require.Equal(t, "{{ .stack }} {{ .profile }} {{ .remark }}", vmessUser.DisplayTemplate)
	require.Empty(t, stackSet.Stacks[0].Xray.Inbounds[0].UserRefs)
	ssUser := stackSet.Stacks[0].Xray.Inbounds[1].Users[0]
	require.Equal(t, "alice-ss-override", ssUser.Password)
	require.Equal(t, "SS Tokyo", ssUser.Remark)
}

// TestLoadStacksRejectsUserRefMissingProtocolCredential 验证协议必需凭据缺失时 fail fast。
func TestLoadStacksRejectsUserRefMissingProtocolCredential(t *testing.T) {
	tempDir := t.TempDir()
	stacksDir := filepath.Join(tempDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o755))
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+`users:
  - user: alice
    profile: default
    password: alice-password
`)
	writeFile(t, filepath.Join(stacksDir, "edge.yaml"), strings.Replace(validStackYAML("edge"), `    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: noauth
      sub: false`, `    - name: vmess
      protocol: vmess
      listen: 127.0.0.1
      port: 24001
      network: raw
      sub: true
      user_refs: [alice]`, 1))
	globalConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	require.NoError(t, err)

	_, err = config.LoadStacks(globalConfig, false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "uuid is required for vmess user_ref")
}

// TestLoadStacksRejectsMissingUserRefProfile 验证 user_refs 引用不存在的全局用户档案会失败。
func TestLoadStacksRejectsMissingUserRefProfile(t *testing.T) {
	tempDir := t.TempDir()
	stacksDir := filepath.Join(tempDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o755))
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+`users:
  - user: alice
    profile: default
    uuid: 11111111-1111-4111-8111-111111111111
`)
	writeFile(t, filepath.Join(stacksDir, "edge.yaml"), strings.Replace(validStackYAML("edge"), `    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: noauth
      sub: false`, `    - name: vmess
      protocol: vmess
      listen: 127.0.0.1
      port: 24001
      network: raw
      sub: true
      user_refs:
        - user: alice
          profile: tokyo`, 1))
	globalConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	require.NoError(t, err)

	_, err = config.LoadStacks(globalConfig, false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "config user profile does not exist")
}

// TestLoadStacksRejectsDuplicateProxyNameFromUserProfileTemplate 验证全局用户模板展开后会参与订阅节点名重复检查。
func TestLoadStacksRejectsDuplicateProxyNameFromUserProfileTemplate(t *testing.T) {
	tempDir := t.TempDir()
	stacksDir := filepath.Join(tempDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o755))
	writeFile(t, filepath.Join(tempDir, "config.yaml"), validConfigYAML(tempDir)+`users:
  - user: alice
    profile: default
    display_template: '{{ .user }} fixed'
`)
	firstStack := strings.Replace(validStackYAML("edge-a"), `      sub: false`, `      sub: true
      user_refs: [alice]`, 1)
	firstStack = strings.Replace(firstStack, "        type: noauth", "        type: password\n        username: demo-user\n        password: demo-pass", 1)
	secondStack := strings.Replace(validStackYAML("edge-b"), `      sub: false`, `      sub: true
      user_refs: [alice]`, 1)
	secondStack = strings.Replace(secondStack, "        type: noauth", "        type: password\n        username: demo-user\n        password: demo-pass", 1)
	secondStack = strings.ReplaceAll(secondStack, "24001", "24002")
	secondStack = strings.ReplaceAll(secondStack, "17091", "17092")
	secondStack = strings.ReplaceAll(secondStack, "19091", "19092")
	writeFile(t, filepath.Join(stacksDir, "edge-a.yaml"), firstStack)
	writeFile(t, filepath.Join(stacksDir, "edge-b.yaml"), secondStack)
	globalConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	require.NoError(t, err)

	_, err = config.LoadStacks(globalConfig, false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate proxy name for user")
	require.Contains(t, err.Error(), "alice fixed")
}

// TestLoadStackRejectsUDPForHTTPInbound 验证不支持 UDP 的 inbound 协议会拒绝显式 udp 字段。
func TestLoadStackRejectsUDPForHTTPInbound(t *testing.T) {
	stackPath := filepath.Join(t.TempDir(), "edge.yaml")
	writeFile(t, stackPath, `name: edge
enabled: true
role: edge
xray:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
    - name: web
      protocol: http
      listen: 127.0.0.1
      port: 24001
      udp: false
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`)

	_, err := config.LoadStack(stackPath)

	require.Error(t, err)
	require.Contains(t, err.Error(), "udp is not supported for http inbound")
}

// TestLoadConfigUsesConfigDirectoryAsBaseDir 验证全局配置不含 base_dir 时按配置文件目录解析路径。
func TestLoadConfigUsesConfigDirectoryAsBaseDir(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	writeFile(t, configPath, validConfigYAML(tempDir))

	globalConfig, err := config.LoadConfig(configPath)

	require.NoError(t, err)
	require.Equal(t, tempDir, globalConfig.BaseDir)
	require.Equal(t, filepath.Join(tempDir, "stacks"), globalConfig.StacksDir())
}

// TestStrictTransportRejectsUnknownFields 验证 sub/订阅传输模型使用 strict decode。
func TestStrictTransportRejectsUnknownFields(t *testing.T) {
	data := []byte("listen: 0.0.0.0:3003\nunknown: true\n")
	var subConfig config.SubServerConfig

	err := config.DecodeStrictYAML(data, &subConfig)

	require.Error(t, err)
	require.Contains(t, err.Error(), "field unknown not found")
}

// TestLoadSubServerConfigRejectsDataDir 验证 data_dir 不再属于 sub config YAML 契约。
func TestLoadSubServerConfigRejectsDataDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sub.yaml")
	writeFile(t, configPath, "data_dir: /tmp/sub\n")

	_, err := config.LoadSubServerConfig(configPath)

	require.Error(t, err)
	require.Contains(t, err.Error(), "field data_dir not found")
}

// TestLoadSubServerConfigPreservesExplicitFalse 验证 managed_config 显式 false 不会被默认值覆盖。
func TestLoadSubServerConfigPreservesExplicitFalse(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sub.yaml")
	writeFile(t, configPath, `managed_config:
  enabled: false
  strict: false
watch_debounce: 0
`)

	subConfig, err := config.LoadSubServerConfig(configPath)

	require.NoError(t, err)
	require.False(t, subConfig.ManagedConfig.EnabledValue())
	require.False(t, subConfig.ManagedConfig.StrictValue())
	require.Equal(t, 0.0, subConfig.WatchDebounce)
	require.Equal(t, config.LogFormatJSON, subConfig.Log.Format)
}

// TestLoadSubServerConfigDefaultsToLoopback 验证默认订阅服务只监听本机。
func TestLoadSubServerConfigDefaultsToLoopback(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sub.yaml")
	writeFile(t, configPath, "log:\n  format: json\n")

	subConfig, err := config.LoadSubServerConfig(configPath)

	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3003", subConfig.Listen)
	require.Equal(t, "none", subConfig.Access.Type)
}

// TestLoadSubServerConfigRejectsInvalidValues 验证 sub config 非法值会 fail fast。
func TestLoadSubServerConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "bad listen", content: "listen: bad-listen\n", want: "listen must use host:port format"},
		{name: "token missing", content: "access:\n  type: token\n", want: "access.token is required"},
		{name: "public listen without token", content: "listen: 0.0.0.0:3003\naccess:\n  type: none\n", want: "access.type none is only allowed"},
		{name: "access unknown", content: "access:\n  type: none\n  extra: true\n", want: "field extra not found"},
		{name: "log unknown", content: "log:\n  format: json\n  extra: true\n", want: "field extra not found"},
		{name: "bad log format", content: "log:\n  format: text\n", want: "log.format must be json or console"},
		{name: "bad managed scheme", content: "managed_config:\n  public_base_url: ftp://example.com/sub\n", want: "must use http or https"},
		{name: "managed query", content: "managed_config:\n  public_base_url: https://example.com/sub?token=1\n", want: "must not include query or fragment"},
		{name: "managed fragment", content: "managed_config:\n  public_base_url: https://example.com/sub#token\n", want: "must not include query or fragment"},
		{name: "managed userinfo", content: "managed_config:\n  public_base_url: https://user@example.com/sub\n", want: "must not include userinfo"},
		{name: "managed missing host", content: "managed_config:\n  public_base_url: https:///sub\n", want: "host is required"},
		{name: "bad watch", content: "watch_interval: -1\n", want: "watch_interval must be greater than 0"},
		{name: "bad managed interval", content: "managed_config:\n  interval: -1\n", want: "managed_config.interval must be greater than 0"},
		{name: "zero managed interval", content: "managed_config:\n  interval: 0\n", want: "managed_config.interval must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "sub.yaml")
			writeFile(t, configPath, strings.TrimSpace(tt.content)+"\n")

			_, err := config.LoadSubServerConfig(configPath)

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func validConfigYAML(baseDir string) string {
	return `version: 1
paths:
  stacks: stacks
port_ranges:
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
`
}

func validStackYAML(name string) string {
	return `name: ` + name + `
enabled: true
role: edge
xray:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: noauth
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`
}
