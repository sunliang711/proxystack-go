package agent

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestIPInfoCommandIsRegistered 验证 psctl 命令树包含 ipinfo 诊断入口。
func TestIPInfoCommandIsRegistered(t *testing.T) {
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"ipinfo", "--help"})

	err := command.Execute()

	require.NoError(t, err)
	require.Contains(t, output.String(), "ipinfo STACK")
	require.Contains(t, output.String(), "--family")
	require.Contains(t, output.String(), "--timeout")
}

// TestDoctorCommandIsRegistered 验证 psctl 命令树包含 doctor 诊断入口。
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

// TestRunDoctorMissingConfigSuggestsInit 验证未初始化时 doctor 会提示先执行 init。
func TestRunDoctorMissingConfigSuggestsInit(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	_, err := runDoctor(configPath)

	require.Error(t, err)
	require.Contains(t, err.Error(), "agent config is missing")
	require.Contains(t, err.Error(), "psctl --base-dir")
	require.Contains(t, err.Error(), "init")
}

// TestRunDoctorSkipsLiveSystemPortProbe 验证 doctor 不把运行中服务占用的端口当作配置错误。
func TestRunDoctorSkipsLiveSystemPortProbe(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	require.NoError(t, os.WriteFile(configPath, []byte(doctorTestConfig()), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "edge.yaml"), []byte(doctorTestStack(occupiedPort, freeTCPPort(t), freeTCPPort(t))), 0o640))

	report, err := runDoctor(configPath)

	require.NoError(t, err)
	require.NotContains(t, strings.Join(report.Issues, "\n"), "configuration validation failed")
	require.Contains(t, strings.Join(report.Checks, "\n"), "stacks configuration validated")
}

// TestDoctorCheckSummariesIncludeAccountDetails 验证 doctor OK 摘要会说明账户和 metadata 期望。
func TestDoctorCheckSummariesIncludeAccountDetails(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)
	uid, err := strconv.Atoi(currentUser.Uid)
	require.NoError(t, err)
	gid, err := strconv.Atoi(currentUser.Gid)
	require.NoError(t, err)
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	report := doctorReport{}

	addDoctorMetadataIssues(&report, cfg, uid, gid, true)

	require.Equal(t, "service account: user=proxystack uid=988 group=proxystack gid=989", formatDoctorServiceAccountCheck(988, 989))
	require.Contains(t, strings.Join(report.Checks, "\n"), "filesystem metadata checked: expected owner=proxystack:proxystack mode rules=")
}

// TestDoctorMetadataAllowsMissingGeneratedComponentDirs 验证未启动前缺少生成子目录不会被 doctor 误报。
func TestDoctorMetadataAllowsMissingGeneratedComponentDirs(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, os.Chmod(baseDir, 0o750))
	for _, dir := range []string{
		"bin",
		"geo",
		"stacks",
		"runtime",
		filepath.Join("runtime", "generated"),
		"publish",
		"downloads",
		"sub",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(baseDir, dir), 0o750))
	}
	cfg := domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths()}
	report := doctorReport{}

	addDoctorMetadataIssues(&report, cfg, 0, 0, false)

	require.Empty(t, report.Issues)
}

// TestAddDoctorOwnerIssueReportsMismatch 验证 doctor 会报告标准路径 owner 不匹配。
func TestAddDoctorOwnerIssueReportsMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("owner metadata is not available on windows")
	}
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o640))
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	report := doctorReport{}

	addDoctorOwnerIssue(&report, path, info, int(stat.Uid)+1, int(stat.Gid)+1)

	require.Len(t, report.Issues, 1)
	require.Contains(t, report.Issues[0], "path owner mismatch")
	require.Contains(t, report.Issues[0], path)
	require.Contains(t, report.Issues[0], "got=")
	require.Contains(t, report.Issues[0], "want=")
	require.Contains(t, report.Issues[0], fmt.Sprintf("uid=%d gid=%d", stat.Uid, stat.Gid))
}

// TestFormatDoctorOwnerLabelIncludesNamesAndIDs 验证 owner 输出优先使用名称并保留数字 ID。
func TestFormatDoctorOwnerLabelIncludesNamesAndIDs(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)
	uid, err := strconv.Atoi(currentUser.Uid)
	require.NoError(t, err)
	gid, err := strconv.Atoi(currentUser.Gid)
	require.NoError(t, err)
	currentGroup, err := user.LookupGroupId(currentUser.Gid)
	require.NoError(t, err)

	label := formatDoctorOwnerLabel(uid, gid)

	require.Contains(t, label, currentUser.Username+":"+currentGroup.Name)
	require.Contains(t, label, fmt.Sprintf("uid=%d gid=%d", uid, gid))
}

// freeTCPPort 返回当前可用的本机 TCP 端口，供配置测试避开无关冲突。
func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// doctorTestConfig 生成 doctor 回归测试所需的最小全局配置。
func doctorTestConfig() string {
	return `version: 1
paths:
  bin: bin
  geo: geo
  stacks: stacks
  runtime: runtime
  generated: runtime/generated
  publish: publish
  downloads: downloads
  sub: sub
subscription:
  source: local
port_ranges:
  xrelay_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
defaults:
  clash:
    mode: Rule
    rule_profile: default
  xrelay:
    loglevel: warning
    api:
      enabled: false
    stats:
      enabled: false
    policy:
      enabled: false
security:
  require_auth_for_public_socks_http: true
  allow_noauth_public: false
install:
  mihomo:
    version: latest
    source: auto
  xray:
    version: latest
    source: auto
  geo:
    version: latest
    source: auto
`
}

// doctorTestStack 生成包含指定监听端口的最小 stack 配置。
func doctorTestStack(xrelayPort int, clashSocksPort int, clashControllerPort int) string {
	return fmt.Sprintf(`name: edge
enabled: true
role: edge
xrelay:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: %d
      auth:
        type: noauth
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:%d
    secret: test-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: %d
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`, xrelayPort, clashControllerPort, clashSocksPort)
}
