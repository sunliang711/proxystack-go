package agent

import (
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/stretchr/testify/require"
)

// TestLifecycleTreatsAllAsStackName 验证 all 不再作为生命周期伪目标。
func TestLifecycleTreatsAllAsStackName(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: "all", Template: "pair", KeepTemplatePorts: true}))
	manager := &fakeUninstallManager{}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "stop", "all")

	require.ElementsMatch(t, []string{"proxystack-xray@all.service", "proxystack-clash@all.service"}, manager.stopped)
	require.Contains(t, output, "Service plan for stop (target: all):")
	require.Contains(t, output, "all.xrelay -> proxystack-xray@all.service")
	require.Contains(t, output, "all.clash -> proxystack-clash@all.service")
}

// TestLifecycleRejectsSubWithoutStack 验证 sub 不再作为订阅服务特殊 target。
func TestLifecycleRejectsSubWithoutStack(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	manager := &fakeUninstallManager{}
	withAgentServiceManager(t, manager)

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "stop", "sub")

	require.Error(t, err)
	require.Contains(t, err.Error(), "stack does not exist: sub")
	require.Empty(t, manager.stopped)
}
