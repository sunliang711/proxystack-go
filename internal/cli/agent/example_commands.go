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
		Use:     "example [config|stack|xray|clash] [SECTION] [TYPE]",
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
		return unsupportedExampleSnippetError(snippets, args)
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
		fmt.Fprintf(&builder, "# psctl example %s %s %s - %s\n", snippet.Area, snippet.Section, snippet.Type, snippet.Description)
		builder.WriteString(snippet.Content)
	}
	return builder.String()
}

// exampleUsage 返回包含全部片段说明的 usage 文本。
func exampleUsage(snippets []agentconfig.StackExampleSnippet) string {
	return "Usage:\n" +
		"  psctl example [config|stack|xray|clash] [SECTION] [TYPE]\n\n" +
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

// unsupportedExampleSnippetError 按用户输入层级返回更聚焦的可用值提示。
func unsupportedExampleSnippetError(snippets []agentconfig.StackExampleSnippet, args []string) error {
	rawInput := strings.Join(args, " ")
	area := normalizeExampleTerm(args[0])
	if !exampleAreaExists(snippets, area) {
		return formatUnsupportedExampleSnippetError(rawInput, closestExampleTerm(args[0], exampleAreas(snippets)), supportedExampleAreas(snippets))
	}
	if len(args) > 1 {
		section := normalizeExampleTerm(args[1])
		if !exampleSectionExists(snippets, area, section) {
			suggestion := closestExampleTerm(args[1], exampleSections(snippets, area))
			if suggestion != "" {
				suggestion = area + " " + suggestion
			}
			return formatUnsupportedExampleSnippetError(rawInput, suggestion, supportedExampleSections(snippets, area))
		}
		if len(args) > 2 {
			suggestion := closestExampleTerm(args[2], exampleTypes(snippets, area, section))
			if suggestion != "" {
				suggestion = area + " " + section + " " + suggestion
			}
			return formatUnsupportedExampleSnippetError(rawInput, suggestion, supportedExampleTypes(snippets, area, section))
		}
	}
	return formatUnsupportedExampleSnippetError(rawInput, "", supportedExampleAreas(snippets))
}

// formatUnsupportedExampleSnippetError 组装未知片段错误和可选的相近命令提示。
func formatUnsupportedExampleSnippetError(rawInput string, suggestion string, supported string) error {
	if suggestion != "" {
		return fmt.Errorf("unsupported example snippet: %s\n\nDid you mean: %s\n\n%s", rawInput, suggestion, supported)
	}
	return fmt.Errorf("unsupported example snippet: %s\n\n%s", rawInput, supported)
}

// exampleAreaExists 判断 area 是否存在于片段清单。
func exampleAreaExists(snippets []agentconfig.StackExampleSnippet, area string) bool {
	for _, snippet := range snippets {
		if snippet.Area == area {
			return true
		}
	}
	return false
}

// exampleSectionExists 判断 section 是否存在于指定 area。
func exampleSectionExists(snippets []agentconfig.StackExampleSnippet, area string, section string) bool {
	for _, snippet := range snippets {
		if snippet.Area == area && snippet.Section == section {
			return true
		}
	}
	return false
}

// exampleAreas 返回片段清单中的 area，保持定义顺序且去重。
func exampleAreas(snippets []agentconfig.StackExampleSnippet) []string {
	areas := make([]string, 0)
	seen := map[string]bool{}
	for _, snippet := range snippets {
		if seen[snippet.Area] {
			continue
		}
		seen[snippet.Area] = true
		areas = append(areas, snippet.Area)
	}
	return areas
}

// exampleSections 返回指定 area 下的 section，保持定义顺序且去重。
func exampleSections(snippets []agentconfig.StackExampleSnippet, area string) []string {
	sections := make([]string, 0)
	seen := map[string]bool{}
	for _, snippet := range snippets {
		if snippet.Area != area || seen[snippet.Section] {
			continue
		}
		seen[snippet.Section] = true
		sections = append(sections, snippet.Section)
	}
	return sections
}

// exampleTypes 返回指定 area 和 section 下的 type，保持定义顺序。
func exampleTypes(snippets []agentconfig.StackExampleSnippet, area string, section string) []string {
	types := make([]string, 0)
	for _, snippet := range snippets {
		if snippet.Area != area || snippet.Section != section {
			continue
		}
		types = append(types, snippet.Type)
	}
	return types
}

// supportedExampleAreas 返回错误提示中可用的 area 列表。
func supportedExampleAreas(snippets []agentconfig.StackExampleSnippet) string {
	var builder strings.Builder
	builder.WriteString("Supported areas:\n")
	for _, area := range exampleAreas(snippets) {
		fmt.Fprintf(&builder, "  %s\n", area)
	}
	return strings.TrimRight(builder.String(), "\n")
}

// supportedExampleSections 返回错误提示中指定 area 下可用的 section 列表。
func supportedExampleSections(snippets []agentconfig.StackExampleSnippet, area string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Supported sections for area %q:\n", area)
	for _, section := range exampleSections(snippets, area) {
		fmt.Fprintf(&builder, "  %s\n", section)
	}
	return strings.TrimRight(builder.String(), "\n")
}

// supportedExampleTypes 返回错误提示中指定 section 下可用的 type 列表。
func supportedExampleTypes(snippets []agentconfig.StackExampleSnippet, area string, section string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Supported types for section %q:\n", area+" "+section)
	for _, snippetType := range exampleTypes(snippets, area, section) {
		fmt.Fprintf(&builder, "  %s\n", snippetType)
	}
	return strings.TrimRight(builder.String(), "\n")
}

// closestExampleTerm 返回和用户输入最接近的候选值，距离过大时不提示。
func closestExampleTerm(input string, candidates []string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	best := ""
	bestDistance := 0
	for _, candidate := range candidates {
		distance := editDistance(input, candidate)
		if best == "" || distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	if best == "" || bestDistance > maxExampleSuggestionDistance(input) {
		return ""
	}
	return best
}

// maxExampleSuggestionDistance 按输入长度限制建议范围，避免给出牵强提示。
func maxExampleSuggestionDistance(input string) int {
	if len(input) >= 10 {
		return 3
	}
	if len(input) >= 5 {
		return 2
	}
	return 1
}

// editDistance 计算两个短命令片段的 Levenshtein 距离。
func editDistance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	previous := make([]int, len(rightRunes)+1)
	current := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range leftRunes {
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range rightRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = minExampleDistance(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(rightRunes)]
}

// minExampleDistance 返回三个编辑距离候选中的最小值。
func minExampleDistance(first int, second int, third int) int {
	if first <= second && first <= third {
		return first
	}
	if second <= third {
		return second
	}
	return third
}

// exampleCommandExamples 返回 help 中展示的常用调用方式。
func exampleCommandExamples() string {
	return "  psctl example\n" +
		"  psctl example config users default\n" +
		"  psctl example stack role edge\n" +
		"  psctl example xray inbound\n" +
		"  psctl example xray inbound vmess-raw\n" +
		"  psctl example xray outbound direct\n" +
		"  psctl example clash listener socks\n" +
		"  psctl example clash upstream raw\n" +
		"  psctl example clash group url-test"
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
