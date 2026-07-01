package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	backupgen "github.com/eagle/proxystack-go/internal/generator/backup"
	"github.com/eagle/proxystack-go/internal/systemd"
	"github.com/stretchr/testify/require"
)

// TestNativeBackupCommandsExportAndImport 验证 ps-agent 顶层 export/import 可完成原生备份闭环。
func TestNativeBackupCommandsExportAndImport(t *testing.T) {
	sourceDir := t.TempDir()
	sourceConfigPath := filepath.Join(sourceDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: sourceDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: sourceConfigPath, Name: "usa1", Template: "pair", AllocatePorts: true}))
	backupPath := filepath.Join(t.TempDir(), "proxystack-backup.zip")

	exportOutput := runAgentCommandForTest(t, "--base-dir", sourceDir, "export", "--output", backupPath)

	require.Contains(t, exportOutput, "Native backup exported:")
	require.FileExists(t, backupPath)
	requireBackupConfigWithoutBaseDir(t, backupPath)

	targetDir := t.TempDir()
	oldRepair := repairServiceMetadataFunc
	repairedBaseDirs := make([]string, 0)
	repairServiceMetadataFunc = func(cfg domain.GlobalConfig) error {
		repairedBaseDirs = append(repairedBaseDirs, cfg.BaseDir)
		return nil
	}
	t.Cleanup(func() {
		repairServiceMetadataFunc = oldRepair
	})
	importOutput := runAgentCommandForTest(t, "--base-dir", targetDir, "import", backupPath)

	require.Contains(t, importOutput, "Native backup imported:")
	importedConfig, err := config.LoadConfig(filepath.Join(targetDir, "config.yaml"))
	require.NoError(t, err)
	require.Equal(t, targetDir, importedConfig.BaseDir)
	require.FileExists(t, filepath.Join(targetDir, "stacks", "usa1.yaml"))
	require.Equal(t, []string{targetDir}, repairedBaseDirs)
}

// TestNativeBackupImportRequiresForce 验证 import 默认不会覆盖已有 agent 配置。
func TestNativeBackupImportRequiresForce(t *testing.T) {
	sourceDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: sourceDir, ExternalHost: "proxy.example.com"}))
	backupPath := filepath.Join(t.TempDir(), "proxystack-backup.zip")
	runAgentCommandForTest(t, "--base-dir", sourceDir, "export", "--output", backupPath)
	targetDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config.yaml"), []byte("version: 1\n"), 0o640))

	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--base-dir", targetDir, "import", backupPath})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "use --force")
}

// TestNativeBackupImportStopsRunningOldServices 验证 import 覆盖前会停止旧配置中正在运行的服务。
func TestNativeBackupImportStopsRunningOldServices(t *testing.T) {
	backupPath := createNativeBackupFixture(t, "new1")
	targetDir := t.TempDir()
	require.NoError(t, prepareNativeImportTarget(t, targetDir, "usa1"))
	manager := &fakeUninstallManager{active: map[string]bool{
		"proxystack-xray@usa1.service": true,
	}}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", targetDir, "import", backupPath, "--force")

	require.Contains(t, output, "Stopping running services before import:")
	require.Contains(t, output, "- usa1.xrelay -> proxystack-xray@usa1.service")
	require.NotContains(t, output, "- usa1.clash -> proxystack-clash@usa1.service")
	require.ElementsMatch(t, []string{"proxystack-xray@usa1.service", "proxystack-clash@usa1.service"}, manager.activeChecks)
	require.Equal(t, []string{"proxystack-xray@usa1.service"}, manager.stopped)
	require.Equal(t, []string{"stop"}, manager.calls)
	require.FileExists(t, filepath.Join(targetDir, "stacks", "new1.yaml"))
	require.NoFileExists(t, filepath.Join(targetDir, "stacks", "usa1.yaml"))
}

// TestNativeBackupImportReportsNoRunningOldServices 验证旧配置存在但没有运行服务时只打印提示。
func TestNativeBackupImportReportsNoRunningOldServices(t *testing.T) {
	backupPath := createNativeBackupFixture(t, "new1")
	targetDir := t.TempDir()
	require.NoError(t, prepareNativeImportTarget(t, targetDir, "usa1"))
	manager := &fakeUninstallManager{active: map[string]bool{}}
	withAgentServiceManager(t, manager)

	output := runAgentCommandForTest(t, "--base-dir", targetDir, "import", backupPath, "--force")

	require.Contains(t, output, "No running services to stop before import.")
	require.Empty(t, manager.stopped)
	require.Empty(t, manager.calls)
	require.FileExists(t, filepath.Join(targetDir, "stacks", "new1.yaml"))
}

// TestNativeBackupImportStopFailurePreventsRestore 验证停止运行服务失败时不会覆盖旧配置。
func TestNativeBackupImportStopFailurePreventsRestore(t *testing.T) {
	backupPath := createNativeBackupFixture(t, "new1")
	targetDir := t.TempDir()
	require.NoError(t, prepareNativeImportTarget(t, targetDir, "usa1"))
	manager := &fakeUninstallManager{
		active: map[string]bool{
			"proxystack-xray@usa1.service": true,
		},
		stopErr: errors.New("stop failed"),
	}
	withAgentServiceManager(t, manager)

	_, err := runAgentCommandForTestError("--base-dir", targetDir, "import", backupPath, "--force")

	require.Error(t, err)
	require.Contains(t, err.Error(), "stop running services before import failed")
	require.FileExists(t, filepath.Join(targetDir, "stacks", "usa1.yaml"))
	require.NoFileExists(t, filepath.Join(targetDir, "stacks", "new1.yaml"))
}

// TestAgentInitUsesGlobalBaseDir 验证全局 --base-dir 决定 config.yaml 和标准安装目录。
func TestAgentInitUsesGlobalBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	oldRunner := serviceAccountRunner
	oldOwnerIDs := serviceAccountOwnerIDsFunc
	serviceAccountRunner = &fakeServiceAccountRunner{results: map[string]systemd.Result{
		"getent group proxystack":  {ExitCode: 0},
		"getent passwd proxystack": {ExitCode: 0},
	}}
	serviceAccountOwnerIDsFunc = func() (int, int, error) {
		return os.Getuid(), os.Getgid(), nil
	}
	t.Cleanup(func() {
		serviceAccountRunner = oldRunner
		serviceAccountOwnerIDsFunc = oldOwnerIDs
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "init", "--external-host", "proxy.example.com")

	configPath := filepath.Join(baseDir, "config.yaml")
	require.Contains(t, output, "Initialized agent config: "+configPath)
	require.FileExists(t, configPath)
	require.DirExists(t, filepath.Join(baseDir, "bin"))
	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, baseDir, cfg.BaseDir)
	require.Equal(t, filepath.Join(baseDir, "bin"), cfg.ResolvePath(cfg.Paths.Bin))
	require.Equal(t, filepath.Join(baseDir, "geo"), cfg.ResolvePath(cfg.Paths.Geo))
	require.Equal(t, filepath.Join(baseDir, "stacks"), cfg.ResolvePath(cfg.Paths.Stacks))
	require.Equal(t, filepath.Join(baseDir, "runtime"), cfg.ResolvePath(cfg.Paths.Runtime))
	require.Equal(t, filepath.Join(baseDir, "runtime", "generated"), cfg.ResolvePath(cfg.Paths.Generated))
	require.Equal(t, filepath.Join(baseDir, "publish"), cfg.ResolvePath(cfg.Paths.Publish))
	require.Equal(t, filepath.Join(baseDir, "downloads"), cfg.ResolvePath(cfg.Paths.Downloads))
	require.Equal(t, filepath.Join(baseDir, "sub"), cfg.ResolvePath(cfg.Paths.Sub))
}

// TestAgentRejectsRemovedConfigFlag 验证 ps-agent 不再接受旧版 --config 入口。
func TestAgentRejectsRemovedConfigFlag(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "config.yaml"), "version"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown flag")
}

// TestSetupCommandIsRegistered 验证 ps-agent 命令树包含 setup 入口。
func TestSetupCommandIsRegistered(t *testing.T) {
	output := runAgentCommandForTest(t, "setup", "--help")

	require.Contains(t, output, "setup")
	require.Contains(t, output, "--base-dir")
	require.Contains(t, output, "--external-host")
	require.Contains(t, output, "--start")
}

// createNativeBackupFixture 创建只包含一个 stack 的原生备份包。
func createNativeBackupFixture(t *testing.T, stackName string) string {
	t.Helper()
	sourceDir := t.TempDir()
	require.NoError(t, prepareNativeImportTarget(t, sourceDir, stackName))
	backupPath := filepath.Join(t.TempDir(), "proxystack-backup.zip")
	runAgentCommandForTest(t, "--base-dir", sourceDir, "export", "--output", backupPath)
	return backupPath
}

// prepareNativeImportTarget 初始化一个带单个 stack 的 agent 配置目录。
func prepareNativeImportTarget(t *testing.T, baseDir string, stackName string) error {
	t.Helper()
	if err := agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}); err != nil {
		return err
	}
	return agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: filepath.Join(baseDir, "config.yaml"), Name: stackName, Template: "pair", AllocatePorts: true})
}

// requireBackupConfigWithoutBaseDir 验证原生备份里的 config 不包含旧版 base_dir。
func requireBackupConfigWithoutBaseDir(t *testing.T, backupPath string) {
	t.Helper()
	_, files, err := backupgen.ReadNativeBackup(backupPath)
	require.NoError(t, err)
	require.NotContains(t, string(files["config/config.yaml"]), "base_dir:")
}

// runAgentCommandForTest 执行 agent Cobra 命令并返回合并输出。
func runAgentCommandForTest(t *testing.T, args ...string) string {
	t.Helper()
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)

	err := command.Execute()

	require.NoError(t, err)
	return output.String()
}
