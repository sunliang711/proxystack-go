package testutil

import (
	"path/filepath"
	"runtime"
	"testing"
)

// RepoRoot 返回当前仓库根目录，供跨 package 测试读取共享 fixture。
func RepoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve testutil path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// RepoPath 返回仓库根目录下的路径。
func RepoPath(t testing.TB, parts ...string) string {
	t.Helper()
	values := append([]string{RepoRoot(t)}, parts...)
	return filepath.Join(values...)
}

// ChdirRepo 把当前测试工作目录切到仓库根目录。
func ChdirRepo(t testing.TB) {
	t.Helper()
	if tb, ok := t.(interface{ Chdir(string) }); ok {
		tb.Chdir(RepoRoot(t))
		return
	}
	t.Fatal("test type does not support Chdir")
}
