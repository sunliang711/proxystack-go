package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	configloader "github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	calls   []string
	result  Result
	results []Result
}

// Run 记录命令调用并返回预设结果。
func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		return result, nil
	}
	return f.result, nil
}

// TestRenderLaunchdPlistsUsesStackInstances 验证 launchd 为每个 stack 生成具体 plist。
func TestRenderLaunchdPlistsUsesStackInstances(t *testing.T) {
	cfg := prepareLaunchdConfig(t)

	plists, err := RenderLaunchdPlists(cfg, "")

	require.NoError(t, err)
	require.Contains(t, plists, LaunchdXrayLabel("usa1")+".plist")
	require.Contains(t, plists, LaunchdMihomoLabel("usa1")+".plist")
	require.Contains(t, plists[LaunchdXrayLabel("usa1")+".plist"], filepath.Join(cfg.BaseDir, "runtime", "generated", "xray", "usa1.json"))
	require.Contains(t, plists[LaunchdMihomoLabel("usa1")+".plist"], filepath.Join(cfg.BaseDir, "runtime", "generated", "mihomo", "usa1.yaml"))
	require.NotContains(t, plists, LaunchdSubLabel+".plist")
}

// TestLaunchdManagerInstallReconcilesPlists 验证 launchd install 会写入期望 plist 并清理陈旧 plist。
func TestLaunchdManagerInstallReconcilesPlists(t *testing.T) {
	cfg := prepareLaunchdConfig(t)
	dir := t.TempDir()
	runner := &fakeRunner{results: []Result{{ExitCode: 0}, {ExitCode: 0}}}
	manager := LaunchdManager{Dir: dir, Runner: runner}
	stalePath := filepath.Join(dir, LaunchdXrayLabel("removed")+".plist")
	require.NoError(t, os.WriteFile(stalePath, []byte("stale"), 0o644))

	paths, err := manager.InstallUnits(cfg, "")

	require.NoError(t, err)
	require.NotContains(t, paths, filepath.Join(dir, LaunchdSubLabel+".plist"))
	require.Contains(t, paths, filepath.Join(dir, LaunchdXrayLabel("usa1")+".plist"))
	require.FileExists(t, filepath.Join(dir, LaunchdMihomoLabel("usa1")+".plist"))
	require.NoFileExists(t, stalePath)
	require.Equal(t, []string{
		"launchctl print system/com.proxystack.xray.removed",
		"launchctl bootout system/com.proxystack.xray.removed",
	}, runner.calls)
}

// TestLaunchdManagerUninstallBootsOutLoadedJobs 验证 launchd uninstall 删除 plist 前会卸载已加载 job。
func TestLaunchdManagerUninstallBootsOutLoadedJobs(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{results: []Result{{ExitCode: 0}, {ExitCode: 0}}}
	manager := LaunchdManager{Dir: dir, Runner: runner}
	path := filepath.Join(dir, LaunchdSubLabel+".plist")
	require.NoError(t, os.WriteFile(path, []byte("plist"), 0o644))

	removed, err := manager.UninstallUnits(domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}, "sub")

	require.NoError(t, err)
	require.Equal(t, []string{path}, removed)
	require.NoFileExists(t, path)
	require.Equal(t, []string{
		"launchctl print system/com.proxystack.sub",
		"launchctl bootout system/com.proxystack.sub",
	}, runner.calls)
}

// TestLaunchdManagerLifecycleCommands 验证 launchd 生命周期命令映射。
func TestLaunchdManagerLifecycleCommands(t *testing.T) {
	runner := &fakeRunner{results: []Result{
		{ExitCode: 4, Stderr: "Could not find service"},
		{ExitCode: 4, Stderr: "Could not find service"},
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 4, Stderr: "Could not find service"},
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 0},
		{ExitCode: 0},
	}}
	manager := LaunchdManager{Runner: runner, Dir: "/Library/LaunchDaemons"}

	require.NoError(t, manager.Start(context.Background(), []string{LaunchdSubLabel}))
	require.NoError(t, manager.Restart(context.Background(), []string{LaunchdSubLabel}))
	require.NoError(t, manager.Stop(context.Background(), []string{LaunchdSubLabel}))

	require.Equal(t, []string{
		"launchctl print system/com.proxystack.sub",
		"launchctl print system/com.proxystack.sub",
		"launchctl bootstrap system /Library/LaunchDaemons/com.proxystack.sub.plist",
		"launchctl kickstart system/com.proxystack.sub",
		"launchctl print system/com.proxystack.sub",
		"launchctl bootstrap system /Library/LaunchDaemons/com.proxystack.sub.plist",
		"launchctl kickstart -k system/com.proxystack.sub",
		"launchctl print system/com.proxystack.sub",
		"launchctl bootout system/com.proxystack.sub",
	}, runner.calls)
}

// TestLaunchdEnableDisableUsePersistentOverride 验证 enable/disable 使用 launchd 持久开关。
func TestLaunchdEnableDisableUsePersistentOverride(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 0}}
	manager := LaunchdManager{Runner: runner}

	require.NoError(t, manager.Enable(context.Background(), []string{LaunchdSubLabel}))
	require.NoError(t, manager.Disable(context.Background(), []string{LaunchdSubLabel}))

	require.Equal(t, []string{
		"launchctl enable system/com.proxystack.sub",
		"launchctl disable system/com.proxystack.sub",
	}, runner.calls)
}

// TestLaunchdIsActiveParsesRunningState 验证 loaded 但未运行的服务不会被误判 active。
func TestLaunchdIsActiveParsesRunningState(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   bool
	}{
		{name: "running state", stdout: "state = running\n", want: true},
		{name: "valid pid", stdout: "pid = 123\n", want: true},
		{name: "waiting state", stdout: "state = waiting\n", want: false},
		{name: "empty", stdout: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{result: Result{ExitCode: 0, Stdout: tt.stdout}}
			manager := LaunchdManager{Runner: runner}

			active, err := manager.IsActive(context.Background(), LaunchdSubLabel)

			require.NoError(t, err)
			require.Equal(t, tt.want, active)
		})
	}
}

// TestLaunchdIsLoadedReturnsUnexpectedPrintError 验证 launchctl 异常不会被误当成未加载。
func TestLaunchdIsLoadedReturnsUnexpectedPrintError(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 1, Stderr: "permission denied"}}
	manager := LaunchdManager{Runner: runner}

	_, err := manager.isLoaded(context.Background(), LaunchdSubLabel)

	require.Error(t, err)
	require.Contains(t, err.Error(), "permission denied")
}

// TestLaunchdLogsUsesProcessPredicate 验证 launchd logs 会构造进程过滤表达式。
func TestLaunchdLogsUsesProcessPredicate(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 0}}
	manager := LaunchdManager{Runner: runner}

	_, err := manager.Logs(context.Background(), []string{LaunchdXrayLabel("usa1"), LaunchdMihomoLabel("usa1")}, true)

	require.NoError(t, err)
	require.Equal(t, `log stream --style compact --predicate process == "mihomo" OR process == "xray"`, runner.calls[0])
}

// prepareLaunchdConfig 创建 launchd plist 测试使用的最小配置。
func prepareLaunchdConfig(t *testing.T) domain.GlobalConfig {
	t.Helper()
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	cfg, err := configloader.LoadConfig(filepath.Join(baseDir, "config.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "runtime"), 0o750))
	return cfg
}
