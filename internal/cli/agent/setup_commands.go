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

var setupRunLifecycleFunc = runLifecycle

// newSetupCommand 创建 init、install all 和 service install 的组合安装命令。
func newSetupCommand() *cobra.Command {
	var externalHost string
	var force bool
	var start bool
	command := &cobra.Command{
		Use:   "setup",
		Short: "Initialize config, install managed targets, and install service files",
		RunE: func(command *cobra.Command, args []string) error {
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
			if err := runSetupInit(baseDir, externalHost, force); err != nil {
				return fmt.Errorf("setup init failed: %w", err)
			}
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("setup config load failed: %w", err)
			}
			if err := repairServiceMetadata(cfg); err != nil {
				return fmt.Errorf("setup metadata repair failed: %w", err)
			}
			progressPrinter := newInstallProgressPrinter(command.ErrOrStderr())
			defer progressPrinter.Finish()
			results, err := setupInstallTargetFunc(context.Background(), install.Request{Config: cfg, Target: install.TargetAll}, progressPrinter.Print)
			if err != nil {
				return fmt.Errorf("setup install all failed: %w", err)
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
			manager, err := agentServiceManager(command)
			if err != nil {
				return err
			}
			paths, err := manager.InstallUnits(cfg, "")
			if err != nil {
				return fmt.Errorf("setup service install failed: %w", err)
			}
			fmt.Fprintf(command.OutOrStdout(), "Installed units: %v\n", paths)
			if start {
				if err := setupRunLifecycleFunc(command, "start", "", false); err != nil {
					return fmt.Errorf("setup start failed: %w", err)
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&externalHost, "external-host", "", "External subscription host")
	command.Flags().BoolVar(&force, "force", false, "Overwrite existing config")
	command.Flags().BoolVar(&start, "start", false, "Start proxystack services after setup")
	return command
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
