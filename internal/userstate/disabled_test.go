package userstate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/userstate"
	"github.com/stretchr/testify/require"
)

// TestLoadMissingFileReturnsEmptyState 验证首次使用时缺失的 disabled.json 不是错误。
func TestLoadMissingFileReturnsEmptyState(t *testing.T) {
	state, err := userstate.Load(filepath.Join(t.TempDir(), "disabled.json"))

	require.NoError(t, err)
	require.Equal(t, userstate.StateVersion, state.Version)
	require.Empty(t, state.Disabled)
}

// TestSaveAndLoadRoundTrip 验证禁用状态可以稳定落盘并读回。
func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "disabled.json")
	state := userstate.State{}
	require.True(t, state.Disable("usa2", "bob", "2026-08-27T10:00:00+08:00"))
	require.True(t, state.Disable("usa1", "alice", "2026-08-27T11:00:00+08:00"))

	require.NoError(t, userstate.Save(path, state))
	loaded, err := userstate.Load(path)

	require.NoError(t, err)
	require.Equal(t, []userstate.Entry{
		{User: "alice", Stack: "usa1", Since: "2026-08-27T11:00:00+08:00"},
		{User: "bob", Stack: "usa2", Since: "2026-08-27T10:00:00+08:00"},
	}, loaded.Disabled)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

// TestDisableAndEnableReportChange 验证重复操作不会产生冗余写入。
func TestDisableAndEnableReportChange(t *testing.T) {
	state := userstate.State{}

	require.True(t, state.Disable("usa1", "bob", "now"))
	require.False(t, state.Disable("usa1", "bob", "later"))
	require.True(t, state.IsDisabled("usa1", "bob"))
	require.False(t, state.IsDisabled("usa2", "bob"))

	require.True(t, state.Enable("usa1", "bob"))
	require.False(t, state.Enable("usa1", "bob"))
	require.False(t, state.IsDisabled("usa1", "bob"))
}

// TestUserSetIndexesByStack 验证转换出的集合按 stack 隔离。
func TestUserSetIndexesByStack(t *testing.T) {
	state := userstate.State{}
	state.Disable("usa1", "bob", "now")

	users := state.UserSet()

	require.True(t, users.Has("usa1", "bob"))
	require.False(t, users.Has("usa2", "bob"))
	require.False(t, users.Has("usa1", "alice"))
	require.False(t, domain.DisabledUserSet(nil).Has("usa1", "bob"))
}

// TestLoadRejectsUnsupportedVersion 验证未知版本 fail fast 而不是被当成空状态。
func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disabled.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":99,"disabled":[]}`), 0o640))

	_, err := userstate.Load(path)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported disabled state version")
}

// TestLoadRejectsEntryWithoutStack 验证条目必须带具体 stack，避免通配语义。
func TestLoadRejectsEntryWithoutStack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disabled.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"disabled":[{"user":"bob","stack":""}]}`), 0o640))

	_, err := userstate.Load(path)

	require.Error(t, err)
	require.Contains(t, err.Error(), "requires user and stack")
}
