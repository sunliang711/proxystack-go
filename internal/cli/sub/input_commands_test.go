package sub

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestInputListShowsInputSummaries 验证 input list 会列出 inputs 下的文件摘要。
func TestInputListShowsInputSummaries(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "list"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Subscription inputs: count=1")
	require.Contains(t, output.String(), "file=manual.yaml")
	require.Contains(t, output.String(), "source=manual")
	require.Contains(t, output.String(), "nodes=1")
	require.Contains(t, output.String(), "users=alice")
}

// TestInputShowRedactsSecretsByDefault 验证默认摘要输出会脱敏 password。
func TestInputShowRedactsSecretsByDefault(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "show", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "password: '[REDACTED]'")
	require.NotContains(t, output.String(), "demo-pass")
}

// TestInputShowRawPrintsOriginalContent 验证 --raw 会输出原始 input 文件内容。
func TestInputShowRawPrintsOriginalContent(t *testing.T) {
	baseDir := t.TempDir()
	original := writeSubInputFixture(t, baseDir, "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "show", "manual.yaml", "--raw"})

	err := command.Execute()

	require.NoError(t, err)
	require.Equal(t, string(original), output.String())
	require.Contains(t, output.String(), "demo-pass")
}

// TestInputShowSecretsPrintsSummarySecrets 验证 --show-secrets 会在摘要输出中保留敏感值。
func TestInputShowSecretsPrintsSummarySecrets(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "show", "manual", "--show-secrets"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "password: demo-pass")
}

// TestInputValidateSupportsSingleAndAll 验证 validate 可校验单文件并用合并逻辑发现全量重复节点。
func TestInputValidateSupportsSingleAndAll(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	writeSubInputFixture(t, baseDir, "copy.yaml")
	singleCommand := NewRootCommand()
	var singleOutput bytes.Buffer
	singleCommand.SetOut(&singleOutput)
	singleCommand.SetArgs([]string{"--base-dir", baseDir, "input", "validate", "manual"})

	err := singleCommand.Execute()

	require.NoError(t, err)
	require.Contains(t, singleOutput.String(), "Subscription input OK: file=manual.yaml")

	allCommand := NewRootCommand()
	allCommand.SetArgs([]string{"--base-dir", baseDir, "input", "validate"})

	err = allCommand.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
	require.Contains(t, err.Error(), "copy.yaml")
	require.Contains(t, err.Error(), "manual.yaml")
}

// TestInputEditRejectsInvalidContent 验证编辑后的 input 非法时不会覆盖原文件并保留草稿。
func TestInputEditRejectsInvalidContent(t *testing.T) {
	baseDir := t.TempDir()
	original := writeSubInputFixture(t, baseDir, "manual.yaml")
	editorPath := writeEditorScript(t, `cat > "$1" <<'EOF'
input_version: 1
source: manual
nodes: []
EOF
`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "edit", "manual", "--editor", editorPath})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "input.generated_at is required")
	require.Contains(t, err.Error(), "draft preserved: "+filepath.Join(baseDir, "inputs", "manual.yaml.draft"))
	data, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Equal(t, original, data)
	draft, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml.draft"))
	require.NoError(t, err)
	require.Contains(t, string(draft), "nodes: []")
}

// TestInputEditRejectsDuplicateProxyName 验证 edit 保存前会执行单文件合并校验。
func TestInputEditRejectsDuplicateProxyName(t *testing.T) {
	baseDir := t.TempDir()
	original := writeSubInputFixture(t, baseDir, "manual.yaml")
	editorPath := writeEditorScript(t, `cat > "$1" <<'EOF'
input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: manual:relay-a
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24001
    tag: socks5:24001:relay-a
    remark: Duplicate Relay
  - id: manual:relay-b
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24002
    tag: socks5:24002:relay-b
    remark: Duplicate Relay
EOF
`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "edit", "manual", "--editor", editorPath})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate proxy name for user")
	data, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Equal(t, original, data)
}

// TestInputEditUpdatesValidContent 验证编辑后的 input 合法时才写回原文件。
func TestInputEditUpdatesValidContent(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	inputPath := filepath.Join(baseDir, "inputs", "manual.yaml")
	require.NoError(t, os.Chmod(inputPath, 0o600))
	editorPath := writeEditorScript(t, `cat > "$1" <<'EOF'
input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: manual:relay
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24001
    tag: socks5:24001:relay
    remark: Edited Relay
    udp: true
    auth:
      type: password
      username: demo-user
      password: edited-pass
EOF
`)
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "edit", "manual", "--editor", editorPath})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Input updated:")
	data, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "Edited Relay")
	require.Contains(t, string(data), "edited-pass")
	info, err := os.Stat(inputPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestInputCloneEditsAndWritesValidTarget 验证 clone 会进入编辑器并只在全量校验通过后写入目标文件。
func TestInputCloneEditsAndWritesValidTarget(t *testing.T) {
	baseDir := t.TempDir()
	original := writeSubInputFixture(t, baseDir, "manual.yaml")
	editorPath := writeEditorScript(t, `cat > "$1" <<'EOF'
input_schema: proxystack.subscription-input
input_version: 1
source: copy
generated_at: "2026-06-05T12:00:00+08:00"
nodes:
  - id: copy:relay
    user: alice
    protocol: socks5
    server: proxy.example.com
    port: 24001
    tag: socks5:24001:relay
    remark: Copy Relay
    udp: true
    auth:
      type: password
      username: demo-user
      password: demo-pass
EOF
`)
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "clone", "manual", "copy", "--editor", editorPath})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Input cloned: manual.yaml -> copy.yaml")
	data, err := os.ReadFile(filepath.Join(baseDir, "inputs", "copy.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "source: copy")
	require.Contains(t, string(data), "id: copy:relay")
	require.Contains(t, string(data), "remark: Copy Relay")
	current, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Equal(t, original, current)
}

// TestInputCloneRejectsUneditedDuplicateWithoutWrite 验证未修改关键字段的 clone 不会落盘并保留草稿。
func TestInputCloneRejectsUneditedDuplicateWithoutWrite(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	editorPath := writeEditorScript(t, `: "$1"`)
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "clone", "manual", "copy", "--editor", editorPath})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
	require.Contains(t, err.Error(), "draft preserved: "+filepath.Join(baseDir, "inputs", "copy.yaml.draft"))
	require.NoFileExists(t, filepath.Join(baseDir, "inputs", "copy.yaml"))
	require.FileExists(t, filepath.Join(baseDir, "inputs", "copy.yaml.draft"))
}

// TestInputCloneRejectsExistingTarget 验证 clone 不会覆盖已有 input。
func TestInputCloneRejectsExistingTarget(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	writeSubInputVariant(t, baseDir, "copy.yaml")
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "clone", "manual", "copy"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "subscription input target already exists: copy.yaml")
}

// TestInputSetHostUpdatesSingleFile 验证 set-host 可修改单个 input 的所有节点 server。
func TestInputSetHostUpdatesSingleFile(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	inputPath := filepath.Join(baseDir, "inputs", "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Input host updated: manual.yaml")
	data, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "server: edge.example.com")
	require.NotContains(t, string(data), "server: proxy.example.com")
}

// TestInputSetHostUpdatesExistingExternalHost 验证 set-host 会同步更新已有文件级 external_host。
func TestInputSetHostUpdatesExistingExternalHost(t *testing.T) {
	baseDir := t.TempDir()
	inputDir := filepath.Join(baseDir, "inputs")
	data := writeSubInputData(t, inputDir, "manual.yaml")
	data = bytes.ReplaceAll(data, []byte("generated_at: \"2026-06-05T12:00:00+08:00\"\nnodes:"), []byte("generated_at: \"2026-06-05T12:00:00+08:00\"\nexternal_host: proxy.example.com\nnodes:"))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "manual.yaml"), data, 0o640))
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	updated, err := os.ReadFile(filepath.Join(inputDir, "manual.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(updated), "external_host: edge.example.com")
	require.Contains(t, string(updated), "server: edge.example.com")
	require.NotContains(t, string(updated), "proxy.example.com")
}

// TestInputSetHostWritesDefaultedServer 验证 set-host 会把 external_host 默认出来的 server 写回节点。
func TestInputSetHostWritesDefaultedServer(t *testing.T) {
	baseDir := t.TempDir()
	inputDir := filepath.Join(baseDir, "inputs")
	inputPath := filepath.Join(inputDir, "manual.yaml")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	require.NoError(t, os.WriteFile(inputPath, []byte(`input_schema: proxystack.subscription-input
input_version: 1
source: manual
generated_at: "2026-06-05T12:00:00+08:00"
external_host: proxy.example.com
nodes:
  - id: manual:relay
    user: alice
    protocol: socks5
    port: 24001
    tag: socks5:24001:relay
    remark: Manual Relay
`), 0o640))
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	updated, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), "external_host: edge.example.com")
	require.Contains(t, string(updated), "server: edge.example.com")
}

// TestInputSetHostUpdatesJSONInput 验证 set-host 支持 JSON input 的规范回写。
func TestInputSetHostUpdatesJSONInput(t *testing.T) {
	baseDir := t.TempDir()
	inputDir := filepath.Join(baseDir, "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	input, err := subgen.LoadInputFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	data, err := json.MarshalIndent(input, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "manual.json"), append(data, '\n'), 0o640))
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "manual.json"})

	err = command.Execute()

	require.NoError(t, err)
	updated, err := os.ReadFile(filepath.Join(inputDir, "manual.json"))
	require.NoError(t, err)
	require.Contains(t, string(updated), `"server": "edge.example.com"`)
	require.NotContains(t, string(updated), "proxy.example.com")
}

// TestInputSetHostAllUpdatesSafeInputFiles 验证 --all 会扫描并修改全部安全 input 文件。
func TestInputSetHostAllUpdatesSafeInputFiles(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	writeSubInputVariant(t, baseDir, "copy.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "--all"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Input hosts updated: changed=2 unchanged=0 total=2")
	manualData, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(manualData), "server: edge.example.com")
	copyData, err := os.ReadFile(filepath.Join(baseDir, "inputs", "copy.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(copyData), "server: edge.example.com")
}

// TestInputSetHostRejectsInvalidArguments 验证 set-host 的 HOST、SOURCE 和 --all 参数约束。
func TestInputSetHostRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "empty host",
			args: []string{"--base-dir", t.TempDir(), "input", "set-host", "  ", "--all"},
			want: "input host must not be empty",
		},
		{
			name: "missing source or all",
			args: []string{"--base-dir", t.TempDir(), "input", "set-host", "edge.example.com"},
			want: "SOURCE or --all is required",
		},
		{
			name: "source with all",
			args: []string{"--base-dir", t.TempDir(), "input", "set-host", "edge.example.com", "manual", "--all"},
			want: "SOURCE and --all cannot be used together",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetArgs(tt.args)

			err := command.Execute()

			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestInputSetHostRejectsMergeFailureWithoutWrite 验证全量合并校验失败时不会写回任一 input。
func TestInputSetHostRejectsMergeFailureWithoutWrite(t *testing.T) {
	baseDir := t.TempDir()
	manualOriginal := writeSubInputFixture(t, baseDir, "manual.yaml")
	copyOriginal := writeSubInputFixture(t, baseDir, "copy.yaml")
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "edge.example.com", "--all"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
	manualData, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Equal(t, manualOriginal, manualData)
	copyData, err := os.ReadFile(filepath.Join(baseDir, "inputs", "copy.yaml"))
	require.NoError(t, err)
	require.Equal(t, copyOriginal, copyData)
}

// TestInputSetHostReportsUnchanged 验证没有实际变化时命令成功并输出 unchanged。
func TestInputSetHostReportsUnchanged(t *testing.T) {
	baseDir := t.TempDir()
	original := writeSubInputFixture(t, baseDir, "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "set-host", "proxy.example.com", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "Input host unchanged: manual.yaml")
	data, err := os.ReadFile(filepath.Join(baseDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Equal(t, original, data)
}

// TestInputRemoveRejectsPathTraversal 验证 remove 不允许 SOURCE 逃出 inputs 目录。
func TestInputRemoveRejectsPathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n"), 0o640))
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "remove", "../config.yaml"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe subscription input source")
	require.FileExists(t, configPath)
}

// TestInputRemoveDeletesResolvedInputFile 验证 remove 只删除解析出的 input 文件。
func TestInputRemoveDeletesResolvedInputFile(t *testing.T) {
	baseDir := t.TempDir()
	writeSubInputFixture(t, baseDir, "manual.yaml")
	inputPath := filepath.Join(baseDir, "inputs", "manual.yaml")
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--base-dir", baseDir, "input", "remove", "manual"})

	err := command.Execute()

	require.NoError(t, err)
	require.NoFileExists(t, inputPath)
	require.Contains(t, output.String(), "Input removed: manual.yaml")
}

// TestInputCommandsRejectSymlinkInputDir 验证 inputs 目录符号链接不会被当作合法目录。
func TestInputCommandsRejectSymlinkInputDir(t *testing.T) {
	baseDir := t.TempDir()
	externalDir := t.TempDir()
	writeSubInputData(t, externalDir, "manual.yaml")
	require.NoError(t, os.MkdirAll(baseDir, 0o750))
	if err := os.Symlink(externalDir, filepath.Join(baseDir, "inputs")); err != nil {
		t.Skipf("symlink is not available: %v", err)
	}
	command := NewRootCommand()
	command.SetArgs([]string{"--base-dir", baseDir, "input", "show", "manual"})

	err := command.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "input directory must not be a symlink")
}

// TestInputCommandsRejectSymlinkInputFile 验证单文件命令不会跟随 input 文件符号链接。
func TestInputCommandsRejectSymlinkInputFile(t *testing.T) {
	baseDir := t.TempDir()
	externalDir := t.TempDir()
	writeSubInputData(t, externalDir, "manual.yaml")
	externalPath := filepath.Join(externalDir, "manual.yaml")
	inputDir := filepath.Join(baseDir, "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	if err := os.Symlink(externalPath, filepath.Join(inputDir, "manual.yaml")); err != nil {
		t.Skipf("symlink is not available: %v", err)
	}
	showCommand := NewRootCommand()
	showCommand.SetArgs([]string{"--base-dir", baseDir, "input", "show", "manual.yaml"})

	err := showCommand.Execute()

	require.Error(t, err)
	require.Contains(t, err.Error(), "subscription input must be a regular file")

	validateCommand := NewRootCommand()
	var output bytes.Buffer
	validateCommand.SetOut(&output)
	validateCommand.SetArgs([]string{"--base-dir", baseDir, "input", "validate"})

	err = validateCommand.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "sources=0 nodes=0 users=0")
}

// writeSubInputFixture 写入测试订阅 input，并返回原始内容。
func writeSubInputFixture(t *testing.T, baseDir string, name string) []byte {
	t.Helper()
	inputDir := filepath.Join(baseDir, "inputs")
	return writeSubInputData(t, inputDir, name)
}

// writeSubInputData 向指定目录写入测试订阅 input，并返回原始内容。
func writeSubInputData(t *testing.T, inputDir string, name string) []byte {
	t.Helper()
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	data, err := os.ReadFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, name), data, 0o640))
	return data
}

// writeSubInputVariant 写入与 manual 不冲突的第二个测试 input。
func writeSubInputVariant(t *testing.T, baseDir string, name string) []byte {
	t.Helper()
	inputDir := filepath.Join(baseDir, "inputs")
	data := writeSubInputData(t, inputDir, name)
	data = bytes.ReplaceAll(data, []byte("source: manual"), []byte("source: copy"))
	data = bytes.ReplaceAll(data, []byte("manual:relay"), []byte("copy:relay"))
	data = bytes.ReplaceAll(data, []byte("proxy.example.com"), []byte("copy.example.com"))
	data = bytes.ReplaceAll(data, []byte("Manual Relay"), []byte("Copy Relay"))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, name), data, 0o640))
	return data
}
