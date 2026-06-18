package agent

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRootHelpUsesCommandGroups 验证 ps-agent 无参数 usage 按 Python 版 help panel 分组展示。
func TestRootHelpUsesCommandGroups(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{})

	err := command.Execute()

	require.NoError(t, err)
	helpText := output.String()
	require.Contains(t, helpText, "配置管理")
	require.Contains(t, helpText, "安装更新")
	require.Contains(t, helpText, "校验与渲染")
	require.Contains(t, helpText, "服务控制")
	require.Contains(t, helpText, "订阅发布")
	require.Contains(t, helpText, "诊断工具")
	require.Contains(t, helpText, "其它")
	require.Contains(t, helpText, "  init")
	require.Contains(t, helpText, "  setup")
	require.Contains(t, helpText, "  render")
	require.Contains(t, helpText, "  service")
	require.Contains(t, helpText, "  sub")
	require.Contains(t, helpText, "  ipinfo")
	require.Contains(t, helpText, "--base-dir")
	require.NotContains(t, helpText, "--config")
	require.NotContains(t, helpText, "-c,")
	require.NotContains(t, helpText, "Available Commands:")
	require.NotContains(t, helpText, "Additional Commands:")
}
