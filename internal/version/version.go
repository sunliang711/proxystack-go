package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const (
	// Name 是当前 Go 重写项目的产品名。
	Name = "proxystack"

	defaultVersion    = "0.1.0-dev"
	unknownCommit     = "unknown"
	unknownBuildTime  = "unknown"
	shortCommitLength = 7
)

var (
	// Version 是构建时从 git tag 注入的版本号，未注入时使用开发默认值。
	Version = defaultVersion
	// Commit 是构建时注入的 git commit short hash，未注入时尝试读取 Go build info。
	Commit = ""
	// BuildDateTime 是构建时注入的 UTC 时间，使用 RFC3339 格式。
	BuildDateTime = ""
)

// Info 返回 CLI version 命令展示的稳定版本字符串。
func Info(binary string) string {
	return fmt.Sprintf("%s\n  version: %s\n  commit: %s\n  build_datetime: %s", binary, resolvedVersion(), CommitShort(), resolvedBuildDateTime())
}

// CommitShort 返回当前构建的短 commit hash，用于 CLI version 输出。
func CommitShort() string {
	if commit := strings.TrimSpace(Commit); commit != "" {
		return shortCommit(commit)
	}
	if commit := buildInfoCommit(); commit != "" {
		return shortCommit(commit)
	}
	return unknownCommit
}

// resolvedVersion 返回构建注入的版本号，空值时回退到开发默认版本。
func resolvedVersion() string {
	if version := strings.TrimSpace(Version); version != "" {
		return version
	}
	return defaultVersion
}

// resolvedBuildDateTime 返回构建注入的 UTC 时间，空值时回退为 unknown。
func resolvedBuildDateTime() string {
	if buildDateTime := strings.TrimSpace(BuildDateTime); buildDateTime != "" {
		return buildDateTime
	}
	return unknownBuildTime
}

// buildInfoCommit 从 Go build info 读取 VCS revision，覆盖未通过 ldflags 注入的本地构建。
func buildInfoCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}

// shortCommit 将完整 commit hash 收敛为 git 默认风格的短 hash。
func shortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) <= shortCommitLength {
		return commit
	}
	return commit[:shortCommitLength]
}
