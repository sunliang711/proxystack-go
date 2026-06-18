package mihomo_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/generator/mihomo"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestRenderMihomoExamplesMatchGolden 验证示例 stack 生成稳定 mihomo YAML。
func TestRenderMihomoExamplesMatchGolden(t *testing.T) {
	tests := []struct {
		stackName  string
		goldenName string
	}{
		{stackName: "usa1", goldenName: "usa1.yaml"},
		{stackName: "usa2", goldenName: "usa2.yaml"},
		{stackName: "auto", goldenName: "auto.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.stackName, func(t *testing.T) {
			output, err := mihomo.DumpsConfig(loadExampleStackSet(t), tt.stackName)

			require.NoError(t, err)
			require.Equal(t, readGolden(t, tt.goldenName), output)
		})
	}
}

// TestRenderMihomoLogLevelOverrideMatchGolden 验证 stack 级 loglevel 覆盖优先。
func TestRenderMihomoLogLevelOverrideMatchGolden(t *testing.T) {
	stack := parseStack(t, `name: loglevel
enabled: true
role: edge
xrelay:
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
  mode: Rule
  loglevel: silent
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
	stackSet := domain.StackSet{Config: loadGlobalConfig(t), Stacks: []domain.Stack{stack}}

	output, err := mihomo.DumpsConfig(stackSet, "loglevel")

	require.NoError(t, err)
	require.Equal(t, readGolden(t, "loglevel-override.yaml"), output)
}

// TestRenderMihomoPreservesEmptyListenerUsers 验证 users: [] 不会被当作缺失字段丢弃。
func TestRenderMihomoPreservesEmptyListenerUsers(t *testing.T) {
	stack := parseStack(t, `name: empty-users
enabled: true
role: edge
xrelay:
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
  mode: Rule
  controller:
    listen: 127.0.0.1:19091
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
        users: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: direct.example.com
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	stackSet := domain.StackSet{Config: loadGlobalConfig(t), Stacks: []domain.Stack{stack}}

	output, err := mihomo.DumpsConfig(stackSet, "empty-users")

	require.NoError(t, err)
	require.Contains(t, output, "users: []")
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

func loadGlobalConfig(t *testing.T) domain.GlobalConfig {
	t.Helper()
	testutil.ChdirRepo(t)
	globalConfig, err := config.LoadConfig("tests/fixtures/example-project/config.yaml")
	require.NoError(t, err)
	return globalConfig
}

func parseStack(t *testing.T, content string) domain.Stack {
	t.Helper()
	var stack domain.Stack
	require.NoError(t, yaml.Unmarshal([]byte(content), &stack))
	require.NoError(t, stack.Validate())
	return stack
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.RepoRoot(t), "tests", "golden", "mihomo", name))
	require.NoError(t, err)
	return string(data)
}
