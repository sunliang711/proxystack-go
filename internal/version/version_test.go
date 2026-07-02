package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInfoUsesInjectedBuildMetadata 验证 version 输出使用构建期注入的 tag、短 commit 和时间。
func TestInfoUsesInjectedBuildMetadata(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildDateTime := BuildDateTime
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDateTime = originalBuildDateTime
	})
	Version = "v1.2.3"
	Commit = "abcdef1234567890"
	BuildDateTime = "2026-06-18T11:29:10Z"

	require.Equal(t, "psctl\n  version: v1.2.3\n  commit: abcdef1\n  build_datetime: 2026-06-18T11:29:10Z", Info("psctl"))
}

// TestInfoFallsBackToDevelopmentVersion 验证未注入 tag 时仍有稳定的开发版本输出。
func TestInfoFallsBackToDevelopmentVersion(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildDateTime := BuildDateTime
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildDateTime = originalBuildDateTime
	})
	Version = ""
	Commit = ""
	BuildDateTime = ""

	info := Info("pssub")

	require.Contains(t, info, "pssub\n  version: 0.1.0-dev\n  commit: ")
	require.Contains(t, info, "\n  build_datetime: unknown")
}
