package agentconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/stretchr/testify/require"
)

// TestInitProjectDoesNotOverwriteWithoutForce 验证 init 默认不覆盖已有 config。
func TestInitProjectDoesNotOverwriteWithoutForce(t *testing.T) {
	baseDir := t.TempDir()

	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	err := InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigAlreadyExists)
	require.Contains(t, err.Error(), "config already exists")
}

// TestEnsureProjectLayoutDoesNotOverwriteConfig 验证补齐标准目录不会覆盖既有 agent 配置。
func TestEnsureProjectLayoutDoesNotOverwriteConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	configData := []byte("version: 1\nexternal_host: existing.example.com\n")
	require.NoError(t, os.WriteFile(configPath, configData, 0o640))

	require.NoError(t, EnsureProjectLayout(InitOptions{BaseDir: baseDir}))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, configData, data)
	require.DirExists(t, filepath.Join(baseDir, "bin"))
	require.DirExists(t, filepath.Join(baseDir, "runtime", "generated"))
	require.NoDirExists(t, filepath.Join(baseDir, "sub"))
	require.NoFileExists(t, filepath.Join(baseDir, "sub", "config.yaml"))
}

// TestInitProjectWritesConfigWithoutBaseDir 验证初始化配置不再写入 base_dir 字段。
func TestInitProjectWritesConfigWithoutBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")

	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "base_dir:")
	require.NotContains(t, string(data), "sub: sub")
}

// TestInitProjectWritesCommentedAgentConfig 验证 init 生成的 agent 配置包含字段说明注释。
func TestInitProjectWritesCommentedAgentConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")

	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "# default agent 配置。")
	require.Contains(t, content, "# 路径配置。相对路径均以 <base-dir> 为基准解析。")
	require.Contains(t, content, "# 自动分配端口范围。仅 add/clone --allocate-ports 使用；手工配置端口可在范围外。")
	require.Contains(t, content, "# 安全策略。默认禁止公开 noauth socks/http。")
	require.Contains(t, content, "# 核心组件安装来源配置。")
}

// TestAddCloneAndMemberCommands 验证模板创建、端口分配、clone 和 member 写入行为。
func TestAddCloneAndMemberCommands(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", AllocatePorts: true}))
	usa1, err := config.LoadStack(filepath.Join(baseDir, "stacks", "usa1.yaml"))
	require.NoError(t, err)
	require.NotEqual(t, "11111111-1111-4111-8111-111111111111", usa1.Xrelay.Inbounds[1].Users[0].UUID)
	require.Equal(t, 4300, usa1.Xrelay.Inbounds[0].Port)

	require.NoError(t, CloneStack(CloneOptions{ConfigPath: configPath, Source: "usa1", Target: "usa2", AllocatePorts: true}))
	usa2, err := config.LoadStack(filepath.Join(baseDir, "stacks", "usa2.yaml"))
	require.NoError(t, err)
	require.Equal(t, "usa2.clash.socks", usa2.Xrelay.Outbound.Ref)
	require.NotEqual(t, usa1.Xrelay.Inbounds[0].Port, usa2.Xrelay.Inbounds[0].Port)

	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "auto", Template: "auto-url-test", Members: []string{"usa1"}, AllocatePorts: true}))
	require.NoError(t, AddMember(MemberOptions{ConfigPath: configPath, Stack: "auto", Member: "usa2"}))
	members, err := ListMembers(MemberOptions{ConfigPath: configPath, Stack: "auto"})
	require.NoError(t, err)
	require.Equal(t, []string{"usa1", "usa2"}, members)

	require.NoError(t, RemoveMember(MemberOptions{ConfigPath: configPath, Stack: "auto", Member: "usa1"}))
	members, err = ListMembers(MemberOptions{ConfigPath: configPath, Stack: "auto"})
	require.NoError(t, err)
	require.Equal(t, []string{"usa2"}, members)
}

// TestCloneStackOnlyRewritesSelfRefFields 验证 clone 只改自身 ref，不误改普通域名和 Host。
func TestCloneStackOnlyRewritesSelfRefFields(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	sourcePath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	data, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	data = bytes.Replace(data, []byte("server: server.example.com"), []byte("server: usa1.example.com"), 1)
	data = bytes.Replace(data, []byte("Host: server.example.com"), []byte("Host: usa1.example.com"), 1)
	data = bytes.Replace(data, []byte("network: ws"), []byte("network: ws\n        ref: usa1.relay\n        headers:\n          ref: usa1.relay"), 1)
	require.NoError(t, os.WriteFile(sourcePath, data, 0o640))

	require.NoError(t, CloneStack(CloneOptions{ConfigPath: configPath, Source: "usa1", Target: "usa2", AllocatePorts: true}))

	cloned, err := os.ReadFile(filepath.Join(baseDir, "stacks", "usa2.yaml"))
	require.NoError(t, err)
	content := string(cloned)
	require.Contains(t, content, "ref: usa2.clash.socks")
	require.Contains(t, content, "server: usa1.example.com")
	require.Contains(t, content, "Host: usa1.example.com")
	require.Contains(t, content, "ref: usa1.relay")
	require.NotContains(t, content, "server: usa2.example.com")
	require.NotContains(t, content, "Host: usa2.example.com")
	require.NotContains(t, content, "ref: usa2.relay")
}

// TestAddStackUsesReferenceTemplateFormat 验证 add 写出的 stack 文件沿用 Python 版内置模板格式。
func TestAddStackUsesReferenceTemplateFormat(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "edge1", Template: "pair", KeepTemplatePorts: true}))

	stackPath := filepath.Join(baseDir, "stacks", "edge1.yaml")
	data, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "# pair 模板，也是 add 默认模板")
	require.Contains(t, string(data), "ref: edge1.clash.socks")
	stack, err := config.LoadStack(stackPath)
	require.NoError(t, err)
	require.Equal(t, 24000, stack.Xrelay.Inbounds[0].Port)
	require.NotEqual(t, "11111111-1111-4111-8111-111111111111", stack.Xrelay.Inbounds[1].Users[0].UUID)
}

// TestAddStackUsesSharedSnippetComments 验证 add 模板会展开 example 共用的注释片段。
func TestAddStackUsesSharedSnippetComments(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "example", Template: "pair", KeepTemplatePorts: true}))

	stackPath := filepath.Join(baseDir, "stacks", "example.yaml")
	data, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	content := string(data)
	upstreamSnippet, err := renderStackSnippet("clash.upstream.vmess-websocket", stackTemplateSnippetContext("pair"))
	require.NoError(t, err)
	listenerSnippet, err := renderStackSnippet("clash.listener.socks", stackTemplateSnippetContext("pair"))
	require.NoError(t, err)
	require.Contains(t, content, strings.SplitN(upstreamSnippet, "\n", 2)[0])
	require.Contains(t, content, strings.SplitN(listenerSnippet, "\n", 2)[0])
	require.Contains(t, content, "port: 17090")
}

// TestAddAutoTemplateWithoutMembersCreatesDisabledDraft 验证 auto 模板无成员时按 Python 版写成禁用草稿。
func TestAddAutoTemplateWithoutMembersCreatesDisabledDraft(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))

	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "auto", Template: "auto-url-test", KeepTemplatePorts: true}))

	stackPath := filepath.Join(baseDir, "stacks", "auto.yaml")
	stack, err := config.LoadStack(stackPath)
	require.NoError(t, err)
	require.False(t, stack.Enabled)
	require.Equal(t, []string{"usa1.relay", "usa2.relay"}, []string{stack.Clash.Upstreams[0].Ref, stack.Clash.Upstreams[1].Ref})
	globalConfig, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	_, err = config.LoadStacks(globalConfig, false)
	require.NoError(t, err)
}

// TestAddStackFromFileRequiresMatchingName 验证 from-file 不再隐式改名，保持 Python 版约束。
func TestAddStackFromFileRequiresMatchingName(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "source", Template: "pair", KeepTemplatePorts: true}))
	sourcePath := filepath.Join(baseDir, "stacks", "source.yaml")

	err := AddStack(AddOptions{ConfigPath: configPath, Name: "target", FromFile: sourcePath, KeepTemplatePorts: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "from-file stack name must match add target")
}

// TestMemberCommandsPreserveTemplateComments 验证 member 命令按 YAML 文档修改，不抹掉模板注释。
func TestMemberCommandsPreserveTemplateComments(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", AllocatePorts: true}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "usa2", Template: "pair", AllocatePorts: true}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "auto", Template: "auto-url-test", Members: []string{"usa1"}, AllocatePorts: true}))

	require.NoError(t, AddMember(MemberOptions{ConfigPath: configPath, Stack: "auto", Member: "usa2"}))

	data, err := os.ReadFile(filepath.Join(baseDir, "stacks", "auto.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "# auto 实例模板")
	require.Contains(t, string(data), "ref: usa2.relay")
	require.Less(t, strings.Index(string(data), "usa2-local"), strings.Index(string(data), "DIRECT"))
}

// TestRemoveStackArchivesFile 验证 remove 默认只归档 stack 文件。
func TestRemoveStackArchivesFile(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, InitProject(InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, AddStack(AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", AllocatePorts: true}))

	require.NoError(t, RemoveStack(RemoveOptions{ConfigPath: configPath, Name: "usa1"}))

	_, err := os.Stat(filepath.Join(baseDir, "stacks", "usa1.yaml"))
	require.True(t, os.IsNotExist(err))
	require.FileExists(t, filepath.Join(baseDir, "stacks", "usa1.yaml.removed"))
}
