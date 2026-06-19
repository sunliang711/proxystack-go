package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/install"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type fakeSetupManager struct {
	fakeUninstallManager
	config domain.GlobalConfig
	target string
}

// InstallUnits 记录 setup 安装服务文件时传入的配置和 target。
func (f *fakeSetupManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	f.config = config
	f.target = target
	return []string{"/tmp/proxystack-xray@.service"}, nil
}

// TestSetupContinuesWhenConfigExists 验证 setup 遇到既有 config 时只补 layout 并继续安装。
func TestSetupContinuesWhenConfigExists(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "existing.example.com"}))
	require.NoError(t, os.RemoveAll(filepath.Join(baseDir, "bin")))
	manager := &fakeSetupManager{}
	withAgentServiceManager(t, manager)
	var installRequest install.Request
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		installRequest = request
		return []install.Result{{Target: install.TargetMihomo, Written: []string{filepath.Join(baseDir, "bin", "mihomo")}}}, nil
	})
	withSetupRunLifecycle(t, func(command *cobra.Command, action string, target string, follow bool) error {
		t.Fatalf("setup without --start should not run lifecycle")
		return nil
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "setup")

	require.Equal(t, install.TargetAll, installRequest.Target)
	require.Equal(t, "", manager.target)
	require.Equal(t, baseDir, manager.config.BaseDir)
	require.DirExists(t, filepath.Join(baseDir, "bin"))
	require.NoDirExists(t, filepath.Join(baseDir, "sub"))
	require.NoFileExists(t, filepath.Join(baseDir, "sub", "config.yaml"))
	require.Contains(t, output, "mihomo installed")
	require.Contains(t, output, "Installed units:")
}

// TestSetupStartRunsLifecycle 验证 setup --start 在安装服务文件后复用 start 生命周期。
func TestSetupStartRunsLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	manager := &fakeSetupManager{}
	withAgentServiceManager(t, manager)
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		return nil, nil
	})
	var lifecycleAction string
	var lifecycleTarget string
	var lifecycleFollow bool
	withSetupRunLifecycle(t, func(command *cobra.Command, action string, target string, follow bool) error {
		lifecycleAction = action
		lifecycleTarget = target
		lifecycleFollow = follow
		return nil
	})

	runAgentCommandForTest(t, "--base-dir", baseDir, "setup", "--start")

	require.Equal(t, "start", lifecycleAction)
	require.Equal(t, "", lifecycleTarget)
	require.False(t, lifecycleFollow)
}

// TestSetupStartWrapsLifecycleError 验证 setup --start 的启动失败会保留步骤上下文。
func TestSetupStartWrapsLifecycleError(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	manager := &fakeSetupManager{}
	withAgentServiceManager(t, manager)
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		return nil, nil
	})
	withSetupRunLifecycle(t, func(command *cobra.Command, action string, target string, follow bool) error {
		return errors.New("boom")
	})

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "setup", "--start")

	require.Error(t, err)
	require.Contains(t, err.Error(), "setup start failed: boom")
}

// withSetupInstallTarget 注入 fake install 流程，避免 setup 测试访问网络。
func withSetupInstallTarget(t *testing.T, fn func(context.Context, install.Request, install.Progress) ([]install.Result, error)) {
	t.Helper()
	old := setupInstallTargetFunc
	setupInstallTargetFunc = fn
	t.Cleanup(func() {
		setupInstallTargetFunc = old
	})
}

// withSetupRunLifecycle 注入 fake 生命周期流程，避免 setup 测试访问真实服务管理器。
func withSetupRunLifecycle(t *testing.T, fn func(*cobra.Command, string, string, bool) error) {
	t.Helper()
	old := setupRunLifecycleFunc
	setupRunLifecycleFunc = fn
	t.Cleanup(func() {
		setupRunLifecycleFunc = old
	})
}
