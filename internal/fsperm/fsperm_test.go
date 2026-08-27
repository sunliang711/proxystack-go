package fsperm_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/eagle/proxystack-go/internal/fsperm"
	"github.com/stretchr/testify/require"
)

// TestMatchesComparesSpecialBits 验证特殊位参与比较。
//
// setgid 在 os.FileMode 里是高位而不是裸八进制，只比 Perm() 会把它整个漏掉，
// 而组可写目录丢了 setgid 正是最容易静默破坏服务的状态。
func TestMatchesComparesSpecialBits(t *testing.T) {
	require.True(t, fsperm.Matches(os.FileMode(0o770)|os.ModeSetgid|os.ModeDir, fsperm.SharedDirMode))
	require.False(t, fsperm.Matches(os.FileMode(0o770)|os.ModeDir, fsperm.SharedDirMode), "缺 setgid 必须判为不符")
	require.False(t, fsperm.Matches(os.FileMode(0o750)|os.ModeSetgid|os.ModeDir, fsperm.SharedDirMode))
	require.True(t, fsperm.Matches(os.FileMode(0o640), fsperm.FileMode))
	require.False(t, fsperm.Matches(os.FileMode(0o640)|os.ModeSetuid, fsperm.FileMode), "组成员给自己文件加 setuid 必须被发现")
}

// TestFormatLabelsSpecialBits 验证输出把特殊位显式标出。
func TestFormatLabelsSpecialBits(t *testing.T) {
	require.Equal(t, "0770+setgid", fsperm.Format(fsperm.SharedDirMode))
	require.Equal(t, "0750", fsperm.Format(fsperm.ServiceDirMode))
	require.Equal(t, "0640+setuid", fsperm.Format(os.FileMode(0o640)|os.ModeSetuid))
}

// TestMkdirManagedSetsModeRegardlessOfUmask 验证目录权限不受 umask 影响。
//
// os.MkdirAll 的 mode 会被 umask 削掉（默认 022 下 0o770 变成 0750），
// 而且 mkdir(2) 从来不设置 setgid 位，所以必须建完再显式 chmod。
func TestMkdirManagedSetsModeRegardlessOfUmask(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	path := filepath.Join(t.TempDir(), "nested", "deep")

	require.NoError(t, fsperm.MkdirManaged(path, fsperm.SharedDirMode))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, fsperm.Matches(info.Mode(), fsperm.SharedDirMode), "得到 %s，期望 %s", fsperm.Format(info.Mode()), fsperm.Format(fsperm.SharedDirMode))
}

// TestChmodIfDifferentSkipsMatchingMode 验证权限已符合时不调用 chmod。
//
// chmod 要求调用者是 owner，无变化时也 chmod 会让非 owner 撞 EPERM。
func TestChmodIfDifferentSkipsMatchingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	require.NoError(t, os.Chmod(path, fsperm.FileMode))
	before, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, fsperm.ChmodIfDifferent(path, fsperm.FileMode))

	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before.Mode(), after.Mode())
}

// TestChmodIfDifferentFixesDrift 验证权限漂移时会修回来。
func TestChmodIfDifferentFixesDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o666))

	require.NoError(t, fsperm.ChmodIfDifferent(path, fsperm.FileMode))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, fsperm.Matches(info.Mode(), fsperm.FileMode))
}

// TestChmodIfDifferentSkipsSymlinkAndMissing 验证软链接和缺失路径都不会被 chmod。
//
// os.Chmod 跟随软链接，而 Linux 没有 lchmod，唯一的防线就是这里跳过。
func TestChmodIfDifferentSkipsSymlinkAndMissing(t *testing.T) {
	dir := t.TempDir()
	victimPath := filepath.Join(dir, "victim")
	require.NoError(t, os.WriteFile(victimPath, []byte("x"), 0o600))
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(victimPath, linkPath))

	require.NoError(t, fsperm.ChmodIfDifferent(linkPath, fsperm.FileMode))
	require.NoError(t, fsperm.ChmodIfDifferent(filepath.Join(dir, "missing"), fsperm.FileMode))

	info, err := os.Stat(victimPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "软链接目标不应被 chmod")
}

// TestMkdirManagedFixesIntermediateDirs 验证 MkdirAll 顺带创建的中间目录也被修正。
//
// 只 chmod 最后一级的话，中间目录会停在被 umask 削过的权限上，
// 组成员随后就写不进去。
func TestMkdirManagedFixesIntermediateDirs(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "c")

	require.NoError(t, fsperm.MkdirManaged(path, fsperm.SharedDirMode))

	for _, dir := range []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b"), path} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		require.True(t, fsperm.Matches(info.Mode(), fsperm.SharedDirMode), "%s 得到 %s", dir, fsperm.Format(info.Mode()))
	}
}

// TestMkdirManagedFixesExistingTarget 验证已存在的目标目录也会被修正（存量升级路径）。
func TestMkdirManagedFixesExistingTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	require.NoError(t, os.MkdirAll(path, 0o750))

	require.NoError(t, fsperm.MkdirManaged(path, fsperm.SharedDirMode))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, fsperm.Matches(info.Mode(), fsperm.SharedDirMode), "得到 %s", fsperm.Format(info.Mode()))
}

// TestMkdirManagedLeavesExistingAncestorsAlone 验证已存在的祖先目录不被改动。
func TestMkdirManagedLeavesExistingAncestorsAlone(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o700))

	require.NoError(t, fsperm.MkdirManaged(filepath.Join(root, "child"), fsperm.SharedDirMode))

	info, err := os.Stat(root)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "已存在的祖先不应被改权限")
}
