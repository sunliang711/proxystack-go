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

// TestSetupLocalPrintsNonRootLinuxGroupHint 验证 Linux 非 root setup local 时会提示加入 proxystack 组。
func TestSetupLocalPrintsNonRootLinuxGroupHint(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 1000, 1000, []int{1000}, func(name string) (*user.Group, error) {
		require.Equal(t, systemd.DefaultServiceGroup, name)
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})
	withAgentServiceManager(t, &fakeSetupManager{})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "setup", "local", "--external-host", "proxy.example.com")

	require.Contains(t, output, "sudo usermod -aG proxystack \"$USER\"")
	require.Contains(t, output, "newgrp proxystack")
	require.Contains(t, output, "Installed units:")
}

// TestSetupLocalSkipsNonRootLinuxGroupHintWhenAlreadyMember 验证用户已在服务组时不重复提示。
func TestSetupLocalSkipsNonRootLinuxGroupHintWhenAlreadyMember(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 1000, 1000, []int{988}, func(name string) (*user.Group, error) {
		require.Equal(t, systemd.DefaultServiceGroup, name)
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})
	withAgentServiceManager(t, &fakeSetupManager{})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "setup", "local", "--external-host", "proxy.example.com")

	require.NotContains(t, output, "sudo usermod")
	require.Contains(t, output, "Installed units:")
}

// TestSetupLocalPrintsSudoInvokerGroupHint 验证 root 安装时也会提示把 sudo 背后的账号加入服务组。
//
// 走标准流程的人都是 sudo 装的，如果只在非 root 分支提示，最需要知道这件事的人反而看不到。
func TestSetupLocalPrintsSudoInvokerGroupHint(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 0, 0, nil, func(name string) (*user.Group, error) {
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})
	withRootSetupFakes(t)
	withSudoInvoker(t, "eagle", []string{"1000", "27"})
	withAgentServiceManager(t, &fakeSetupManager{})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "setup", "local", "--external-host", "proxy.example.com")

	require.Contains(t, output, "sudo usermod -aG proxystack eagle")
	require.Contains(t, output, "newgrp proxystack")
	require.Contains(t, output, "Installed units:")
}

// TestSetupLocalSkipsSudoInvokerHintWhenAlreadyMember 验证调用者已在组里时不提示。
func TestSetupLocalSkipsSudoInvokerHintWhenAlreadyMember(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 0, 0, nil, func(name string) (*user.Group, error) {
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})
	withRootSetupFakes(t)
	withSudoInvoker(t, "eagle", []string{"1000", "988"})
	withAgentServiceManager(t, &fakeSetupManager{})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "setup", "local", "--external-host", "proxy.example.com")

	require.NotContains(t, output, "sudo usermod")
}

// TestSetupLocalSkipsSudoInvokerHintWithoutSudoUser 验证直接以 root 登录时不给无意义的提示。
func TestSetupLocalSkipsSudoInvokerHintWithoutSudoUser(t *testing.T) {
	withServiceAccountRuntime(t, "linux", 0, 0, nil, func(name string) (*user.Group, error) {
		return &user.Group{Name: systemd.DefaultServiceGroup, Gid: "988"}, nil
	})
	withRootSetupFakes(t)
	withSudoInvoker(t, "", nil)
	withAgentServiceManager(t, &fakeSetupManager{})

	output := runAgentCommandForTest(t, "--base-dir", t.TempDir(), "setup", "local", "--external-host", "proxy.example.com")

	require.NotContains(t, output, "sudo usermod")
}

// withRootSetupFakes 注入 root 路径下 setup 会触达的账户命令和 metadata 修复。
func withRootSetupFakes(t *testing.T) {
	t.Helper()
	oldRunner := serviceAccountRunner
	oldOwnerIDs := serviceAccountOwnerIDsFunc
	serviceAccountRunner = &fakeServiceAccountRunner{results: map[string]systemd.Result{
		"getent group proxystack":  {ExitCode: 0},
		"getent passwd proxystack": {ExitCode: 0},
	}}
	serviceAccountOwnerIDsFunc = func() (int, int, error) { return -1, -1, nil }
	t.Cleanup(func() {
		serviceAccountRunner = oldRunner
		serviceAccountOwnerIDsFunc = oldOwnerIDs
	})
}

// withSudoInvoker 注入 SUDO_USER 及其所属组，避免测试依赖真实环境变量和用户数据库。
func withSudoInvoker(t *testing.T, name string, groupIDs []string) {
	t.Helper()
	oldUser := serviceAccountSudoUserFunc
	oldGroups := serviceAccountUserGroupIDsFunc
	serviceAccountSudoUserFunc = func() string { return name }
	serviceAccountUserGroupIDsFunc = func(lookup string) ([]string, error) {
		require.Equal(t, name, lookup)
		return groupIDs, nil
	}
	t.Cleanup(func() {
		serviceAccountSudoUserFunc = oldUser
		serviceAccountUserGroupIDsFunc = oldGroups
	})
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
