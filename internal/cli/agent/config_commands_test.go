package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"github.com/stretchr/testify/require"
)

// TestAgentListOutputsReferenceTable 验证 list 输出与 Python 版保持同样的展开表格格式。
func TestAgentListOutputsReferenceTable(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "list")

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	require.True(t, strings.HasPrefix(lines[0], "Name  Role"))
	require.Contains(t, lines[0], "Enabled")
	require.Contains(t, lines[0], "Component")
	require.Contains(t, lines[0], "Running")
	require.Contains(t, lines[0], "Generated")
	require.Contains(t, lines[0], "Ports")
	require.Contains(t, output, "usa1")
	require.Contains(t, output, "xrelay")
	require.Contains(t, output, "clash")
	require.Contains(t, output, "socks5:24000(L),vmess:24100(*)")
	require.Contains(t, output, "socks:17090(L) | http:18090(L)")
	require.NotContains(t, output, "enabled=true")
	require.Equal(t, listPortScopeNote, lines[len(lines)-1])

	verboseOutput := runAgentCommandForTest(t, "--base-dir", baseDir, "list", "--verbose")

	require.Contains(t, verboseOutput, "Endpoints")
	require.Contains(t, verboseOutput, "inbounds: socks5:24000(L),vmess:24100(*) | api:10085(L)")
	require.Contains(t, verboseOutput, "socks:17090(L) | http:18090(L) | controller:19090(L)")
}

// TestAgentConfigUsesTemporaryEditTarget 验证 config [name] 传给编辑器的是临时文件而不是真实 stack 文件。
func TestAgentConfigUsesTemporaryEditTarget(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	markerPath := filepath.Join(t.TempDir(), "edited-path.txt")
	editorPath := writeEditorScript(t, "printf '%s' \"$1\" > \""+markerPath+"\"\n")

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "config", "usa1", "--editor", editorPath)

	editedPathBytes, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	editedPath := string(editedPathBytes)
	require.NotEqual(t, stackPath, editedPath)
	require.Contains(t, editedPath, "proxystack-edit-")
	require.Contains(t, output, "Config OK: "+stackPath)
}

// TestAgentConfigRejectsInvalidStackBeforeReplacing 验证非法临时编辑不会污染真实 stack 文件。
func TestAgentConfigRejectsInvalidStackBeforeReplacing(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	before, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	editorPath := writeEditorScript(t, "printf 'name: [\\n' > \"$1\"\n")

	_, err = runAgentCommandForTestError("--base-dir", baseDir, "config", "usa1", "--editor", editorPath)

	require.Error(t, err)
	after, readErr := os.ReadFile(stackPath)
	require.NoError(t, readErr)
	require.Equal(t, before, after)
}

// TestAgentConfigRepairsServiceMetadataBeforeRestart 验证 config NAME 自动重启前会修复 runtime owner/mode。
func TestAgentConfigRepairsServiceMetadataBeforeRestart(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "bin", "xray"), []byte("#!/bin/sh\n"), 0o750))
	calls := make([]string, 0)
	manager := &fakeConfigRestartManager{
		active:     map[string]bool{"proxystack-xray@usa1.service": true},
		recordCall: func(name string) { calls = append(calls, name) },
	}
	withAgentServiceManager(t, manager)
	oldRepair := repairServiceMetadataFunc
	repairServiceMetadataFunc = func(cfg domain.GlobalConfig) error {
		calls = append(calls, "repair")
		require.Equal(t, baseDir, cfg.BaseDir)
		return nil
	}
	t.Cleanup(func() {
		repairServiceMetadataFunc = oldRepair
	})
	editorPath := writeEditorScript(t, "printf '\\n# edited by test\\n' >> \"$1\"\n")

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "config", "usa1", "--editor", editorPath)

	require.Equal(t, []string{"repair", "restart"}, calls)
	require.Equal(t, []string{"proxystack-xray@usa1.service"}, manager.restarted)
	require.Contains(t, output, "Restarted active services: [proxystack-xray@usa1.service]")
}

// TestAgentAddRepairsServiceMetadata 验证 root 写入 stack 后会触发标准 metadata 修复。
func TestAgentAddRepairsServiceMetadata(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	oldRepair := repairServiceMetadataFunc
	repairedBaseDirs := make([]string, 0)
	repairServiceMetadataFunc = func(cfg domain.GlobalConfig) error {
		repairedBaseDirs = append(repairedBaseDirs, cfg.BaseDir)
		return nil
	}
	t.Cleanup(func() {
		repairServiceMetadataFunc = oldRepair
	})

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "add", "usa1", "--keep-template-ports")

	require.Contains(t, output, "Created stack: usa1")
	require.Equal(t, []string{baseDir}, repairedBaseDirs)
}

type fakeConfigRestartManager struct {
	fakeUninstallManager
	active     map[string]bool
	restarted  []string
	recordCall func(string)
}

// Restart 记录 config NAME 自动重启请求。
func (f *fakeConfigRestartManager) Restart(ctx context.Context, services []string) error {
	if f.recordCall != nil {
		f.recordCall("restart")
	}
	f.restarted = append([]string(nil), services...)
	return nil
}

// IsActive 按测试预设返回服务是否 active。
func (f *fakeConfigRestartManager) IsActive(ctx context.Context, service string) (bool, error) {
	return f.active[service], nil
}

// ServiceForNode 返回服务节点对应的 unit 名称。
func (f *fakeConfigRestartManager) ServiceForNode(node graph.ServiceNode) string {
	return node.ServiceName()
}

func runAgentCommandForTestError(args ...string) (string, error) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func writeEditorScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return path
}
