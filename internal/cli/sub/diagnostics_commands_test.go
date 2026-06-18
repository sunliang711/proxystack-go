package sub

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestDoctorCommandIsRegistered 验证 ps-sub 命令树包含 doctor 诊断入口。
func TestDoctorCommandIsRegistered(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"doctor", "--help"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "doctor")
}

// TestRunSubDoctorMissingConfigSuggestsInit 验证未初始化时 doctor 会提示先执行 init。
func TestRunSubDoctorMissingConfigSuggestsInit(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "sub", "config.yaml")

	_, err := runSubDoctor(configPath, "test")

	require.Error(t, err)
	require.Contains(t, err.Error(), "sub config is missing")
	require.Contains(t, err.Error(), "ps-sub --base-dir")
	require.Contains(t, err.Error(), "init")
}

// TestRunSubDoctorValidatesConfigAndInputs 验证 doctor 会加载 sub 配置并校验 inputs。
func TestRunSubDoctorValidatesConfigAndInputs(t *testing.T) {
	baseDir := t.TempDir()
	writeSubDoctorConfig(t, baseDir)
	inputDir := filepath.Join(baseDir, "sub", "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	inputData, err := os.ReadFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "manual.yaml"), inputData, 0o640))

	report, err := runSubDoctor(filepath.Join(baseDir, "sub", "config.yaml"), "test")

	require.NoError(t, err)
	checks := strings.Join(report.Checks, "\n")
	issues := strings.Join(report.Issues, "\n")
	require.Contains(t, checks, "config loaded")
	require.Contains(t, checks, "subscription inputs validated")
	require.NotContains(t, issues, "subscription inputs validation failed")
	require.Contains(t, issues, "unsupported service manager: test")
}

// TestRunSubDoctorReportsInvalidInputs 验证 inputs 解析失败会作为 ISSUE 输出。
func TestRunSubDoctorReportsInvalidInputs(t *testing.T) {
	baseDir := t.TempDir()
	writeSubDoctorConfig(t, baseDir)
	inputDir := filepath.Join(baseDir, "sub", "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "bad.yaml"), []byte("bad: ["), 0o640))

	report, err := runSubDoctor(filepath.Join(baseDir, "sub", "config.yaml"), "test")

	require.NoError(t, err)
	require.Contains(t, strings.Join(report.Issues, "\n"), "subscription inputs validation failed")
}

// TestAddSubDoctorMetadataIssuesReportsModeMismatch 验证 doctor 会报告 sub 目录树权限不匹配。
func TestAddSubDoctorMetadataIssuesReportsModeMismatch(t *testing.T) {
	baseDir := t.TempDir()
	writeSubDoctorConfig(t, baseDir)
	inputDir := filepath.Join(baseDir, "sub", "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o750))
	inputPath := filepath.Join(inputDir, "manual.yaml")
	require.NoError(t, os.WriteFile(inputPath, []byte("nodes: []\n"), 0o644))
	report := subDoctorReport{}

	addSubDoctorMetadataIssues(&report, subOnlyGlobalConfig(baseDir), 0, 0, false)

	require.Contains(t, strings.Join(report.Issues, "\n"), "path mode mismatch: "+inputPath)
}

// writeSubDoctorConfig 写入 doctor 测试使用的最小 sub 配置。
func writeSubDoctorConfig(t *testing.T, baseDir string) {
	t.Helper()
	configPath := filepath.Join(baseDir, "sub", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte("access:\n  type: none\n"), 0o640))
}
