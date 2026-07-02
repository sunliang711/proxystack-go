package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/stretchr/testify/require"
)

type fakeUninstallManager struct {
	serviceFiles []string
	target       string
	stopped      []string
	calls        []string
	active       map[string]bool
	activeChecks []string
	stopErr      error
}

// InstallUnits 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	return nil, nil
}

// UninstallUnits 记录卸载 target 并返回预设服务文件路径。
func (f *fakeUninstallManager) UninstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	f.calls = append(f.calls, "uninstall")
	f.target = target
	return f.serviceFiles, nil
}

// Start 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Start(ctx context.Context, services []string) error {
	return nil
}

// Stop 记录顶层 uninstall 停止服务的请求。
func (f *fakeUninstallManager) Stop(ctx context.Context, services []string) error {
	f.calls = append(f.calls, "stop")
	f.stopped = append([]string(nil), services...)
	return f.stopErr
}

// Restart 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Restart(ctx context.Context, services []string) error {
	return nil
}

// Enable 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Enable(ctx context.Context, services []string) error {
	return nil
}

// Disable 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Disable(ctx context.Context, services []string) error {
	return nil
}

// IsActive 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) IsActive(ctx context.Context, service string) (bool, error) {
	f.activeChecks = append(f.activeChecks, service)
	return f.active[service], nil
}

// Status 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Status(ctx context.Context, services []string) (servicemanager.Result, error) {
	return servicemanager.Result{}, nil
}

// Logs 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) Logs(ctx context.Context, services []string, follow bool) (servicemanager.Result, error) {
	return servicemanager.Result{}, nil
}

// ServiceForNode 满足 service.Manager 接口，uninstall 测试不会调用。
func (f *fakeUninstallManager) ServiceForNode(node graph.ServiceNode) string {
	return ""
}

// ServicesForNodes 返回服务节点对应的 systemd unit 名。
func (f *fakeUninstallManager) ServicesForNodes(nodes []graph.ServiceNode) []string {
	services := make([]string, 0, len(nodes))
	for _, node := range nodes {
		services = append(services, node.ServiceName())
	}
	return services
}

// SubService 返回订阅服务对应的 systemd unit 名。
func (f *fakeUninstallManager) SubService() string {
	return "proxystack-sub.service"
}

// TestAgentUninstallPreservesConfigAndStacks 验证普通卸载只保留用户配置数据。
func TestAgentUninstallPreservesConfigAndStacks(t *testing.T) {
	baseDir := prepareUninstallBaseDir(t)
	manager := &fakeUninstallManager{serviceFiles: []string{"/tmp/proxystack-xray@.service"}}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "uninstall")

	require.Equal(t, "", manager.target)
	require.Equal(t, []string{"stop", "uninstall"}, manager.calls)
	require.ElementsMatch(t, []string{"proxystack-xray@usa1.service", "proxystack-clash@usa1.service"}, manager.stopped)
	require.Contains(t, output, "Stopping service: usa1")
	require.NotContains(t, output, "Stopping service: sub")
	require.Contains(t, output, "Removing service file: /tmp/proxystack-xray@.service")
	require.Contains(t, output, "Removing base directory entry: "+filepath.Join(baseDir, "bin"))
	require.Contains(t, output, "Removing base directory entry: "+filepath.Join(baseDir, "runtime"))
	require.Contains(t, output, "Preserving: "+filepath.Join(baseDir, "config.yaml"))
	require.Contains(t, output, "Preserving: "+filepath.Join(baseDir, "stacks"))
	require.Contains(t, output, "Uninstall OK")
	require.NotContains(t, output, "Stopped services: [")
	require.FileExists(t, filepath.Join(baseDir, "config.yaml"))
	require.FileExists(t, filepath.Join(baseDir, "stacks", "usa1.yaml"))
	require.NoDirExists(t, filepath.Join(baseDir, "bin"))
	require.NoDirExists(t, filepath.Join(baseDir, "runtime"))
}

// TestAgentUninstallPurgeRemovesBaseDir 验证 purge 卸载会删除整个 base dir。
func TestAgentUninstallPurgeRemovesBaseDir(t *testing.T) {
	baseDir := prepareUninstallBaseDir(t)
	manager := &fakeUninstallManager{serviceFiles: []string{"/tmp/proxystack-xray@.service"}}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "uninstall", "--purge")

	require.Equal(t, "", manager.target)
	require.Equal(t, []string{"stop", "uninstall"}, manager.calls)
	require.Contains(t, output, "Removing base directory: "+baseDir)
	require.Contains(t, output, "Uninstall OK")
	require.NoDirExists(t, baseDir)
}

// TestAgentUninstallSkipsStopWhenNoStackServices 验证无 stack 服务时不会调用空 stop。
func TestAgentUninstallSkipsStopWhenNoStackServices(t *testing.T) {
	baseDir := prepareEmptyUninstallBaseDir(t)
	manager := &fakeUninstallManager{serviceFiles: []string{"/tmp/proxystack-xray@.service"}}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "uninstall")

	require.Equal(t, []string{"uninstall"}, manager.calls)
	require.Empty(t, manager.stopped)
	require.NotContains(t, output, "Stopping service:")
	require.Contains(t, output, "Removing service file: /tmp/proxystack-xray@.service")
	require.Contains(t, output, "Uninstall OK")
}

// TestUninstallServiceDisplayNames 验证卸载日志展示 stack 名并去重底层组件服务。
func TestUninstallServiceDisplayNames(t *testing.T) {
	names := uninstallServiceDisplayNames([]string{
		"proxystack-clash@usa1.service",
		"proxystack-xray@usa1.service",
		"com.proxystack.mihomo.de1",
		"com.proxystack.xray.de1",
		"custom.service",
	})

	require.Equal(t, []string{"usa1", "de1", "custom.service"}, names)
}

// TestPurgeFlagIsNotGlobal 验证 purge 不再作为 root 级 flag 被接受。
func TestPurgeFlagIsNotGlobal(t *testing.T) {
	_, err := runAgentCommandForTestError("--purge", "uninstall")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown flag: --purge")
}

// TestPurgeFlagRejectedForServiceUninstall 验证 service uninstall 不接受顶层 uninstall 的 purge。
func TestPurgeFlagRejectedForServiceUninstall(t *testing.T) {
	_, err := runAgentCommandForTestError("service", "uninstall", "--purge")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown flag: --purge")
}

// TestAgentUninstallRejectsUserHome 验证卸载命令拒绝过宽的用户 home 路径。
func TestAgentUninstallRejectsUserHome(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	_, err = runAgentCommandForTestError("--base-dir", homeDir, "uninstall")

	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be user home")
}

// TestAgentUninstallRejectsCurrentWorkingDirectory 验证卸载命令拒绝当前工作目录及其父级。
func TestAgentUninstallRejectsCurrentWorkingDirectory(t *testing.T) {
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	_, err = runAgentCommandForTestError("--base-dir", workingDir, "uninstall")

	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be current working directory")
}

// TestAgentUninstallRejectsDirectoryWithoutInstallMarker 验证卸载命令拒绝没有安装标记的既有目录。
func TestAgentUninstallRejectsDirectoryWithoutInstallMarker(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "plain")
	require.NoError(t, os.MkdirAll(baseDir, 0o750))

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "uninstall")

	require.Error(t, err)
	require.Contains(t, err.Error(), "does not look like a proxystack installation")
}

// prepareUninstallBaseDir 构造包含配置、stack 和安装产物的测试目录。
func prepareUninstallBaseDir(t *testing.T) string {
	t.Helper()
	baseDir := filepath.Join(t.TempDir(), "proxystack")
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "bin"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "runtime"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(doctorTestConfig()), 0o640))
	stackContent := strings.Replace(doctorTestStack(24001, 17091, 19091), "name: edge", "name: usa1", 1)
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "usa1.yaml"), []byte(stackContent), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "bin", "psctl"), []byte("bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "runtime", "manifest.json"), []byte("{}\n"), 0o640))
	return baseDir
}

// prepareEmptyUninstallBaseDir 构造没有 stack 文件但带安装标记的卸载测试目录。
func prepareEmptyUninstallBaseDir(t *testing.T) string {
	t.Helper()
	baseDir := filepath.Join(t.TempDir(), "proxystack")
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(doctorTestConfig()), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "bin", "psctl"), []byte("bin"), 0o750))
	return baseDir
}

// withAgentServiceManager 注入 fake service manager，避免测试访问真实系统服务目录。
func withAgentServiceManager(t *testing.T, manager servicemanager.Manager) {
	t.Helper()
	oldFactory := agentServiceManagerFactory
	agentServiceManagerFactory = func(kind string, opts ...servicemanager.ManagerOption) (servicemanager.Manager, error) {
		return manager, nil
	}
	t.Cleanup(func() {
		agentServiceManagerFactory = oldFactory
	})
}
