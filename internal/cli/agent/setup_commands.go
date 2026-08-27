package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/install"
	"github.com/spf13/cobra"
)

var setupInstallTargetFunc = func(ctx context.Context, request install.Request, progress install.Progress) ([]install.Result, error) {
	return (install.Installer{Progress: progress}).InstallTarget(ctx, request)
}

// newSetupCommand 创建本地初始化、托管依赖安装及组合安装命令。
func newSetupCommand() *cobra.Command {
	var externalHost string
	var force bool
	command := &cobra.Command{
		Use:   "setup",
		Short: "Initialize local files, install service files, and install managed targets",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSetupAll(command, externalHost, force)
		},
	}
	addSetupLocalFlags(command, &externalHost, &force)
	command.AddCommand(newSetupLocalCommand(&externalHost, &force))
	command.AddCommand(newSetupDepsCommand())
	command.AddCommand(newSetupAllCommand(&externalHost, &force))
	return command
}

// newSetupLocalCommand 创建只初始化本地文件和安装 service unit 的 setup 子命令。
func newSetupLocalCommand(externalHost *string, force *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "local",
		Short: "Initialize local files and install service files",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSetupLocal(command, *externalHost, *force)
		},
	}
	addSetupLocalFlags(command, externalHost, force)
	return command
}

// newSetupDepsCommand 创建只安装托管依赖的 setup 子命令。
func newSetupDepsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "deps",
		Short: "Install managed proxystack binaries and geo data",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSetupDeps(command)
		},
	}
}

// newSetupAllCommand 创建按 local -> deps 顺序执行的 setup 子命令。
func newSetupAllCommand(externalHost *string, force *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "all",
		Short: "Run setup local and setup deps",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSetupAll(command, *externalHost, *force)
		},
	}
	addSetupLocalFlags(command, externalHost, force)
	return command
}

// addSetupLocalFlags 注册本地初始化相关 flag，兼容 setup、setup local 和 setup all。
func addSetupLocalFlags(command *cobra.Command, externalHost *string, force *bool) {
	command.Flags().StringVar(externalHost, "external-host", "", "External subscription host")
	command.Flags().BoolVar(force, "force", false, "Overwrite existing config")
}

// runSetupAll 默认按 local -> deps 顺序完成完整 setup。
func runSetupAll(command *cobra.Command, externalHost string, force bool) error {
	if err := runSetupLocal(command, externalHost, force); err != nil {
		return err
	}
	return runSetupDeps(command)
}

// runSetupLocal 初始化本地目录和配置，修复 metadata，并安装 service unit。
func runSetupLocal(command *cobra.Command, externalHost string, force bool) error {
	baseDir, err := agentBaseDir(command)
	if err != nil {
		return err
	}
	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	if err := ensureServiceAccountForInit(context.Background(), baseDir); err != nil {
		return fmt.Errorf("setup service account failed: %w", err)
	}
	// 服务组可能刚由上一步创建，提示必须放在它之后才判断得出成员关系。
	printServiceGroupHint(command.OutOrStdout(), baseDir)
	if err := runSetupInit(baseDir, externalHost, force); err != nil {
		return fmt.Errorf("setup local init failed: %w", err)
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("setup config load failed: %w", err)
	}
	if err := repairServiceMetadata(cfg); err != nil {
		return fmt.Errorf("setup metadata repair failed: %w", err)
	}
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	paths, err := manager.InstallUnits(cfg, "")
	if err != nil {
		return fmt.Errorf("setup service install failed: %w", err)
	}
	fmt.Fprintf(command.OutOrStdout(), "Installed units: %v\n", paths)
	return nil
}

// runSetupDeps 安装 xray、mihomo、geo 等托管依赖，目标固定为 all。
func runSetupDeps(command *cobra.Command) error {
	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("setup config load failed: %w", err)
	}
	progressPrinter := newInstallProgressPrinter(command.ErrOrStderr())
	defer progressPrinter.Finish()
	results, err := setupInstallTargetFunc(context.Background(), install.Request{Config: cfg, Target: install.TargetAll}, progressPrinter.Print)
	if err != nil {
		return fmt.Errorf("setup deps failed: %w", err)
	}
	if err := repairServiceMetadata(cfg); err != nil {
		return fmt.Errorf("setup install metadata repair failed: %w", err)
	}
	for _, result := range results {
		if result.Skipped {
			fmt.Fprintf(command.OutOrStdout(), "%s skipped\n", result.Target)
			continue
		}
		fmt.Fprintf(command.OutOrStdout(), "%s installed: %v\n", result.Target, result.Written)
	}
	return nil
}

// runSetupInit 初始化项目；已有 config 时只补齐标准目录并继续后续安装。
func runSetupInit(baseDir string, externalHost string, force bool) error {
	options := agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: externalHost, Force: force}
	if err := agentconfig.InitProject(options); err != nil {
		if !errors.Is(err, agentconfig.ErrConfigAlreadyExists) {
			return err
		}
		return agentconfig.EnsureProjectLayout(options)
	}
	return nil
}
