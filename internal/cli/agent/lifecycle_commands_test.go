package agent

import (
	"os"
	"path/filepath"
	"strings"
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
	require.Contains(t, output, "all.xray -> proxystack-xray@all.service")
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

// TestLifecycleStopsDisabledExplicitStackTarget 验证显式指定 disabled stack 时仍可操作历史服务。
func TestLifecycleStopsDisabledExplicitStackTarget(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	stackData, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stackPath, []byte(strings.Replace(string(stackData), "enabled: true", "enabled: false", 1)), 0o640))
	manager := &fakeUninstallManager{}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "stop", "usa1")

	require.ElementsMatch(t, []string{"proxystack-xray@usa1.service", "proxystack-clash@usa1.service"}, manager.stopped)
	require.Contains(t, output, "Service plan for stop (target: usa1):")
	require.Contains(t, output, "usa1.xray -> proxystack-xray@usa1.service")
	require.Contains(t, output, "usa1.clash -> proxystack-clash@usa1.service")
}

// TestLifecycleStartSkipsDisabledExplicitStackTarget 验证显式启动 disabled stack 时不会误启动服务。
func TestLifecycleStartSkipsDisabledExplicitStackTarget(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	stackData, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stackPath, []byte(strings.Replace(string(stackData), "enabled: true", "enabled: false", 1)), 0o640))
	manager := &fakeUninstallManager{}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "start", "usa1")

	require.Empty(t, manager.started)
	require.Contains(t, output, "No enabled services matched target: usa1")
}
