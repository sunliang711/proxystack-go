package sub_test

import (
	"os"
	"path/filepath"
	"testing"

	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

const fixedGeneratedAt = "2026-06-05T12:00:00+08:00"

// TestInputYAMLMatchesGolden 验证 input 传输契约输出稳定。
func TestInputYAMLMatchesGolden(t *testing.T) {
	input := loadManualInput(t)

	output := subgen.InputToYAML(input)

	require.Equal(t, readSubGolden(t, "input.yaml"), output)
}

// TestIndexJSONMatchesGolden 验证合并后的 index JSON 稳定且包含 access。
func TestIndexJSONMatchesGolden(t *testing.T) {
	index := buildManualIndex(t)

	output, err := subgen.IndexToJSON(index)

	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "index.json"), output)
}

// TestRenderSubscriptionsMatchGolden 验证默认三类订阅模板与 Python golden 对齐。
func TestRenderSubscriptionsMatchGolden(t *testing.T) {
	index := buildManualIndex(t)

	clashOutput, err := subgen.RenderClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "clash.yaml"), clashOutput)

	premiumOutput, err := subgen.RenderPremiumClashSubscription(index, "alice", "", "")
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "premium-clash.yaml"), premiumOutput)

	surgeOutput, err := subgen.RenderSurgeSubscription(index, "alice", "", "", "", 86400, true)
	require.NoError(t, err)
	require.Equal(t, readSubGolden(t, "surge.txt"), surgeOutput)
}

// TestMergeInputsRejectsDuplicateNodeID 验证重复 node id 会 fail fast。
func TestMergeInputsRejectsDuplicateNodeID(t *testing.T) {
	input := loadManualInput(t)

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: input},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
}

// TestMergeInputsRejectsDuplicateProxyNameForSameUser 验证同用户重复代理名会失败。
func TestMergeInputsRejectsDuplicateProxyNameForSameUser(t *testing.T) {
	input := loadManualInput(t)
	other := cloneInput(input)
	other.Source = "other"
	other.Nodes[0].ID = "other:relay"

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: other},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate proxy name for user")
}

// TestMergeInputsAllowsSameProxyNameForDifferentUsers 验证不同用户可使用相同代理名。
func TestMergeInputsAllowsSameProxyNameForDifferentUsers(t *testing.T) {
	input := loadManualInput(t)
	other := cloneInput(input)
	other.Source = "other"
	other.Nodes[0].ID = "other:relay"
	other.Nodes[0].User = "bob"

	_, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "a.yaml", Input: input},
		{Name: "b.yaml", Input: other},
	}, subgen.Access{Type: "none"}, fixedGeneratedAt)

	require.NoError(t, err)
}

// TestTemplateOverrideAndStrictUndefined 验证模板覆盖顺序和未定义变量失败。
func TestTemplateOverrideAndStrictUndefined(t *testing.T) {
	index := buildManualIndex(t)
	templateDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(templateDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{{ user }}:{{ proxy_names | tojson }}\n"), 0o644))

	output, err := subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.NoError(t, err)
	require.Equal(t, "alice:[\"Manual Relay\"]\n", output)

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{{ missing_value }}\n"), 0o644))
	_, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "undefined variable")

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{% if not managed_config_url %}ok{% endif %}\n"), 0o644))
	output, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.NoError(t, err)
	require.Equal(t, "ok\n", output)

	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{% for proxy_name in proxy_names %}{% endfor %}{{ proxy_name }}\n"), 0o644))
	_, err = subgen.RenderClashSubscription(index, "alice", templateDir, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "undefined variable: proxy_name")
}

func loadManualInput(t *testing.T) subgen.Input {
	t.Helper()
	input, err := subgen.LoadInputFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	return input
}

func buildManualIndex(t *testing.T) subgen.Index {
	t.Helper()
	index, err := subgen.MergeInputs([]subgen.InputFile{
		{Name: "manual.yaml", Input: loadManualInput(t)},
	}, subgen.Access{Type: "token", Token: "demo-token"}, fixedGeneratedAt)
	require.NoError(t, err)
	return index
}

func readSubGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(testutil.RepoPath(t, "tests", "golden", "sub", name))
	require.NoError(t, err)
	return string(data)
}

func cloneInput(input subgen.Input) subgen.Input {
	cloned := input
	cloned.Nodes = append([]subgen.Node(nil), input.Nodes...)
	return cloned
}
