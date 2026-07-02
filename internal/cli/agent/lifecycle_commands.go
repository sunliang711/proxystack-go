package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
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
		Short: "Run service manager " + action + " for stack services",
		Long:  lifecycleCommandLong(action),
		Example: "  psctl " + action + "\n" +
			"  psctl " + action + " usa1\n" +
			"  psctl " + action + " xrelay/usa1\n" +
			"  psctl " + action + " clash/usa1",
		Args: cobra.MaximumNArgs(1),
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
		Short: name + " stack service files",
		Long:  serviceInstallCommandLong(name),
		Example: "  psctl service " + name + "\n" +
			"  psctl service " + name + " xrelay/usa1\n" +
			"  psctl service " + name + " clash/usa1",
		Args: cobra.MaximumNArgs(1),
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
				cfg, err := serviceUninstallConfig(command, configPath, target)
				if err != nil {
					return err
				}
				paths, err := manager.UninstallUnits(cfg, target)
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

// serviceUninstallConfig 返回 service uninstall 使用的配置；空 target 允许在 config 缺失时清理通用 stack 服务文件。
func serviceUninstallConfig(command *cobra.Command, configPath string, target string) (domain.GlobalConfig, error) {
	if target != "" {
		return config.LoadConfig(configPath)
	}
	baseDir, err := agentBaseDir(command)
	if err != nil {
		return domain.GlobalConfig{}, err
	}
	return uninstallConfig(configPath, baseDir)
}

// runLifecycle 执行顶层或 service wrapper 生命周期动作。
func runLifecycle(command *cobra.Command, action string, target string, follow bool) error {
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	ctx := context.Background()
	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{ConfigPath: configPath, Target: target, SkipSystemPorts: true})
	if err != nil {
		return err
	}
	services := manager.ServicesForNodes(plan.Scope.Nodes)
	if shouldPrintServicePlan(action) {
		printServicePlan(command, action, target, plan.Scope.Nodes, services)
	} else if len(services) == 0 {
		printNoServicesMatched(command, target)
	}
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
	if len(services) == 0 {
		return nil
	}
	return runServiceAction(command, ctx, manager, action, services, follow)
}

// newServiceActionCommand 创建不写 runtime 的 service wrapper 子命令。
func newServiceActionCommand(action string) *cobra.Command {
	var follow bool
	command := &cobra.Command{
		Use:   action + " [TARGET]",
		Short: "Run service manager " + action + " for installed stack services",
		Long:  lifecycleCommandLong(action),
		Example: "  psctl service " + action + "\n" +
			"  psctl service " + action + " usa1\n" +
			"  psctl service " + action + " xrelay/usa1\n" +
			"  psctl service " + action + " clash/usa1",
		Args: cobra.MaximumNArgs(1),
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
			scope, services, err := resolveServiceTargets(configPath, target, manager)
			if err != nil {
				return err
			}
			if shouldPrintServicePlan(action) {
				printServicePlan(command, action, target, scope.Nodes, services)
			} else if len(services) == 0 {
				printNoServicesMatched(command, target)
			}
			if len(services) == 0 {
				return nil
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

// resolveServiceTargets 只解析 target 到 stack 服务节点和服务名称，不生成或写入 runtime 文件。
func resolveServiceTargets(configPath string, target string, manager servicemanager.Manager) (graph.TargetScope, []string, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return graph.TargetScope{}, nil, err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return graph.TargetScope{}, nil, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return graph.TargetScope{}, nil, err
	}
	scope, err := graph.ResolveTargetScope(referenceGraph, target)
	if err != nil {
		return graph.TargetScope{}, nil, err
	}
	return scope, manager.ServicesForNodes(scope.Nodes), nil
}

// resolveServiceNames 只解析 target 到服务名称，不生成或写入 runtime 文件。
func resolveServiceNames(configPath string, target string, manager servicemanager.Manager) ([]string, error) {
	_, services, err := resolveServiceTargets(configPath, target, manager)
	return services, err
}

// runServiceAction 分派具体服务管理操作并输出 status/logs 内容。
func runServiceAction(command *cobra.Command, ctx context.Context, manager servicemanager.Manager, action string, services []string, follow bool) error {
	if len(services) == 0 {
		return nil
	}
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

// lifecycleCommandLong 返回 stack 生命周期 target 的说明文本。
func lifecycleCommandLong(action string) string {
	return "Run service manager " + action + " for stack services.\n\n" +
		"TARGET rules:\n" +
		"  omitted       all enabled stack services\n" +
		"  NAME          xrelay and clash services for one stack\n" +
		"  xrelay/NAME   xray service for one stack\n" +
		"  clash/NAME    mihomo service for one stack"
}

// serviceInstallCommandLong 返回 service install/uninstall target 的说明文本。
func serviceInstallCommandLong(action string) string {
	return "Run service file " + action + " for stack services.\n\n" +
		"TARGET rules:\n" +
		"  omitted       all enabled stack services\n" +
		"  NAME          xrelay and clash services for one stack\n" +
		"  xrelay/NAME   xray service for one stack\n" +
		"  clash/NAME    mihomo service for one stack"
}

// shouldPrintServicePlan 判断动作执行前是否需要向用户展示服务计划。
func shouldPrintServicePlan(action string) bool {
	switch action {
	case "start", "stop", "restart", "enable", "disable":
		return true
	default:
		return false
	}
}

// printServicePlan 输出生命周期动作即将操作的 stack 组件和底层服务名。
func printServicePlan(command *cobra.Command, action string, target string, nodes []graph.ServiceNode, services []string) {
	writer := command.OutOrStdout()
	if len(services) == 0 {
		printNoServicesMatched(command, target)
		return
	}
	fmt.Fprintf(writer, "Service plan for %s (target: %s):\n", action, serviceTargetLabel(target))
	for index, service := range services {
		label := service
		if index < len(nodes) {
			label = nodes[index].Label()
		}
		fmt.Fprintf(writer, "- %s -> %s\n", label, service)
	}
}

// printNoServicesMatched 输出 target 未匹配任何启用服务的提示。
func printNoServicesMatched(command *cobra.Command, target string) {
	fmt.Fprintf(command.OutOrStdout(), "No services matched target: %s\n", serviceTargetLabel(target))
}

// serviceTargetLabel 返回适合 CLI 展示的 target 名称。
func serviceTargetLabel(target string) string {
	if target == "" {
		return "all enabled stacks"
	}
	return target
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
