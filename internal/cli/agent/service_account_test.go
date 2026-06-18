package agent

import (
	"context"
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
