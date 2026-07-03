package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/install"
	"github.com/stretchr/testify/require"
)

type fakeSetupManager struct {
	fakeUninstallManager
	config domain.GlobalConfig
	target string
	calls  *[]string
}

// InstallUnits 记录 setup 安装服务文件时传入的配置和 target。
func (f *fakeSetupManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "local")
	}
	f.config = config
	f.target = target
	return []string{"/tmp/proxystack-xray@.service"}, nil
}

// TestSetupDefaultRunsLocalThenDepsWhenConfigExists 验证 setup 默认等价 all，并按 local -> deps 顺序执行。
func TestSetupDefaultRunsLocalThenDepsWhenConfigExists(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "existing.example.com"}))
	require.NoError(t, os.RemoveAll(filepath.Join(baseDir, "bin")))
	calls := []string{}
	manager := &fakeSetupManager{calls: &calls}
	withAgentServiceManager(t, manager)
	var installRequest install.Request
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		calls = append(calls, "deps")
		installRequest = request
		return []install.Result{{Target: install.TargetMihomo, Written: []string{filepath.Join(baseDir, "bin", "mihomo")}}}, nil
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "setup")

	require.Equal(t, []string{"local", "deps"}, calls)
	require.Equal(t, install.TargetAll, installRequest.Target)
	require.Equal(t, "", manager.target)
	require.Equal(t, baseDir, manager.config.BaseDir)
	require.DirExists(t, filepath.Join(baseDir, "bin"))
	require.NoDirExists(t, filepath.Join(baseDir, "sub"))
	require.NoFileExists(t, filepath.Join(baseDir, "sub", "config.yaml"))
	require.Contains(t, output, "mihomo installed")
	require.Contains(t, output, "Installed units:")
}

// TestSetupLocalDoesNotInstallDeps 验证 setup local 只做本地初始化和 service unit 安装。
func TestSetupLocalDoesNotInstallDeps(t *testing.T) {
	baseDir := t.TempDir()
	manager := &fakeSetupManager{}
	withAgentServiceManager(t, manager)
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		t.Fatalf("setup local should not install managed dependencies")
		return nil, nil
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "setup", "local", "--external-host", "proxy.example.com")

	require.Equal(t, "", manager.target)
	require.Equal(t, baseDir, manager.config.BaseDir)
	require.FileExists(t, filepath.Join(baseDir, "config.yaml"))
	require.Contains(t, output, "Installed units:")
	require.NotContains(t, output, "mihomo installed")
}

// TestSetupDepsUsesInstallTargetAllWithoutServiceInstall 验证 setup deps 只安装托管依赖且目标固定为 all。
func TestSetupDepsUsesInstallTargetAllWithoutServiceInstall(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	var installRequest install.Request
	withSetupInstallTarget(t, func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
		installRequest = request
		return []install.Result{{Target: install.TargetGeo, Skipped: true}}, nil
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "setup", "deps")

	require.Equal(t, install.TargetAll, installRequest.Target)
	require.Contains(t, output, "geo skipped")
	require.NotContains(t, output, "Installed units:")
}

// TestSetupRejectsRemovedStartFlag 验证 setup 不再接受旧版 --start flag。
func TestSetupRejectsRemovedStartFlag(t *testing.T) {
	_, err := runAgentCommandForTestError("--base-dir", t.TempDir(), "setup", "--start")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown flag")
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
