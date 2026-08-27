package agent

import (
	"bytes"
	"context"
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

	"github.com/eagle/proxystack-go/internal/diagnostics"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/fsperm"
	"github.com/eagle/proxystack-go/internal/systemd"
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

// TestIPInfoProgressRendererRewritesInteractiveLines 验证交互式 ipinfo 状态会原地替换完成的 family 行。
func TestIPInfoProgressRendererRewritesInteractiveLines(t *testing.T) {
	var output bytes.Buffer
	renderer := newIPInfoStatusRenderer(&output, true)

	renderer.Handle(diagnostics.IPInfoProgress{State: diagnostics.IPInfoProgressDetecting, Family: "ipv4", Label: "IPv4"})
	renderer.Handle(diagnostics.IPInfoProgress{State: diagnostics.IPInfoProgressDetecting, Family: "ipv6", Label: "IPv6"})
	renderer.Handle(diagnostics.IPInfoProgress{
		State:  diagnostics.IPInfoProgressDone,
		Family: "ipv6",
		Label:  "IPv6",
		Result: diagnostics.FamilyResult{Family: "ipv6", Label: "IPv6", IP: "2607:8700:5501:4b6b::2"},
	})
	renderer.Handle(diagnostics.IPInfoProgress{
		State:  diagnostics.IPInfoProgressDone,
		Family: "ipv4",
		Label:  "IPv4",
		Result: diagnostics.FamilyResult{Family: "ipv4", Label: "IPv4", IP: "104.194.66.220", Region: "Los Angeles / California / US / AS25820 IT7 Networks Inc"},
	})

	text := output.String()
	require.Contains(t, text, "IPv4  Detecting ...\n\nIPv6  Detecting ...\n\n")
	require.Contains(t, text, "\x1b[2A\r\x1b[2KIPv6  2607:8700:5501:4b6b::2\n\r\x1b[2K      Region unknown\x1b[1B\r")
	require.Contains(t, text, "\x1b[4A\r\x1b[2KIPv4  104.194.66.220\n\r\x1b[2K      Los Angeles, California, US · AS25820 IT7 Networks Inc\x1b[3B\r")
}

// TestIPInfoCommandNonTTYOutputDoesNotContainANSI 验证非 TTY 输出使用普通报告且不包含 ANSI 控制字符。
func TestIPInfoCommandNonTTYOutputDoesNotContainANSI(t *testing.T) {
	originalQueryIPInfo := queryIPInfo
	t.Cleanup(func() { queryIPInfo = originalQueryIPInfo })
	queryIPInfo = func(ctx context.Context, options diagnostics.QueryOptions) (diagnostics.IpInfoReport, error) {
		require.Nil(t, options.LineCallback)
		require.Nil(t, options.ProgressCallback)
		return diagnostics.IpInfoReport{
			StackName: options.StackName,
			ProxyURL:  "socks5://127.0.0.1:17091",
			Families: []diagnostics.FamilyResult{
				{Family: "ipv4", Label: "IPv4", IP: "198.51.100.10", Region: "Tokyo / JP"},
				{Family: "ipv6", Label: "IPv6", IP: "2001:db8::10", Region: "Singapore / SG"},
			},
		}, nil
	}
	command := NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--base-dir", t.TempDir(), "ipinfo", "usa1", "--family", "all"})

	err := command.Execute()

	require.NoError(t, err)
	text := output.String()
	require.NotContains(t, text, "\x1b[")
	require.Contains(t, text, "Stack: usa1")
	require.Contains(t, text, "IP: 198.51.100.10")
	require.Contains(t, text, "IP: 2001:db8::10")
}

// TestIPInfoInteractiveOutputRejectsDevNull 验证 character device 但非 TTY 的输出不会启用 ANSI 刷新。
func TestIPInfoInteractiveOutputRejectsDevNull(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	defer devNull.Close()

	require.False(t, shouldUseIPInfoInteractiveOutput(devNull))
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

// TestRunDoctorMissingConfigSuggestsSetupLocal 验证未初始化时 doctor 会提示先执行 setup local。
func TestRunDoctorMissingConfigSuggestsSetupLocal(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	_, err := runDoctor(configPath)

	require.Error(t, err)
	require.Contains(t, err.Error(), "agent config is missing")
	require.Contains(t, err.Error(), "psctl --base-dir")
	require.Contains(t, err.Error(), "setup local")
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
	require.Contains(t, strings.Join(report.Checks, "\n"), "filesystem metadata checked: expected group=proxystack mode rules=")
}

// TestDoctorMetadataAllowsMissingGeneratedComponentDirs 验证未启动前缺少生成子目录不会被 doctor 误报。
func TestDoctorMetadataAllowsMissingGeneratedComponentDirs(t *testing.T) {
	baseDir := t.TempDir()
	require.NoError(t, os.Chmod(baseDir, fsperm.ServiceDirMode))
	for dir, mode := range map[string]os.FileMode{
		"bin":                                 fsperm.ServiceDirMode,
		"geo":                                 fsperm.ServiceDirMode,
		"downloads":                           fsperm.ServiceDirMode,
		"stacks":                              fsperm.SharedDirMode,
		"runtime":                             fsperm.SharedDirMode,
		filepath.Join("runtime", "generated"): fsperm.SharedDirMode,
		"publish":                             fsperm.SharedDirMode,
	} {
		require.NoError(t, fsperm.MkdirManaged(filepath.Join(baseDir, dir), mode))
	}
	cfg := domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths()}
	report := doctorReport{}

	addDoctorMetadataIssues(&report, cfg, 0, 0, false)

	require.Empty(t, report.Issues)
}

// TestDoctorUnitIssuesIgnoreSubService 验证 psctl doctor 不检查 pssub 独立管理的 service。
func TestDoctorUnitIssuesIgnoreSubService(t *testing.T) {
	unitDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(unitDir, systemd.XrayUnitTemplate), []byte("xray"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(unitDir, systemd.ClashUnitTemplate), []byte("clash"), 0o644))
	oldUnitDir := doctorSystemdUnitDir
	doctorSystemdUnitDir = unitDir
	t.Cleanup(func() {
		doctorSystemdUnitDir = oldUnitDir
	})
	report := doctorReport{}

	addDoctorUnitIssues(&report)

	require.Empty(t, report.Issues)
	require.Contains(t, strings.Join(report.Checks, "\n"), "systemd units checked")
}

// TestAddDoctorOwnerIssueReportsGroupMismatch 验证 doctor 会报告标准路径的组不匹配。
func TestAddDoctorOwnerIssueReportsGroupMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("owner metadata is not available on windows")
	}
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o640))
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	report := doctorReport{}

	addDoctorOwnerIssue(&report, path, info, int(stat.Uid), int(stat.Gid)+1)

	require.Len(t, report.Issues, 1)
	require.Contains(t, report.Issues[0], "path group mismatch")
	require.Contains(t, report.Issues[0], path)
	require.Contains(t, report.Issues[0], "got=")
	require.Contains(t, report.Issues[0], "want=")
	require.Contains(t, report.Issues[0], fmt.Sprintf("uid=%d gid=%d", stat.Uid, stat.Gid))
}

// TestAddDoctorOwnerIssueAcceptsForeignOwnerInServiceGroup 验证只要组对，owner 是谁都不算问题。
//
// 受管目录组可写之后，文件可能由运维账号、root 或服务账号任一方创建，
// owner 本来就不唯一；决定服务读不读得到的是组。
func TestAddDoctorOwnerIssueAcceptsForeignOwnerInServiceGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("owner metadata is not available on windows")
	}
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o640))
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	report := doctorReport{}

	addDoctorOwnerIssue(&report, path, info, int(stat.Uid)+1, int(stat.Gid))

	require.Empty(t, report.Issues)
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
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
defaults:
  clash:
    mode: Rule
    rule_profile: default
  xray:
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
func doctorTestStack(xrayPort int, clashSocksPort int, clashControllerPort int) string {
	return fmt.Sprintf(`name: edge
enabled: true
role: edge
xray:
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
`, xrayPort, clashControllerPort, clashSocksPort)
}
