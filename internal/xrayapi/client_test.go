package xrayapi_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/xrayapi"
	"github.com/stretchr/testify/require"
)

// TestRemoveUserPassesExpectedArguments 固定 rmu 的命令行形状。
//
// --timeout 必须是裸秒数（xray 用的是 IntVar 而不是 DurationVar），-tag 是单短横线，
// email 是位置参数且排在所有 flag 之后。
func TestRemoveUserPassesExpectedArguments(t *testing.T) {
	client, argvPath := newFakeClient(t, "Removed 1 user(s) in total.", 0)

	err := client.RemoveUser(context.Background(), "vmess:24100:vmess", "bob@example.com")

	require.NoError(t, err)
	require.Equal(t, []string{
		"api", "rmu",
		"--server=127.0.0.1:10085",
		"--timeout=3",
		"-tag=vmess:24100:vmess",
		"bob@example.com",
	}, readArgv(t, argvPath))
}

// TestAddUserWritesPatchAndPassesFilePath 验证 adu 传的是刚写出的 patch 文件，且用完即删。
func TestAddUserWritesPatchAndPassesFilePath(t *testing.T) {
	client, argvPath := newFakeClient(t, "Added 1 user(s) in total.", 0)
	dir := t.TempDir()

	err := client.AddUser(context.Background(), dir, `{"inbounds":[]}`)

	require.NoError(t, err)
	argv := readArgv(t, argvPath)
	require.Len(t, argv, 5)
	require.Equal(t, []string{"api", "adu", "--server=127.0.0.1:10085", "--timeout=3"}, argv[:4])
	require.Equal(t, dir, filepath.Dir(argv[4]), "patch 应写在传入目录而不是共享的 /tmp")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "临时 patch 文件应在调用结束后删除")
}

// TestRemoveUserRejectsZeroSuccessCount 验证 xray 报 0 个成功时不能当成功。
//
// xray api rmu/adu 对单个用户的失败只是打印出来就继续，进程照样退出 0，
// 所以只看退出码会把“什么都没删”当成删除成功。
func TestRemoveUserRejectsZeroSuccessCount(t *testing.T) {
	client, _ := newFakeClient(t, "no such user: bob\nRemoved 0 user(s) in total.", 0)

	err := client.RemoveUser(context.Background(), "vmess-in", "bob")

	require.Error(t, err)
	require.Contains(t, err.Error(), "xray processed 0 users")
	require.Contains(t, err.Error(), "no such user: bob")
}

// TestAddUserRejectsZeroSuccessCount 验证 adu 同样按计数判定。
func TestAddUserRejectsZeroSuccessCount(t *testing.T) {
	client, _ := newFakeClient(t, "Added 0 user(s) in total.", 0)

	err := client.AddUser(context.Background(), t.TempDir(), `{"inbounds":[]}`)

	require.Error(t, err)
	require.Contains(t, err.Error(), "xray processed 0 users")
}

// TestRemoveUserRejectsMissingCountLine 验证认不出统计行时报错而不是默认成功。
func TestRemoveUserRejectsMissingCountLine(t *testing.T) {
	client, _ := newFakeClient(t, "some unexpected output", 0)

	err := client.RemoveUser(context.Background(), "vmess-in", "bob")

	require.Error(t, err)
	require.Contains(t, err.Error(), "did not report a user count")
}

// TestRemoveUserReportsNonZeroExit 验证 dial 失败之类的硬错误带上原始输出。
func TestRemoveUserReportsNonZeroExit(t *testing.T) {
	client, _ := newFakeClient(t, "failed to dial 127.0.0.1:10085", 1)

	err := client.RemoveUser(context.Background(), "vmess-in", "bob")

	require.Error(t, err)
	require.Contains(t, err.Error(), "xray api rmu")
	require.Contains(t, err.Error(), "failed to dial")
}

// TestRemoveUserRejectsLeadingDashArguments 验证会被 flag 解析吃掉的值直接拒绝执行。
//
// email 是裸位置参数，以 '-' 开头会被当成 flag，把 rmu 打到别的 inbound 上或者变成空操作。
func TestRemoveUserRejectsLeadingDashArguments(t *testing.T) {
	client, argvPath := newFakeClient(t, "Removed 1 user(s) in total.", 0)

	err := client.RemoveUser(context.Background(), "vmess-in", "-tag=api")

	require.Error(t, err)
	require.Contains(t, err.Error(), "user email must not start with '-'")
	require.NoFileExists(t, argvPath, "拒绝的调用不应真的执行 xray")

	err = client.RemoveUser(context.Background(), "-evil", "bob")
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound tag must not start with '-'")

	err = client.RemoveUser(context.Background(), "vmess-in", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "user email must not be empty")
}

// TestBinaryPathUsesManagedBinDir 验证用的是受管的 bin/xray 而不是 PATH 里的。
func TestBinaryPathUsesManagedBinDir(t *testing.T) {
	config := domain.GlobalConfig{BaseDir: "/opt/proxystack", Paths: domain.DefaultConfigPaths()}

	require.Equal(t, "/opt/proxystack/bin/xray", xrayapi.BinaryPath(config))
	require.Equal(t, "/opt/proxystack/bin/xray", xrayapi.NewClient(config, "127.0.0.1:10085").Binary)
}

// TestResolveAPIEndpointMergesStackOverride 验证端点解析遵循 defaults 与 stack 覆盖。
func TestResolveAPIEndpointMergesStackOverride(t *testing.T) {
	config := domain.GlobalConfig{Defaults: domain.DefaultsConfig{Xray: domain.DefaultXrayConfig{API: domain.DefaultXrayAPIConfig()}}}

	endpoint := xrayapi.ResolveAPIEndpoint(config, domain.Stack{Name: "usa1"})

	require.True(t, endpoint.Ready())
	require.Equal(t, "127.0.0.1:10085", endpoint.Server)
	require.Empty(t, endpoint.Reason())
}

// TestAPIEndpointReasonExplainsEachBlocker 验证不可用原因逐项可读。
func TestAPIEndpointReasonExplainsEachBlocker(t *testing.T) {
	tests := []struct {
		name     string
		endpoint xrayapi.APIEndpoint
		reason   string
	}{
		{
			name:     "disabled",
			endpoint: xrayapi.APIEndpoint{Server: "127.0.0.1:10085", HandlerService: true},
			reason:   "xray.api.enabled is false",
		},
		{
			name:     "emptylisten",
			endpoint: xrayapi.APIEndpoint{Enabled: true, HandlerService: true},
			reason:   "xray.api.listen is empty",
		},
		{
			name:     "nohandler",
			endpoint: xrayapi.APIEndpoint{Enabled: true, Server: "127.0.0.1:10085"},
			reason:   "xray.api.services does not include HandlerService",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, tt.endpoint.Ready())
			require.Equal(t, tt.reason, tt.endpoint.Reason())
		})
	}
}

// newFakeClient 构造一个指向假 xray 脚本的客户端；脚本记录 argv 并输出指定内容。
func newFakeClient(t *testing.T, output string, exitCode int) (xrayapi.Client, string) {
	t.Helper()
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	binary := filepath.Join(dir, "xray")
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\" >> " + argvPath + "; done\n" +
		"printf '%s\\n' " + shellQuote(output) + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o755))
	return xrayapi.Client{Binary: binary, Server: "127.0.0.1:10085", Timeout: 3 * time.Second}, argvPath
}

// readArgv 读回假 xray 脚本记录的参数列表。
func readArgv(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
