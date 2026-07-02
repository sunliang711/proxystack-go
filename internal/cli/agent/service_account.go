package agent

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/systemd"
)

var serviceAccountRunner systemd.Runner = systemd.CommandRunner{}
var serviceAccountOwnerIDsFunc = serviceAccountOwnerIDs
var repairServiceMetadataFunc = repairServiceMetadata

// ensureServiceAccountForInit 在 Linux root 初始化时幂等创建 systemd unit 使用的运行用户和组。
func ensureServiceAccountForInit(ctx context.Context, baseDir string) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil
	}
	return ensureLinuxServiceAccount(ctx, serviceAccountRunner, baseDir)
}

// ensureLinuxServiceAccount 通过系统账户命令幂等创建 proxystack 运行组和用户。
func ensureLinuxServiceAccount(ctx context.Context, runner systemd.Runner, baseDir string) error {
	groupExists, err := serviceAccountEntryExists(ctx, runner, "group", systemd.DefaultServiceGroup)
	if err != nil {
		return err
	}
	if !groupExists {
		if err := runServiceAccountCommand(ctx, runner, "groupadd", "--system", systemd.DefaultServiceGroup); err != nil {
			return err
		}
	}
	userExists, err := serviceAccountEntryExists(ctx, runner, "passwd", systemd.DefaultServiceUser)
	if err != nil {
		return err
	}
	if userExists {
		return nil
	}
	return runServiceAccountCommand(ctx, runner, "useradd",
		"--system",
		"--no-create-home",
		"--home-dir", baseDir,
		"--shell", "/usr/sbin/nologin",
		"--gid", systemd.DefaultServiceGroup,
		systemd.DefaultServiceUser,
	)
}

// serviceAccountEntryExists 使用 getent 判断指定 passwd/group 条目是否存在。
func serviceAccountEntryExists(ctx context.Context, runner systemd.Runner, database string, name string) (bool, error) {
	result, err := runner.Run(ctx, "getent", database, name)
	if err != nil {
		return false, fmt.Errorf("getent %s %s failed: %w", database, name, err)
	}
	if result.ExitCode == 0 {
		return true, nil
	}
	if result.ExitCode == 2 {
		return false, nil
	}
	return false, fmt.Errorf("getent %s %s failed: %s", database, name, serviceAccountCommandOutput(result))
}

// runServiceAccountCommand 执行账户管理命令，并把非零退出转换为清晰错误。
func runServiceAccountCommand(ctx context.Context, runner systemd.Runner, name string, args ...string) error {
	result, err := runner.Run(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%s failed: %s", name, serviceAccountCommandOutput(result))
	}
	return nil
}

// serviceAccountCommandOutput 合并外部命令输出，用于错误提示。
func serviceAccountCommandOutput(result systemd.Result) string {
	output := strings.TrimSpace(strings.TrimSpace(result.Stderr) + "\n" + strings.TrimSpace(result.Stdout))
	if output == "" {
		return fmt.Sprintf("exit code %d", result.ExitCode)
	}
	return output
}

// repairServiceMetadata 在 Linux root 操作后修复标准路径 owner 和 mode。
func repairServiceMetadata(config domain.GlobalConfig) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil
	}
	uid, gid, err := serviceAccountOwnerIDsFunc()
	if err != nil {
		return err
	}
	return systemd.RepairStandardMetadata(config, uid, gid, systemd.MetadataFixer{})
}

// repairServiceMetadataForConfigPath 加载配置并修复 root 写配置后留下的标准路径 metadata。
func repairServiceMetadataForConfigPath(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	return repairServiceMetadataFunc(cfg)
}

// serviceAccountOwnerIDs 解析 proxystack 运行用户和组的数字 ID。
func serviceAccountOwnerIDs() (int, int, error) {
	serviceUser, err := user.Lookup(systemd.DefaultServiceUser)
	if err != nil {
		return 0, 0, fmt.Errorf("service user %s is missing; run psctl init first: %w", systemd.DefaultServiceUser, err)
	}
	serviceGroup, err := user.LookupGroup(systemd.DefaultServiceGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("service group %s is missing; run psctl init first: %w", systemd.DefaultServiceGroup, err)
	}
	uid, err := strconv.Atoi(serviceUser.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("service user %s has invalid uid %q: %w", systemd.DefaultServiceUser, serviceUser.Uid, err)
	}
	gid, err := strconv.Atoi(serviceGroup.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("service group %s has invalid gid %q: %w", systemd.DefaultServiceGroup, serviceGroup.Gid, err)
	}
	return uid, gid, nil
}
