package sub

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/eagle/proxystack-go/internal/graph"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// TestHelpUsesCommandGroupsAndBaseDirOnly 验证 ps-sub usage 分组展示且只暴露 base-dir 路径入口。
func TestHelpUsesCommandGroupsAndBaseDirOnly(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--help"})

	err := command.Execute()

	require.NoError(t, err)
	helpText := output.String()
	require.Contains(t, helpText, "初始化")
	require.Contains(t, helpText, "配置管理")
	require.Contains(t, helpText, "订阅数据")
	require.Contains(t, helpText, "服务控制")
	require.Contains(t, helpText, "诊断工具")
	require.Contains(t, helpText, "其它")
	require.Contains(t, helpText, "  init")
	require.Contains(t, helpText, "  config")
	require.Contains(t, helpText, "  import")
	require.Contains(t, helpText, "  input")
	require.Contains(t, helpText, "  serve")
	require.Contains(t, helpText, "  doctor")
	require.Contains(t, helpText, "  version")
	require.Contains(t, helpText, "--base-dir")
	require.Contains(t, helpText, "/opt/proxystack-sub")
	require.NotContains(t, helpText, "--config")
	require.NotContains(t, helpText, "--data-dir")
	require.NotContains(t, helpText, "Available Commands:")
	require.NotContains(t, helpText, "Additional Commands:")
}

// TestInitCommandCreatesSubLayout 验证 ps-sub init 只创建独立运行目录和默认配置。
func TestInitCommandCreatesSubLayout(t *testing.T) {
	baseDir := t.TempDir()
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "init"})

	err := command.Execute()

	require.NoError(t, err)
	require.DirExists(t, baseDir)
	require.DirExists(t, filepath.Join(baseDir, "inputs"))
	require.DirExists(t, filepath.Join(baseDir, "templates"))
	require.FileExists(t, filepath.Join(baseDir, "config.yaml"))
	require.NoDirExists(t, filepath.Join(baseDir, "sub"))
	require.NoDirExists(t, filepath.Join(baseDir, "runtime"))
	require.Contains(t, output.String(), "template_dir="+filepath.Join(baseDir, "templates"))
	require.Contains(t, output.String(), "created_config=true")
	data, err := os.ReadFile(filepath.Join(baseDir, "config.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "# ps-sub 订阅服务配置。")
	require.Contains(t, string(data), "# HTTP 监听地址")
	require.Contains(t, string(data), "# 日志输出设置。")
	require.Contains(t, string(data), "format: json")
	require.Contains(t, string(data), "# HTTP 访问控制。")
	require.Contains(t, string(data), "# Surge managed config 输出设置。")
	subConfig, err := config.LoadSubServerConfig(filepath.Join(baseDir, "config.yaml"))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:3003", subConfig.Listen)
	require.Equal(t, config.LogFormatJSON, subConfig.Log.Format)
}

// TestConfigureSubLoggerSupportsJSONAndConsole 验证 ps-sub serve 可按配置切换日志格式。
func TestConfigureSubLoggerSupportsJSONAndConsole(t *testing.T) {
	originalLogger := log.Logger
	originalTimeFormat := zerolog.TimeFieldFormat
	t.Cleanup(func() {
		log.Logger = originalLogger
		zerolog.TimeFieldFormat = originalTimeFormat
	})

	tests := []struct {
		name          string
		format        string
		wantContains  string
		wantNotOutput string
	}{
		{name: "json", format: config.LogFormatJSON, wantContains: `"message":"logger format test"`},
		{name: "console", format: config.LogFormatConsole, wantContains: "logger format test", wantNotOutput: `"message":"logger format test"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			err := configureSubLogger(config.LogConfig{Format: tt.format}, &output)
			require.NoError(t, err)
			log.Info().Str("format", tt.format).Msg("logger format test")

			require.Contains(t, output.String(), tt.wantContains)
			if tt.wantNotOutput != "" {
				require.NotContains(t, output.String(), tt.wantNotOutput)
			}
		})
	}
}

// TestConfigureSubLoggerRejectsUnknownFormat 验证未知日志格式会在启动前失败。
func TestConfigureSubLoggerRejectsUnknownFormat(t *testing.T) {
	var output bytes.Buffer

	err := configureSubLogger(config.LogConfig{Format: "text"}, &output)

	require.Error(t, err)
	require.Contains(t, err.Error(), "log.format must be json or console")
}

// TestInitCommandKeepsExistingConfig 验证 ps-sub init 默认不覆盖既有 sub config。
func TestInitCommandKeepsExistingConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	original := []byte("listen: 127.0.0.1:39003\naccess:\n  type: none\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, original, 0o640))
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "init"})

	err := command.Execute()

	require.NoError(t, err)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, original, data)
	require.Contains(t, output.String(), "created_config=false")
}

// TestInitCommandForceOverwritesConfig 验证 --force 会重写默认 sub config。
func TestInitCommandForceOverwritesConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("listen: 127.0.0.1:39003\naccess:\n  type: none\n"), 0o600))
	before, err := os.Stat(configPath)
	require.NoError(t, err)
	beforeUID, beforeGID, hasOwner := fileOwnerIDs(before)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "init", "--force"})

	err = command.Execute()

	require.NoError(t, err)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, config.DefaultSubServerConfigYAML(), string(data))
	after, err := os.Stat(configPath)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
	if hasOwner {
		afterUID, afterGID, ok := fileOwnerIDs(after)
		require.True(t, ok)
		require.Equal(t, beforeUID, afterUID)
		require.Equal(t, beforeGID, afterGID)
	}
}

// TestConfigShowUsesBaseDirSubConfig 验证 config show 使用 base-dir 下固定 config.yaml。
func TestConfigShowUsesBaseDirSubConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte(`access:
  type: token
  token: from-base-dir
`), 0o644))
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "config", "show"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "token: '[REDACTED]'")
	require.NotContains(t, output.String(), "data_dir:")
}

// TestConfigShowCanPrintSecrets 验证 --show-secrets 可打印完整 token。
func TestConfigShowCanPrintSecrets(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: token\n  token: from-base-dir\n"), 0o640))
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "config", "show", "--show-secrets"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "token: from-base-dir")
}

// TestConfigShowRedactsUnusedToken 验证默认 show 会脱敏误写在 none 模式下的 token。
func TestConfigShowRedactsUnusedToken(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n  token: unused-secret\n"), 0o640))
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "config", "show"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "token: '[REDACTED]'")
	require.NotContains(t, output.String(), "unused-secret")
}

// TestConfigCommandEditsAndValidates 验证 config 编辑后会校验并写回 config.yaml。
func TestConfigCommandEditsAndValidates(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n"), 0o640))
	editorPath := writeEditorScript(t, `cat > "$1" <<'EOF'
listen: 127.0.0.1:39003
access:
  type: none
EOF
`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "config", "--editor", editorPath})

	err := command.Execute()

	require.NoError(t, err)
	subConfig, err := config.LoadSubServerConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:39003", subConfig.Listen)
}

// TestConfigCommandSupportsQuotedEditorPath 验证带空格路径的编辑器可通过引号传入。
func TestConfigCommandSupportsQuotedEditorPath(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n"), 0o640))
	editorPath := writeEditorScriptInDir(t, filepath.Join(t.TempDir(), "editor dir"), `cat > "$1" <<'EOF'
listen: 127.0.0.1:39004
access:
  type: none
EOF
`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "config", "--editor", `"` + editorPath + `"`})

	err := command.Execute()

	require.NoError(t, err)
	subConfig, err := config.LoadSubServerConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:39004", subConfig.Listen)
}

// TestConfigCommandRejectsInvalidEdit 验证编辑结果非法时不覆盖原配置。
func TestConfigCommandRejectsInvalidEdit(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	original := []byte("access:\n  type: none\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, original, 0o640))
	editorPath := writeEditorScript(t, `printf 'listen: bad-listen\n' > "$1"`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "config", "--editor", editorPath})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "listen must use host:port format")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, original, data)
}

// TestWriteTextFileIfChangedPreservesMetadata 验证原子替换会保留原文件 mode 和 owner。
func TestWriteTextFileIfChangedPreservesMetadata(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n"), 0o640))
	before, err := os.Stat(configPath)
	require.NoError(t, err)
	beforeUID, beforeGID, hasOwner := fileOwnerIDs(before)

	changed, err := writeTextFileIfChanged(configPath, []byte("listen: 127.0.0.1:39005\naccess:\n  type: none\n"))

	require.NoError(t, err)
	require.True(t, changed)
	after, err := os.Stat(configPath)
	require.NoError(t, err)
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
	if hasOwner {
		afterUID, afterGID, ok := fileOwnerIDs(after)
		require.True(t, ok)
		require.Equal(t, beforeUID, afterUID)
		require.Equal(t, beforeGID, afterGID)
	}
}

// TestConfigCheckRequiresConfigFile 验证 check 不会把缺失配置当作默认配置通过。
func TestConfigCheckRequiresConfigFile(t *testing.T) {
	baseDir := t.TempDir()
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "config", "check"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "Sub config could not be read")
}

// TestServeHostPortOverride 验证 serve 的 --host/--port 会覆盖最终监听地址。
func TestServeHostPortOverride(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte(`listen: 127.0.0.1:3003
access:
  type: token
  token: demo-token
`), 0o644))
	command := NewRootCommand()
	require.NoError(t, command.PersistentFlags().Set("base-dir", baseDir))
	serveCommand, _, err := command.Find([]string{"serve"})
	require.NoError(t, err)
	require.NoError(t, serveCommand.Flags().Set("host", "0.0.0.0"))
	require.NoError(t, serveCommand.Flags().Set("port", "39003"))

	subConfig, err := loadRuntimeConfig(serveCommand)

	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:39003", subConfig.Listen)
	require.Equal(t, "token", subConfig.Access.Type)
	require.Equal(t, baseDir, subConfig.DataDir)
}

// TestImportAndClearUseBaseDirSubInputs 验证 import/clear 固定操作 base-dir 下的 inputs。
func TestImportAndClearUseBaseDirSubInputs(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(baseDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte("access:\n  type: none\n"), 0o640))
	bundlePath := writeTestBundle(t)

	importCommand := NewRootCommand()
	importCommand.SetArgs([]string{"--base-dir", baseDir, "import", bundlePath})
	require.NoError(t, importCommand.Execute())
	inputPath := filepath.Join(baseDir, "inputs", "manual.yaml")
	require.FileExists(t, inputPath)

	clearCommand := NewRootCommand()
	clearCommand.SetArgs([]string{"--base-dir", baseDir, "clear"})
	require.NoError(t, clearCommand.Execute())
	require.NoFileExists(t, inputPath)
}

// TestServiceInstallUsesSubOnlyConfig 验证 ps-sub service install 只用 base-dir 渲染 sub 服务。
func TestServiceInstallUsesSubOnlyConfig(t *testing.T) {
	baseDir := t.TempDir()
	manager := &fakeSubManager{installPaths: []string{"/tmp/proxystack-sub.service"}}
	withSubServiceManager(t, manager)
	var repairedConfig domain.GlobalConfig
	withSubRepairServiceMetadata(t, func(cfg domain.GlobalConfig) error {
		repairedConfig = cfg
		return nil
	})
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "service", "install"})

	err := command.Execute()

	require.NoError(t, err)
	require.Equal(t, "sub", manager.installedTarget)
	require.Equal(t, baseDir, manager.installedConfig.BaseDir)
	require.Equal(t, ".", manager.installedConfig.Paths.Sub)
	require.Equal(t, baseDir, repairedConfig.BaseDir)
	require.Equal(t, ".", repairedConfig.Paths.Sub)
	require.NoFileExists(t, filepath.Join(baseDir, "config.yaml"))
	require.Contains(t, output.String(), "Installed units:")
}

// TestLifecycleCommandsUseSubServiceOnly 验证 ps-sub 生命周期命令固定只操作订阅服务。
func TestLifecycleCommandsUseSubServiceOnly(t *testing.T) {
	actions := []string{"start", "stop", "restart", "enable", "disable"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			manager := &fakeSubManager{}
			withSubServiceManager(t, manager)
			repairCalls := 0
			withSubRepairServiceMetadata(t, func(cfg domain.GlobalConfig) error {
				repairCalls++
				return nil
			})
			command := NewRootCommand()
			command.SetArgs([]string{action})

			err := command.Execute()

			require.NoError(t, err)
			require.Equal(t, action, manager.action)
			require.Equal(t, []string{"proxystack-sub.service"}, manager.services)
			if action == "start" || action == "restart" {
				require.Equal(t, 1, repairCalls)
			} else {
				require.Zero(t, repairCalls)
			}
		})
	}
}

// TestStatusAndLogsForwardOutput 验证 status/logs 会转发服务管理器输出。
func TestStatusAndLogsForwardOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "status", args: []string{"status"}, want: "status output"},
		{name: "logs", args: []string{"logs", "-f"}, want: "logs output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeSubManager{statusResult: servicemanager.Result{Stdout: "status output"}, logsResult: servicemanager.Result{Stdout: "logs output"}}
			withSubServiceManager(t, manager)
			command := NewRootCommand()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetArgs(tt.args)

			err := command.Execute()

			require.NoError(t, err)
			require.Contains(t, output.String(), tt.want)
		})
	}
}

// TestRejectsRemovedPathFlags 验证 ps-sub 不再接受旧版路径 flag。
func TestRejectsRemovedPathFlags(t *testing.T) {
	tests := [][]string{
		{"--config", filepath.Join(t.TempDir(), "config.yaml"), "version"},
		{"--data-dir", t.TempDir(), "version"},
	}
	for _, args := range tests {
		command := NewRootCommand()
		command.SetArgs(args)

		err := command.Execute()

		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown flag")
	}
}

// writeTestBundle 写入包含 manual input 的测试订阅包。
func writeTestBundle(t *testing.T) string {
	t.Helper()
	inputData, err := os.ReadFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	bundlePath := filepath.Join(t.TempDir(), "sub-bundle.zip")
	_, err = subgen.WriteBundle(bundlePath, "manual", "2026-06-05T12:00:00+08:00", []subgen.BundleInputFile{{Name: "manual.yaml", Content: inputData}})
	require.NoError(t, err)
	return bundlePath
}

// writeEditorScript 写入测试编辑器脚本，用于模拟用户保存配置。
func writeEditorScript(t *testing.T, body string) string {
	t.Helper()
	return writeEditorScriptInDir(t, t.TempDir(), body)
}

// writeEditorScriptInDir 在指定目录写入测试编辑器脚本。
func writeEditorScriptInDir(t *testing.T, dir string, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, "editor.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return path
}

// withSubServiceManager 注入 fake 服务管理器并在测试结束后恢复。
func withSubServiceManager(t *testing.T, manager servicemanager.Manager) {
	t.Helper()
	original := subServiceManagerFactory
	subServiceManagerFactory = func(kind string, opts ...servicemanager.ManagerOption) (servicemanager.Manager, error) {
		return manager, nil
	}
	t.Cleanup(func() {
		subServiceManagerFactory = original
	})
}

// withSubRepairServiceMetadata 注入 fake metadata 修复器并在测试结束后恢复。
func withSubRepairServiceMetadata(t *testing.T, repair func(domain.GlobalConfig) error) {
	t.Helper()
	original := subRepairServiceMetadataFunc
	subRepairServiceMetadataFunc = repair
	t.Cleanup(func() {
		subRepairServiceMetadataFunc = original
	})
}

type fakeSubManager struct {
	installedConfig domain.GlobalConfig
	installedTarget string
	installPaths    []string
	action          string
	services        []string
	statusResult    servicemanager.Result
	logsResult      servicemanager.Result
}

// InstallUnits 记录 service install 的配置和 target。
func (f *fakeSubManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	f.installedConfig = config
	f.installedTarget = target
	return f.installPaths, nil
}

// UninstallUnits 满足 service.Manager 接口，当前 ps-sub 测试不会调用。
func (f *fakeSubManager) UninstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	return nil, nil
}

// Start 记录 start 操作。
func (f *fakeSubManager) Start(ctx context.Context, services []string) error {
	f.record("start", services)
	return nil
}

// Stop 记录 stop 操作。
func (f *fakeSubManager) Stop(ctx context.Context, services []string) error {
	f.record("stop", services)
	return nil
}

// Restart 记录 restart 操作。
func (f *fakeSubManager) Restart(ctx context.Context, services []string) error {
	f.record("restart", services)
	return nil
}

// Enable 记录 enable 操作。
func (f *fakeSubManager) Enable(ctx context.Context, services []string) error {
	f.record("enable", services)
	return nil
}

// Disable 记录 disable 操作。
func (f *fakeSubManager) Disable(ctx context.Context, services []string) error {
	f.record("disable", services)
	return nil
}

// IsActive 满足 service.Manager 接口。
func (f *fakeSubManager) IsActive(ctx context.Context, service string) (bool, error) {
	return false, nil
}

// Status 返回预设状态输出。
func (f *fakeSubManager) Status(ctx context.Context, services []string) (servicemanager.Result, error) {
	f.record("status", services)
	return f.statusResult, nil
}

// Logs 返回预设日志输出。
func (f *fakeSubManager) Logs(ctx context.Context, services []string, follow bool) (servicemanager.Result, error) {
	f.record("logs", services)
	return f.logsResult, nil
}

// ServiceForNode 满足 service.Manager 接口。
func (f *fakeSubManager) ServiceForNode(node graph.ServiceNode) string {
	return ""
}

// ServicesForNodes 满足 service.Manager 接口。
func (f *fakeSubManager) ServicesForNodes(nodes []graph.ServiceNode) []string {
	return nil
}

// SubService 返回订阅服务名。
func (f *fakeSubManager) SubService() string {
	return "proxystack-sub.service"
}

func (f *fakeSubManager) record(action string, services []string) {
	f.action = action
	f.services = append([]string(nil), services...)
}
