package agent

import (
	"context"
	"os/user"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/systemd"
	"github.com/stretchr/testify/require"
)

type fakeServiceAccountRunner struct {
	results map[string]systemd.Result
	calls   []string
}

// Run 记录账户管理命令调用，并返回预设结果。
func (r *fakeServiceAccountRunner) Run(ctx context.Context, name string, args ...string) (systemd.Result, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if result, ok := r.results[call]; ok {
		return result, nil
	}
	return systemd.Result{}, nil
}

// TestEnsureLinuxServiceAccountCreatesMissingEntries 验证服务账户不存在时会按顺序创建组和用户。
func TestEnsureLinuxServiceAccountCreatesMissingEntries(t *testing.T) {
	runner := &fakeServiceAccountRunner{results: map[string]systemd.Result{
		"getent group proxystack":  {ExitCode: 2},
		"getent passwd proxystack": {ExitCode: 2},
	}}

	err := ensureLinuxServiceAccount(context.Background(), runner, "/opt/proxystack")

	require.NoError(t, err)
	require.Equal(t, []string{
		"getent group proxystack",
		"groupadd --system proxystack",
		"getent passwd proxystack",
		"useradd --system --no-create-home --home-dir /opt/proxystack --shell /usr/sbin/nologin --gid proxystack proxystack",
	}, runner.calls)
}

// TestEnsureLinuxServiceAccountSkipsExistingEntries 验证服务账户已存在时不会重复创建。
func TestEnsureLinuxServiceAccountSkipsExistingEntries(t *testing.T) {
	runner := &fakeServiceAccountRunner{results: map[string]systemd.Result{
		"getent group proxystack":  {ExitCode: 0},
		"getent passwd proxystack": {ExitCode: 0},
	}}

	err := ensureLinuxServiceAccount(context.Background(), runner, "/opt/proxystack")

	require.NoError(t, err)
	require.Equal(t, []string{
		"getent group proxystack",
		"getent passwd proxystack",
	}, runner.calls)
}

// TestInitPrintsNonRootLinuxGroupHint 验证 Linux 非 root 初始化时会提示加入 proxystack 组。
func TestInitPrintsNonRootLinuxGroupHint(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 1000, 1000, []int{1000}, func(name string) (*user.Group, error) {
		require.Equal(t, systemd.DefaultServiceGroup, name)
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "init", "--external-host", "proxy.example.com")

	require.Contains(t, output, "sudo usermod -aG proxystack \"$USER\"")
	require.Contains(t, output, "newgrp proxystack")
	require.Contains(t, output, "Initialized agent config:")
}

// TestInitSkipsNonRootLinuxGroupHintWhenAlreadyMember 验证用户已在服务组时不重复提示。
func TestInitSkipsNonRootLinuxGroupHintWhenAlreadyMember(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 1000, 1000, []int{988}, func(name string) (*user.Group, error) {
		require.Equal(t, systemd.DefaultServiceGroup, name)
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "init", "--external-host", "proxy.example.com")

	require.NotContains(t, output, "sudo usermod")
	require.Contains(t, output, "Initialized agent config:")
}

// withServiceAccountRuntime 注入运行时账户信息，避免测试依赖真实操作系统用户和组。
func withServiceAccountRuntime(t *testing.T, goos string, euid int, gid int, groups []int, lookupGroup func(string) (*user.Group, error)) {
	t.Helper()
	oldGOOS := serviceAccountGOOSFunc
	oldEUID := serviceAccountEUIDFunc
	oldGID := serviceAccountGIDFunc
	oldGroups := serviceAccountGroupsFunc
	oldLookupGroup := serviceAccountLookupGroupFunc
	serviceAccountGOOSFunc = func() string { return goos }
	serviceAccountEUIDFunc = func() int { return euid }
	serviceAccountGIDFunc = func() int { return gid }
	serviceAccountGroupsFunc = func() ([]int, error) { return groups, nil }
	serviceAccountLookupGroupFunc = lookupGroup
	t.Cleanup(func() {
		serviceAccountGOOSFunc = oldGOOS
		serviceAccountEUIDFunc = oldEUID
		serviceAccountGIDFunc = oldGID
		serviceAccountGroupsFunc = oldGroups
		serviceAccountLookupGroupFunc = oldLookupGroup
	})
}
