package agent

import (
	"fmt"
	"strings"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/spf13/cobra"
)

// newExampleCommand 创建配置片段示例输出命令。
func newExampleCommand() *cobra.Command {
	snippets := agentconfig.StackExampleSnippets()
	long := "Print stack configuration snippets to stdout.\n\nSupported snippets:\n" + exampleCatalog(snippets)
	return &cobra.Command{
		Use:     "example [stack|xrelay|clash] [SECTION] [TYPE]",
		Short:   "Print stack configuration examples",
		Long:    long,
		Example: exampleCommandExamples(),
		Args:    cobra.MaximumNArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			return runExample(command, args)
		},
	}
}

// runExample 根据用户选择输出配置片段或完整索引。
func runExample(command *cobra.Command, args []string) error {
	snippets := agentconfig.StackExampleSnippets()
	if len(args) == 0 {
		_, err := fmt.Fprint(command.OutOrStdout(), exampleUsage(snippets))
		return err
	}
	matches := matchExampleSnippets(snippets, args)
	if len(matches) == 0 {
		return fmt.Errorf("unsupported example snippet: %s\n\n%s", strings.Join(args, " "), supportedExamplePaths(snippets))
	}
	_, err := fmt.Fprint(command.OutOrStdout(), formatExampleSnippets(matches))
	return err
}

// matchExampleSnippets 按 area、section、type 逐级筛选片段。
func matchExampleSnippets(snippets []agentconfig.StackExampleSnippet, args []string) []agentconfig.StackExampleSnippet {
	area := normalizeExampleTerm(args[0])
	section := ""
	snippetType := ""
	if len(args) > 1 {
		section = normalizeExampleTerm(args[1])
	}
	if len(args) > 2 {
		snippetType = normalizeExampleTerm(args[2])
	}
	matches := make([]agentconfig.StackExampleSnippet, 0)
	for _, snippet := range snippets {
		if snippet.Area != area {
			continue
		}
		if section != "" && snippet.Section != section {
			continue
		}
		if snippetType != "" && snippet.Type != snippetType {
			continue
		}
		matches = append(matches, snippet)
	}
	return matches
}

// formatExampleSnippets 输出单片段时保持纯 YAML，多片段时用注释标明来源。
func formatExampleSnippets(snippets []agentconfig.StackExampleSnippet) string {
	if len(snippets) == 1 {
		return snippets[0].Content
	}
	var builder strings.Builder
	builder.WriteString("# 以下是多个候选片段，请按需选择其中一个。\n")
	builder.WriteString("# 如需纯 YAML 输出，请运行注释中的精确命令。\n")
	for index, snippet := range snippets {
		if index > 0 {
			builder.WriteString("\n")
		}
		fmt.Fprintf(&builder, "# ps-agent example %s %s %s - %s\n", snippet.Area, snippet.Section, snippet.Type, snippet.Description)
		builder.WriteString(snippet.Content)
	}
	return builder.String()
}

// exampleUsage 返回包含全部片段说明的 usage 文本。
func exampleUsage(snippets []agentconfig.StackExampleSnippet) string {
	return "Usage:\n" +
		"  ps-agent example [stack|xrelay|clash] [SECTION] [TYPE]\n\n" +
		"Supported snippets:\n" +
		exampleCatalog(snippets) +
		"\nExamples:\n" +
		exampleCommandExamples()
}

// exampleCatalog 生成按 area/section 分组的完整片段清单。
func exampleCatalog(snippets []agentconfig.StackExampleSnippet) string {
	var builder strings.Builder
	currentArea := ""
	currentSection := ""
	for _, snippet := range snippets {
		if snippet.Area != currentArea {
			if currentArea != "" {
				builder.WriteString("\n")
			}
			currentArea = snippet.Area
			currentSection = ""
			fmt.Fprintf(&builder, "  %s:\n", snippet.Area)
		}
		if snippet.Section != currentSection {
			currentSection = snippet.Section
			fmt.Fprintf(&builder, "    %s:\n", snippet.Section)
		}
		fmt.Fprintf(&builder, "      %-16s %s\n", snippet.Type, snippet.Description)
	}
	return builder.String()
}

// supportedExamplePaths 返回错误提示中可用的完整命令路径。
func supportedExamplePaths(snippets []agentconfig.StackExampleSnippet) string {
	var builder strings.Builder
	builder.WriteString("Supported snippets:\n")
	for _, snippet := range snippets {
		fmt.Fprintf(&builder, "  ps-agent example %s %s %s\n", snippet.Area, snippet.Section, snippet.Type)
	}
	return strings.TrimRight(builder.String(), "\n")
}

// exampleCommandExamples 返回 help 中展示的常用调用方式。
func exampleCommandExamples() string {
	return "  ps-agent example\n" +
		"  ps-agent example stack role edge\n" +
		"  ps-agent example xrelay inbound\n" +
		"  ps-agent example xrelay inbound vmess\n" +
		"  ps-agent example xrelay outbound direct\n" +
		"  ps-agent example clash listener socks\n" +
		"  ps-agent example clash upstream raw\n" +
		"  ps-agent example clash group url-test"
}

// normalizeExampleTerm 兼容复数和少量历史拼写错误，输出统一查询键。
func normalizeExampleTerm(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "inbounds", "inboud", "inbouds":
		return "inbound"
	case "outbounds", "oubound", "oubounds":
		return "outbound"
	case "listeners", "listen", "listens", "linsten", "linstens":
		return "listener"
	case "upstreams", "upstrea", "upstreas":
		return "upstream"
	case "groups":
		return "group"
	case "rule":
		return "rules"
	case "vmess":
		return "vmess-raw"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
