package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAgentExampleHelpListsAllSnippets 验证 example usage 展示所有支持的 stack 片段。
func TestAgentExampleHelpListsAllSnippets(t *testing.T) {
	output := runAgentCommandForTest(t, "example", "--help")

	require.Contains(t, output, "Usage:")
	require.Contains(t, output, "Supported snippets:")
	require.Contains(t, output, "config:")
	require.Contains(t, output, "users:")
	require.Contains(t, output, "stack:")
	require.Contains(t, output, "role:")
	require.Contains(t, output, "edge")
	require.Contains(t, output, "auto")
	require.Contains(t, output, "xray:")
	require.Contains(t, output, "loglevel:")
	require.Contains(t, output, "inbound:")
	require.Contains(t, output, "vmess")
	require.Contains(t, output, "shadowsocks")
	require.Contains(t, output, "socks5")
	require.Contains(t, output, "http")
	require.Contains(t, output, "outbound:")
	require.Contains(t, output, "clash")
	require.Contains(t, output, "direct")
	require.Contains(t, output, "auth:")
	require.Contains(t, output, "noauth")
	require.Contains(t, output, "password")
	require.Contains(t, output, "listener:")
	require.Contains(t, output, "mode:")
	require.Contains(t, output, "rule")
	require.Contains(t, output, "global")
	require.Contains(t, output, "silent")
	require.Contains(t, output, "upstream:")
	require.Contains(t, output, "xray-socks5")
	require.Contains(t, output, "raw")
	require.Contains(t, output, "raw-shadowsocks")
	require.Contains(t, output, "raw-socks5")
	require.Contains(t, output, "raw-http")
	require.Contains(t, output, "group:")
	require.Contains(t, output, "select")
	require.Contains(t, output, "url-test")
	require.Contains(t, output, "load-balance")
	require.Contains(t, output, "fallback")
	require.Contains(t, output, "rules:")
}

// TestAgentExampleNoArgsPrintsUsage 验证不带参数时直接输出完整索引。
func TestAgentExampleNoArgsPrintsUsage(t *testing.T) {
	output := runAgentCommandForTest(t, "example")

	require.Contains(t, output, "psctl example [config|stack|xray|clash] [SECTION] [TYPE]")
	require.Contains(t, output, "psctl example config users default")
	require.Contains(t, output, "psctl example stack role edge")
	require.Contains(t, output, "psctl example clash upstream raw")
}

// TestAgentExampleSingleSnippetPrintsPlainYAML 验证精确片段输出带片段内注释的纯 YAML。
func TestAgentExampleSingleSnippetPrintsPlainYAML(t *testing.T) {
	output := runAgentCommandForTest(t, "example", "xray", "inbound", "vmess")

	require.True(t, strings.HasPrefix(output, "# vmess raw inbound"))
	require.Contains(t, output, "- name: vmess")
	require.Contains(t, output, "protocol: vmess")
	require.Contains(t, output, "udp: true")
	require.Contains(t, output, "user_refs:")
	require.Contains(t, output, "profile: default")
	require.NotContains(t, output, "uuid: 11111111-1111-4111-8111-111111111111")
	require.Contains(t, output, "# display_template: '{{ .stack }} {{ .inbound }} {{ .protocol }} {{ .user }}'")
	require.Contains(t, output, "# display_template: '{{ .stack }} {{ .user }} {{ .profile }} {{ .remark }}'")
	require.NotContains(t, output, "# xray inbound vmess")
}

// TestAgentExampleConfigUsersSnippet 验证 example 子命令提供 config.yaml 用户档案模板。
func TestAgentExampleConfigUsersSnippet(t *testing.T) {
	output := runAgentCommandForTest(t, "example", "config", "users", "default")

	require.True(t, strings.HasPrefix(output, "# 全局订阅用户档案"))
	require.Contains(t, output, "users:")
	require.Contains(t, output, "profile: default")
	require.Contains(t, output, "socks5/http 的实际连接账号仍配置在 inbound.auth")
	require.Contains(t, output, "display_template")
}

// TestAgentExampleSectionPrintsAllTypes 验证分类输出包含该分类下所有类型。
func TestAgentExampleSectionPrintsAllTypes(t *testing.T) {
	output := runAgentCommandForTest(t, "example", "xray", "outbound")

	require.Contains(t, output, "选择其中一个")
	require.Contains(t, output, "如需纯 YAML 输出")
	require.Contains(t, output, "# psctl example xray outbound clash")
	require.Contains(t, output, "# psctl example xray outbound socks5")
	require.Contains(t, output, "# psctl example xray outbound http")
	require.Contains(t, output, "# psctl example xray outbound direct")
}

// TestAgentExampleAcceptsCommonTypos 验证命令兼容用户常见拼写错误。
func TestAgentExampleAcceptsCommonTypos(t *testing.T) {
	output := runAgentCommandForTest(t, "example", "xray", "inboud", "socks5")

	require.Contains(t, output, "protocol: socks5")
	require.Contains(t, output, "auth 是 socks5 的实际连接账号")
	require.Contains(t, output, "auth:")
}

// TestAgentExampleRejectsUnknownSnippet 验证未知片段返回可定位的错误。
func TestAgentExampleRejectsUnknownSnippet(t *testing.T) {
	output, err := runAgentCommandForTestError("example", "clash", "listener", "mixed")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported example snippet: clash listener mixed")
	require.Empty(t, output)
}
