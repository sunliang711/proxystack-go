package agent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExportConfigCommandRendersSubscription 验证 export-config 作为 ps-agent 顶层命令输出订阅文本。
func TestExportConfigCommandRendersSubscription(t *testing.T) {
	output := runAgentCommandForTest(t, "--base-dir", agentTestFixtureBaseDir(), "export-config", "sub", "alice")

	require.Contains(t, output, "proxies:")
	require.Contains(t, output, "usa1 vmess")
}

// TestSubExportConfigCommandIsRemoved 验证旧的 ps-agent sub export-config 入口不再注册。
func TestSubExportConfigCommandIsRemoved(t *testing.T) {
	_, err := runAgentCommandForTestError("--base-dir", agentTestFixtureBaseDir(), "sub", "export-config", "sub", "alice")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}

// agentTestFixtureBaseDir 返回 agent 测试 fixture 的仓库内路径。
func agentTestFixtureBaseDir() string {
	return filepath.Join("..", "..", "..", "tests", "fixtures", "example-project")
}
