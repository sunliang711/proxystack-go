package agentconfig

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestStackSnippetDefinitionsAreRenderable 验证片段定义唯一、元数据一致、路径存在且模板可渲染。
func TestStackSnippetDefinitionsAreRenderable(t *testing.T) {
	require.NotEmpty(t, stackSnippetDefinitions)
	examples := StackExampleSnippets()
	require.Len(t, examples, len(stackSnippetDefinitions))

	seenIDs := map[string]bool{}
	seenSelectors := map[string]bool{}
	for index, definition := range stackSnippetDefinitions {
		require.NotEmpty(t, definition.ID)
		require.NotEmpty(t, definition.Area)
		require.NotEmpty(t, definition.Section)
		require.NotEmpty(t, definition.Type)
		require.NotEmpty(t, definition.Description)
		require.NotEmpty(t, definition.Path)
		require.Equal(t, fmt.Sprintf("%s.%s.%s", definition.Area, definition.Section, definition.Type), definition.ID)
		require.False(t, seenIDs[definition.ID], "duplicate snippet ID: %s", definition.ID)
		seenIDs[definition.ID] = true
		selector := definition.Area + "\x00" + definition.Section + "\x00" + definition.Type
		require.False(t, seenSelectors[selector], "duplicate snippet selector: %s", definition.ID)
		seenSelectors[selector] = true

		_, err := stackSnippetFiles.ReadFile(definition.Path)
		require.NoError(t, err)
		content, err := renderStackSnippet(definition.ID, defaultStackSnippetContext())
		require.NoError(t, err)
		require.NotEmpty(t, strings.TrimSpace(content))

		require.Equal(t, definition.Area, examples[index].Area)
		require.Equal(t, definition.Section, examples[index].Section)
		require.Equal(t, definition.Type, examples[index].Type)
		require.Equal(t, definition.Description, examples[index].Description)
		require.Equal(t, content, examples[index].Content)
	}
}

// TestStackSnippetDefinitionsValidateInMinimalParents 验证每个片段注入最小合法父结构后能通过配置模型校验。
func TestStackSnippetDefinitionsValidateInMinimalParents(t *testing.T) {
	covered := map[string]bool{}
	for _, definition := range stackSnippetDefinitions {
		definition := definition
		covered[definition.Area+"."+definition.Section] = true
		t.Run(definition.ID, func(t *testing.T) {
			content, err := renderStackSnippet(definition.ID, defaultStackSnippetContext())
			require.NoError(t, err)
			require.NotEmpty(t, strings.TrimSpace(content))
			validateSnippetInMinimalParent(t, definition, content)
		})
	}
	for _, key := range expectedSnippetCoverageKeys() {
		require.True(t, covered[key], "missing snippet coverage: %s", key)
	}
}

// validateSnippetInMinimalParent 根据片段 area 选择 config 或 stack 父结构执行校验。
func validateSnippetInMinimalParent(t *testing.T, definition stackSnippetDefinition, content string) {
	t.Helper()
	root := decodeYAMLRoot(t, content)
	switch definition.Area {
	case "config":
		validateConfigSnippetInMinimalParent(t, definition, root)
	case "stack":
		validateStackSnippetInMinimalParent(t, definition, root)
	case "xray":
		validateXraySnippetInMinimalParent(t, definition, root)
	case "clash":
		validateClashSnippetInMinimalParent(t, definition, root)
	default:
		require.Failf(t, "unsupported snippet area", "area=%s", definition.Area)
	}
}

// validateConfigSnippetInMinimalParent 将 config 片段注入最小 GlobalConfig 后校验。
func validateConfigSnippetInMinimalParent(t *testing.T, definition stackSnippetDefinition, root *yaml.Node) {
	t.Helper()
	require.Equal(t, "users", definition.Section)
	users := requireMappingValue(t, root, "users")
	configRoot := decodeYAMLRoot(t, minimalGlobalConfigYAML())
	setMappingValue(configRoot, "users", users)

	var config domain.GlobalConfig
	require.NoError(t, configRoot.Decode(&config))
	config.BaseDir = t.TempDir()
	require.NoError(t, config.Validate())
}

// validateStackSnippetInMinimalParent 将 stack 片段注入最小 Stack 后校验。
func validateStackSnippetInMinimalParent(t *testing.T, definition stackSnippetDefinition, root *yaml.Node) {
	t.Helper()
	require.Equal(t, "role", definition.Section)
	stackRoot := decodeYAMLRoot(t, minimalStackYAML())
	setMappingValue(stackRoot, "role", requireMappingValue(t, root, "role"))
	validateStackRoot(t, stackRoot)
}

// validateXraySnippetInMinimalParent 将 xray 片段注入最小 Stack 后校验。
func validateXraySnippetInMinimalParent(t *testing.T, definition stackSnippetDefinition, root *yaml.Node) {
	t.Helper()
	stackRoot := decodeYAMLRoot(t, minimalStackYAML())
	xray := requireMappingValue(t, stackRoot, "xray")
	switch definition.Section {
	case "api", "stats", "policy", "loglevel", "outbound":
		setMappingValue(xray, definition.Section, requireMappingValue(t, root, definition.Section))
	case "auth":
		inbounds := requireMappingValue(t, xray, "inbounds")
		require.NotEmpty(t, inbounds.Content)
		setMappingValue(inbounds.Content[0], "auth", requireMappingValue(t, root, "auth"))
	case "inbound":
		require.Equal(t, yaml.SequenceNode, root.Kind)
		setMappingValue(xray, "inbounds", root)
	default:
		require.Failf(t, "unsupported xray snippet section", "section=%s", definition.Section)
	}
	validateStackRoot(t, stackRoot)
}

// validateClashSnippetInMinimalParent 将 clash 片段注入最小 Stack 后校验。
func validateClashSnippetInMinimalParent(t *testing.T, definition stackSnippetDefinition, root *yaml.Node) {
	t.Helper()
	stackRoot := decodeYAMLRoot(t, minimalStackYAML())
	clash := requireMappingValue(t, stackRoot, "clash")
	switch definition.Section {
	case "mode", "loglevel", "controller", "rules":
		setMappingValue(clash, definition.Section, requireMappingValue(t, root, definition.Section))
	case "listener":
		require.Equal(t, yaml.SequenceNode, root.Kind)
		listeners := requireMappingValue(t, clash, "listeners")
		setMappingValue(listeners, definition.Type, root)
	case "upstream":
		require.Equal(t, yaml.SequenceNode, root.Kind)
		setMappingValue(clash, "upstreams", root)
	case "group":
		require.Equal(t, yaml.SequenceNode, root.Kind)
		require.NotEmpty(t, root.Content)
		setMappingValue(clash, "upstreams", decodeYAMLRoot(t, minimalClashUpstreamsYAML()))
		setMappingValue(clash, "groups", root)
		groupName := scalarValue(requireMappingValue(t, root.Content[0], "name"))
		setMappingScalar(requireMappingValue(t, clash, "rules"), "final", groupName)
	default:
		require.Failf(t, "unsupported clash snippet section", "section=%s", definition.Section)
	}
	validateStackRoot(t, stackRoot)
}

// validateStackRoot 将 YAML 根节点解码为 Stack 并执行领域模型校验。
func validateStackRoot(t *testing.T, root *yaml.Node) {
	t.Helper()
	var stack domain.Stack
	require.NoError(t, root.Decode(&stack))
	require.NoError(t, stack.Validate())
}

// decodeYAMLRoot 解码 YAML 文档并返回顶层节点。
func decodeYAMLRoot(t *testing.T, content string) *yaml.Node {
	t.Helper()
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(content), &document))
	require.NotEmpty(t, document.Content)
	return document.Content[0]
}

// requireMappingValue 读取 mapping key 对应的值，不存在时直接让测试失败。
func requireMappingValue(t *testing.T, root *yaml.Node, key string) *yaml.Node {
	t.Helper()
	require.NotNil(t, root)
	require.Equal(t, yaml.MappingNode, root.Kind)
	value := mappingValue(root, key)
	require.NotNil(t, value, "missing mapping key: %s", key)
	return value
}

// expectedSnippetCoverageKeys 返回必须被片段注入测试覆盖的 area.section 集合。
func expectedSnippetCoverageKeys() []string {
	return []string{
		"config.users",
		"stack.role",
		"xray.api",
		"xray.stats",
		"xray.policy",
		"xray.loglevel",
		"xray.auth",
		"xray.inbound",
		"xray.outbound",
		"clash.mode",
		"clash.loglevel",
		"clash.controller",
		"clash.listener",
		"clash.upstream",
		"clash.group",
		"clash.rules",
	}
}

// minimalGlobalConfigYAML 返回用于承载 config snippet 的最小全局配置。
func minimalGlobalConfigYAML() string {
	return `version: 1
paths:
  stacks: stacks
port_ranges:
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
`
}

// minimalStackYAML 返回用于承载 stack/xray/clash snippet 的最小 stack 配置。
func minimalStackYAML() string {
	return `name: example
enabled: true
xray:
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24000
      auth:
        type: noauth
      sub: false
clash:
  controller:
    listen: 127.0.0.1:19090
    secret: demo-secret
  listeners:
    socks:
      - name: socks
        listen: 127.0.0.1
        port: 17090
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
    final: AllProxy
`
}

// minimalClashUpstreamsYAML 返回 group 片段引用的最小上游集合。
func minimalClashUpstreamsYAML() string {
	return `- name: server-a
  type: raw
  config:
    type: socks5
    server: 127.0.0.1
    port: 1080
- name: server-b
  type: raw
  config:
    type: http
    server: 127.0.0.1
    port: 8080
`
}
