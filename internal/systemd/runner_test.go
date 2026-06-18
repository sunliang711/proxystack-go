package systemd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"github.com/stretchr/testify/require"
)

// fakeRunner 记录外部命令调用并返回预设结果。
type fakeRunner struct {
	calls  []string
	result Result
}

// Run 记录命令名和参数，供测试断言调用形态。
func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.result, nil
}

// TestStatusAllowsInactiveExitCode 验证 systemctl status 退出码 3 不作为错误。
func TestStatusAllowsInactiveExitCode(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 3, Stdout: "inactive"}}
	manager := Manager{Runner: runner}

	_, err := manager.Status(context.Background(), []string{"proxystack-sub.service"})

	require.NoError(t, err)
	require.Equal(t, []string{"systemctl status proxystack-sub.service"}, runner.calls)
}

// TestIsActiveTreatsUnknownUnitAsInactive 验证未安装 unit 不阻断 active 检测。
func TestIsActiveTreatsUnknownUnitAsInactive(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 4}}
	manager := Manager{Runner: runner}

	active, err := manager.IsActive(context.Background(), "missing.service")

	require.NoError(t, err)
	require.False(t, active)
}

// TestLogsUsesSingleJournalctlCall 验证多 unit logs 只调用一次 journalctl。
func TestLogsUsesSingleJournalctlCall(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 0}}
	manager := Manager{Runner: runner}

	_, err := manager.Logs(context.Background(), []string{"a.service", "b.service"}, true)

	require.NoError(t, err)
	require.Len(t, runner.calls, 1)
	require.Equal(t, "journalctl -u a.service -u b.service --no-pager -n 100 -f", runner.calls[0])
}

// TestCommandRunnerStreamsJournalctlFollowOutput 验证 journalctl follow 输出会实时透传到 writer。
func TestCommandRunnerStreamsJournalctlFollowOutput(t *testing.T) {
	binDir := t.TempDir()
	journalctl := filepath.Join(binDir, "journalctl")
	require.NoError(t, os.WriteFile(journalctl, []byte("#!/bin/sh\nprintf 'stdout-line\\n'\nprintf 'stderr-line\\n' >&2\n"), 0o750))
	var stdout strings.Builder
	var stderr strings.Builder
	runner := CommandRunner{Stdout: &stdout, Stderr: &stderr}

	result, err := runner.Run(context.Background(), journalctl, "-u", "a.service", "-f")

	require.NoError(t, err)
	require.Equal(t, "stdout-line\n", stdout.String())
	require.Equal(t, "stderr-line\n", stderr.String())
	require.Empty(t, result.Stdout)
	require.Empty(t, result.Stderr)
}

// TestCommandRunnerStreamsMacOSLogStreamOutput 验证 macOS log stream 输出会实时透传到 writer。
func TestCommandRunnerStreamsMacOSLogStreamOutput(t *testing.T) {
	binDir := t.TempDir()
	logCommand := filepath.Join(binDir, "log")
	require.NoError(t, os.WriteFile(logCommand, []byte("#!/bin/sh\nprintf 'stdout-line\\n'\nprintf 'stderr-line\\n' >&2\n"), 0o750))
	var stdout strings.Builder
	var stderr strings.Builder
	runner := CommandRunner{Stdout: &stdout, Stderr: &stderr}

	result, err := runner.Run(context.Background(), logCommand, "stream", "--style", "compact")

	require.NoError(t, err)
	require.Equal(t, "stdout-line\n", stdout.String())
	require.Equal(t, "stderr-line\n", stderr.String())
	require.Empty(t, result.Stdout)
	require.Empty(t, result.Stderr)
}

// TestRenderUnitsUsesGeneratedFiles 验证 unit 只引用 runtime/generated 中的配置文件。
func TestRenderUnitsUsesGeneratedFiles(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}

	units := RenderUnits(cfg)

	require.Contains(t, units[XrayUnitTemplate], "/opt/proxystack/runtime/generated/xray/%i.json")
	require.Contains(t, units[ClashUnitTemplate], "/opt/proxystack/runtime/generated/mihomo/%i.yaml")
	require.Contains(t, units[SubUnit], "ps-sub --base-dir /opt/proxystack serve")
	require.NotContains(t, units[SubUnit], "--config")
	require.NotContains(t, units[SubUnit], "runtime/generated")
}

// TestRenderUnitsUsesCustomBaseDir 验证 systemd unit 跟随运行时 base dir 渲染路径。
func TestRenderUnitsUsesCustomBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	cfg := domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths()}

	units := RenderUnits(cfg)

	require.Contains(t, units[XrayUnitTemplate], filepath.Join(baseDir, "runtime", "generated", "xray", "%i.json"))
	require.Contains(t, units[ClashUnitTemplate], filepath.Join(baseDir, "runtime", "generated", "mihomo", "%i.yaml"))
	require.Contains(t, units[SubUnit], "ps-sub --base-dir "+baseDir+" serve")
	require.Contains(t, units[SubUnit], "ReadWritePaths="+filepath.Join(baseDir, "sub"))
}

// TestInstallUnitsRejectsUnknownTarget 验证 unit install/uninstall 不会把未知 target 当作 all。
func TestInstallUnitsRejectsUnknownTarget(t *testing.T) {
	manager := Manager{UnitDir: t.TempDir()}

	_, err := manager.InstallUnits(domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}, "typo")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported unit target")
}

// TestInstallUnitsReloadsSystemdDaemon 验证 unit 文件写入后会刷新 systemd 配置。
func TestInstallUnitsReloadsSystemdDaemon(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 0}}
	manager := Manager{Runner: runner, UnitDir: t.TempDir()}

	paths, err := manager.InstallUnits(domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}, "xray")

	require.NoError(t, err)
	require.Len(t, paths, 1)
	require.Equal(t, []string{"systemctl daemon-reload"}, runner.calls)
}

// TestInstallUnitsReturnsDaemonReloadError 验证 systemd 刷新失败时保留已写路径和错误上下文。
func TestInstallUnitsReturnsDaemonReloadError(t *testing.T) {
	runner := &fakeRunner{result: Result{ExitCode: 1, Stderr: "reload failed"}}
	manager := Manager{Runner: runner, UnitDir: t.TempDir()}

	paths, err := manager.InstallUnits(domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}, "xray")

	require.Error(t, err)
	require.Len(t, paths, 1)
	require.Contains(t, err.Error(), "systemd daemon-reload failed")
	require.Contains(t, err.Error(), "reload failed")
	require.Equal(t, []string{"systemctl daemon-reload"}, runner.calls)
}

// TestUnitsForNodes 验证服务节点到 unit 名称的映射。
func TestUnitsForNodes(t *testing.T) {
	units := UnitsForNodes([]graph.ServiceNode{{Stack: "usa1", Component: "xrelay"}, {Stack: "usa1", Component: "clash"}})

	require.Equal(t, []string{"proxystack-xray@usa1.service", "proxystack-clash@usa1.service"}, units)
}

// TestMetadataFixerCallsChmodAndChown 验证 metadata 修复支持 fake owner 修复。
func TestMetadataFixerCallsChmodAndChown(t *testing.T) {
	calls := make([]string, 0)
	fixer := MetadataFixer{
		Chmod: func(path string, mode os.FileMode) error {
			calls = append(calls, "chmod:"+path)
			return nil
		},
		Chown: func(path string, uid int, gid int) error {
			calls = append(calls, "chown:"+path)
			return nil
		},
	}

	err := fixer.Fix("/opt/proxystack/bin/xray", 0o750, 1000, 1000)

	require.NoError(t, err)
	require.Equal(t, []string{"chmod:/opt/proxystack/bin/xray", "chown:/opt/proxystack/bin/xray"}, calls)
}

// TestRepairStandardMetadataUsesOwner 验证标准路径批量修复会传入 uid/gid。
func TestRepairStandardMetadataUsesOwner(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "bin"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "geo"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "runtime", "generated", "xray"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "runtime", "generated", "mihomo"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte("version: 1\n"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "runtime", "generated", "xray", "usa1.json"), []byte("{}\n"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "runtime", "generated", "mihomo", "usa1.yaml"), []byte("{}\n"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "runtime", "manifest.json"), []byte("{}\n"), 0o640))
	cfg := domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths(), ConfigPath: filepath.Join(baseDir, "config.yaml")}
	owners := make([]string, 0)
	fixer := MetadataFixer{
		Chmod: func(path string, mode os.FileMode) error { return nil },
		Chown: func(path string, uid int, gid int) error {
			owners = append(owners, path)
			require.Equal(t, 1000, uid)
			require.Equal(t, 1001, gid)
			return nil
		},
	}

	err := RepairStandardMetadata(cfg, 1000, 1001, fixer)

	require.NoError(t, err)
	require.Contains(t, owners, baseDir)
	require.Contains(t, owners, filepath.Join(baseDir, "config.yaml"))
	require.Contains(t, owners, filepath.Join(baseDir, "runtime", "generated", "xray"))
	require.Contains(t, owners, filepath.Join(baseDir, "runtime", "generated", "mihomo"))
	require.Contains(t, owners, filepath.Join(baseDir, "runtime", "generated", "xray", "usa1.json"))
	require.Contains(t, owners, filepath.Join(baseDir, "runtime", "generated", "mihomo", "usa1.yaml"))
	require.Contains(t, owners, filepath.Join(baseDir, "runtime", "manifest.json"))
}

// TestRepairSubMetadataRecursesSubTree 验证 sub 运行目录下的子目录和文件都会修复 owner。
func TestRepairSubMetadataRecursesSubTree(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "sub", "inputs", "manual"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "sub", "config.yaml"), []byte("access:\n  type: none\n"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "sub", "inputs", "manual", "usa.yaml"), []byte("nodes: []\n"), 0o640))
	cfg := domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths()}
	modes := make(map[string]os.FileMode)
	owners := make(map[string][2]int)
	fixer := MetadataFixer{
		Chmod: func(path string, mode os.FileMode) error {
			modes[path] = mode
			return nil
		},
		Chown: func(path string, uid int, gid int) error {
			owners[path] = [2]int{uid, gid}
			return nil
		},
	}

	err := RepairSubMetadata(cfg, 1000, 1001, fixer)

	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o750), modes[filepath.Join(baseDir, "sub")])
	require.Equal(t, os.FileMode(0o750), modes[filepath.Join(baseDir, "sub", "inputs")])
	require.Equal(t, os.FileMode(0o750), modes[filepath.Join(baseDir, "sub", "inputs", "manual")])
	require.Equal(t, os.FileMode(0o640), modes[filepath.Join(baseDir, "sub", "config.yaml")])
	require.Equal(t, os.FileMode(0o640), modes[filepath.Join(baseDir, "sub", "inputs", "manual", "usa.yaml")])
	require.Equal(t, [2]int{1000, 1001}, owners[filepath.Join(baseDir, "sub", "inputs", "manual", "usa.yaml")])
}
