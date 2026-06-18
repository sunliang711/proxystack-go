package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/graph"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/spf13/cobra"
)

// newLifecycleCommand 创建顶层生命周期命令。
func newLifecycleCommand(action string) *cobra.Command {
	var follow bool
	command := &cobra.Command{
		Use:   action + " [TARGET]",
		Short: "Run service manager " + action + " for proxystack services",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runLifecycle(command, action, optionalArg(args), follow)
		},
	}
	if action == "logs" {
		command.Aliases = []string{"log"}
		command.Flags().BoolVarP(&follow, "follow", "f", false, "Follow journal output")
	}
	return command
}

// newServiceCommand 创建 service wrapper 子命令集合。
func newServiceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "service",
		Short: "Manage proxystack service files and services",
	}
	command.AddCommand(newServiceInstallCommand(false))
	command.AddCommand(newServiceInstallCommand(true))
	for _, action := range []string{"start", "stop", "restart", "status", "enable", "disable"} {
		command.AddCommand(newServiceActionCommand(action))
	}
	command.AddCommand(newServiceActionCommand("logs"))
	return command
}

// newServiceInstallCommand 创建 service install/uninstall 子命令。
func newServiceInstallCommand(uninstall bool) *cobra.Command {
	name := "install"
	if uninstall {
		name = "uninstall"
	}
	return &cobra.Command{
		Use:   name + " [TARGET]",
		Short: name + " service files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			manager, err := agentServiceManager(command)
			if err != nil {
				return err
			}
			target := optionalArg(args)
			if uninstall {
				paths, err := manager.UninstallUnits(target)
				if err != nil {
					return err
				}
				fmt.Fprintf(command.OutOrStdout(), "Uninstalled units: %v\n", paths)
				return nil
			}
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return err
			}
			paths, err := manager.InstallUnits(cfg, target)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Installed units: %v\n", paths)
			return nil
		},
	}
}

// runLifecycle 执行顶层或 service wrapper 生命周期动作。
func runLifecycle(command *cobra.Command, action string, target string, follow bool) error {
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if target == "sub" {
		return runServiceAction(command, ctx, manager, action, []string{manager.SubService()}, follow)
	}
	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{ConfigPath: configPath, Target: target, SkipSystemPorts: true})
	if err != nil {
		return err
	}
	services := manager.ServicesForNodes(plan.Scope.Nodes)
	if action == "start" || action == "restart" {
		if err := checkRequiredBinaries(plan); err != nil {
			return err
		}
		if err := agentruntime.ApplyPlan(plan); err != nil {
			return err
		}
		if err := repairServiceMetadata(plan.Config); err != nil {
			return err
		}
	}
	return runServiceAction(command, ctx, manager, action, services, follow)
}

// newServiceActionCommand 创建不写 runtime 的 service wrapper 子命令。
func newServiceActionCommand(action string) *cobra.Command {
	var follow bool
	command := &cobra.Command{
		Use:   action + " [TARGET]",
		Short: "Run service manager " + action + " without applying runtime plan",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			manager, err := agentServiceManager(command)
			if err != nil {
				return err
			}
			services, err := resolveServiceNames(configPath, optionalArg(args), manager)
			if err != nil {
				return err
			}
			return runServiceAction(command, context.Background(), manager, action, services, follow)
		},
	}
	if action == "logs" {
		command.Aliases = []string{"log"}
		command.Flags().BoolVarP(&follow, "follow", "f", false, "Follow journal output")
	}
	return command
}

// resolveServiceNames 只解析 target 到服务名称，不生成或写入 runtime 文件。
func resolveServiceNames(configPath string, target string, manager servicemanager.Manager) ([]string, error) {
	if target == "sub" {
		return []string{manager.SubService()}, nil
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return nil, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return nil, err
	}
	scope, err := graph.ResolveTargetScope(referenceGraph, target)
	if err != nil {
		return nil, err
	}
	return manager.ServicesForNodes(scope.Nodes), nil
}

// runServiceAction 分派具体服务管理操作并输出 status/logs 内容。
func runServiceAction(command *cobra.Command, ctx context.Context, manager servicemanager.Manager, action string, services []string, follow bool) error {
	switch action {
	case "start":
		return manager.Start(ctx, services)
	case "stop":
		return manager.Stop(ctx, services)
	case "restart":
		return manager.Restart(ctx, services)
	case "status":
		result, err := manager.Status(ctx, services)
		writeCommandResult(command, result)
		return err
	case "logs":
		result, err := manager.Logs(ctx, services, follow)
		writeCommandResult(command, result)
		return err
	case "enable":
		return manager.Enable(ctx, services)
	case "disable":
		return manager.Disable(ctx, services)
	default:
		return fmt.Errorf("unsupported lifecycle action: %s", action)
	}
}

// writeCommandResult 把服务管理器捕获输出转发到 Cobra 的 stdout/stderr。
func writeCommandResult(command *cobra.Command, result servicemanager.Result) {
	if result.Stdout != "" {
		fmt.Fprint(command.OutOrStdout(), result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(command.ErrOrStderr(), result.Stderr)
	}
}

// checkRequiredBinaries 确认 target scope 需要的核心二进制存在且可执行。
func checkRequiredBinaries(plan agentruntime.Plan) error {
	return checkRequiredBinariesForNodes(plan, plan.Scope.Nodes)
}

// checkRequiredBinariesForNodes 确认指定节点需要的核心二进制存在且可执行。
func checkRequiredBinariesForNodes(plan agentruntime.Plan, nodes []graph.ServiceNode) error {
	required := map[string]string{}
	for _, node := range nodes {
		switch node.Component {
		case "xrelay":
			required["xray"] = filepath.Join(plan.Config.ResolvePath(plan.Config.Paths.Bin), "xray")
		case "clash":
			required["mihomo"] = filepath.Join(plan.Config.ResolvePath(plan.Config.Paths.Bin), "mihomo")
		}
	}
	for name, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("%s binary is required before start: %s (%w)", name, path, err)
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("%s binary is not executable: %s", name, path)
		}
	}
	return nil
}
