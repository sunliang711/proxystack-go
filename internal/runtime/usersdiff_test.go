package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	"github.com/stretchr/testify/require"
)

const twoUserConfig = `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vmess-in",
      "port": 24100,
      "protocol": "vmess",
      "settings": {"clients": [{"id": "a", "email": "alice"}, {"id": "b", "email": "bob"}]},
      "streamSettings": {"network": "raw"}
    }
  ]
}`

const oneUserConfig = `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vmess-in",
      "port": 24100,
      "protocol": "vmess",
      "settings": {"clients": [{"id": "a", "email": "alice"}]},
      "streamSettings": {"network": "raw"}
    }
  ]
}`

// TestUsersOnlyPlanAcceptsExactRemoval 验证差异恰好等于预期删除时放行。
func TestUsersOnlyPlanAcceptsExactRemoval(t *testing.T) {
	plan := planWithXrayChange(t, twoUserConfig, oneUserConfig)
	expected := agentruntime.NewUserChange()
	expected.Add(true, "usa1", "vmess-in", "bob")

	usersOnly, path, err := agentruntime.UsersOnlyPlan(plan, expected)

	require.NoError(t, err)
	require.True(t, usersOnly, "unexpected reject at %s", path)
}

// TestUsersOnlyPlanRejectsExtraRemoval 验证混进来的、本次并不热应用的用户删除会被拦下。
//
// 这是最关键的一条：运维在 stack 文件里删掉了某个用户但还没重启，此时任何一次
// user disable 都不能把那次删除顺手写盘并刷新 manifest——运行中的实例收不到它，
// 之后漂移检测也再看不出来。
func TestUsersOnlyPlanRejectsExtraRemoval(t *testing.T) {
	plan := planWithXrayChange(t, twoUserConfig, `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vmess-in",
      "port": 24100,
      "protocol": "vmess",
      "settings": {"clients": []},
      "streamSettings": {"network": "raw"}
    }
  ]
}`)
	expected := agentruntime.NewUserChange()
	expected.Add(true, "usa1", "vmess-in", "bob")

	usersOnly, path, err := agentruntime.UsersOnlyPlan(plan, expected)

	require.NoError(t, err)
	require.False(t, usersOnly)
	require.Equal(t, "generated/xray/usa1.json", path)
}

// TestUsersOnlyPlanRejectsUnexpectedAddition 验证别处新加的用户不会被顺手写盘。
func TestUsersOnlyPlanRejectsUnexpectedAddition(t *testing.T) {
	plan := planWithXrayChange(t, twoUserConfig, `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vmess-in",
      "port": 24100,
      "protocol": "vmess",
      "settings": {"clients": [{"id": "a", "email": "alice"}, {"id": "c", "email": "carol"}]},
      "streamSettings": {"network": "raw"}
    }
  ]
}`)
	expected := agentruntime.NewUserChange()
	expected.Add(true, "usa1", "vmess-in", "bob")

	usersOnly, _, err := agentruntime.UsersOnlyPlan(plan, expected)

	require.NoError(t, err)
	require.False(t, usersOnly)
}

// TestUsersOnlyPlanRejectsPortEdit 验证端口变化必须重启，不能只热改用户。
func TestUsersOnlyPlanRejectsPortEdit(t *testing.T) {
	plan := planWithXrayChange(t, twoUserConfig, `{
  "log": {"loglevel": "warning"},
  "inbounds": [
    {
      "tag": "vmess-in",
      "port": 24200,
      "protocol": "vmess",
      "settings": {"clients": [{"id": "a", "email": "alice"}]},
      "streamSettings": {"network": "raw"}
    }
  ]
}`)
	expected := agentruntime.NewUserChange()
	expected.Add(true, "usa1", "vmess-in", "bob")

	usersOnly, _, err := agentruntime.UsersOnlyPlan(plan, expected)

	require.NoError(t, err)
	require.False(t, usersOnly)
}

// TestUsersOnlyPlanRejectsNonXrayChange 验证 clash 生成物变化一律要求重启。
func TestUsersOnlyPlanRejectsNonXrayChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usa1.yaml")
	require.NoError(t, os.WriteFile(path, []byte("mode: Rule\n"), 0o640))
	plan := agentruntime.Plan{Changes: []agentruntime.FileChange{{
		Action:       agentruntime.ActionUpdate,
		Path:         path,
		RelativePath: "generated/mihomo/usa1.yaml",
		Service:      "proxystack-clash@usa1.service",
		Content:      []byte("mode: Global\n"),
	}}}

	usersOnly, reported, err := agentruntime.UsersOnlyPlan(plan, agentruntime.NewUserChange())

	require.NoError(t, err)
	require.False(t, usersOnly)
	require.Equal(t, "generated/mihomo/usa1.yaml", reported)
}

// TestUsersOnlyPlanAllowsCreate 验证首次生成没有正在运行的旧配置可比，直接放行。
func TestUsersOnlyPlanAllowsCreate(t *testing.T) {
	plan := agentruntime.Plan{Changes: []agentruntime.FileChange{{
		Action:       agentruntime.ActionCreate,
		Path:         filepath.Join(t.TempDir(), "usa1.json"),
		RelativePath: "generated/xray/usa1.json",
		Service:      "proxystack-xray@usa1.service",
		Content:      []byte(oneUserConfig),
	}}}

	usersOnly, _, err := agentruntime.UsersOnlyPlan(plan, agentruntime.NewUserChange())

	require.NoError(t, err)
	require.True(t, usersOnly)
}

// TestUsersOnlyPlanRejectsInvalidJSON 验证坏配置报错而不是被当作无差异。
func TestUsersOnlyPlanRejectsInvalidJSON(t *testing.T) {
	plan := planWithXrayChange(t, "{", oneUserConfig)

	_, _, err := agentruntime.UsersOnlyPlan(plan, agentruntime.NewUserChange())

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid JSON")
}

// planWithXrayChange 构造一个 usa1 Xray 生成文件从 oldContent 变成 newContent 的 plan。
func planWithXrayChange(t *testing.T, oldContent string, newContent string) agentruntime.Plan {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usa1.json")
	require.NoError(t, os.WriteFile(path, []byte(oldContent), 0o640))
	return agentruntime.Plan{Changes: []agentruntime.FileChange{{
		Action:       agentruntime.ActionUpdate,
		Path:         path,
		RelativePath: "generated/xray/usa1.json",
		Service:      "proxystack-xray@usa1.service",
		Content:      []byte(newContent),
	}}}
}
