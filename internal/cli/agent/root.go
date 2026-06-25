package agent

import (
	"fmt"
	"path/filepath"

	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/eagle/proxystack-go/internal/version"
	"github.com/spf13/cobra"
)

const defaultBaseDir = "/opt/proxystack"
const defaultServiceManager = servicemanager.ManagerAuto

var agentServiceManagerFactory = servicemanager.NewManager

const (
	agentConfigGroup       = "config"
	agentInstallGroup      = "install"
	agentValidateGroup     = "validate"
	agentServiceGroup      = "service"
	agentSubscriptionGroup = "subscription"
	agentDiagnosticGroup   = "diagnostic"
	agentHelpGroup         = "help"
)

// NewRootCommand 创建 ps-agent 的根命令和 T01 要求的最小命令树。
func NewRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "ps-agent",
		Short:         "Manage proxystack agent configuration and runtime plans",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.AddGroup(
		&cobra.Group{ID: agentConfigGroup, Title: "配置管理"},
		&cobra.Group{ID: agentInstallGroup, Title: "安装更新"},
		&cobra.Group{ID: agentValidateGroup, Title: "校验与渲染"},
		&cobra.Group{ID: agentServiceGroup, Title: "服务控制"},
		&cobra.Group{ID: agentSubscriptionGroup, Title: "订阅发布"},
		&cobra.Group{ID: agentDiagnosticGroup, Title: "诊断工具"},
		&cobra.Group{ID: agentHelpGroup, Title: "其它"},
	)
	command.SetHelpCommandGroupID(agentHelpGroup)
	command.SetCompletionCommandGroupID(agentHelpGroup)
	command.PersistentFlags().String("base-dir", defaultBaseDir, "Agent base directory")
	command.PersistentFlags().String("service-manager", defaultServiceManager, "Service manager: auto, systemd, launchd")
	command.AddCommand(groupedCommand(agentInstallGroup, newVersionCommand("ps-agent")))
	command.AddCommand(groupedCommand(agentConfigGroup, newInitCommand()))
	command.AddCommand(groupedCommand(agentInstallGroup, newSetupCommand()))
	command.AddCommand(groupedCommand(agentInstallGroup, newUninstallCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newAddCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newExampleCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newConfigCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newListCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newCloneCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newMemberCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newRemoveCommand()))
	command.AddCommand(groupedCommand(agentValidateGroup, newValidateCommand()))
	command.AddCommand(groupedCommand(agentValidateGroup, newCheckCommand()))
	command.AddCommand(groupedCommand(agentValidateGroup, newRenderCommand()))
	command.AddCommand(groupedCommand(agentValidateGroup, newExportConfigCommand()))
	command.AddCommand(groupedCommand(agentSubscriptionGroup, newSubCommand()))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("start")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("stop")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("restart")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("status")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("logs")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("enable")))
	command.AddCommand(groupedCommand(agentServiceGroup, newLifecycleCommand("disable")))
	command.AddCommand(groupedCommand(agentServiceGroup, newServiceCommand()))
	command.AddCommand(groupedCommand(agentInstallGroup, newInstallCommand(false)))
	command.AddCommand(groupedCommand(agentInstallGroup, newInstallCommand(true)))
	command.AddCommand(groupedCommand(agentConfigGroup, newExportCommand()))
	command.AddCommand(groupedCommand(agentConfigGroup, newImportCommand()))
	command.AddCommand(groupedCommand(agentValidateGroup, newDoctorCommand()))
	command.AddCommand(groupedCommand(agentDiagnosticGroup, newIPInfoCommand()))
	return command
}

// agentBaseDir 读取继承的 agent base dir flag，并解析为绝对路径。
func agentBaseDir(command *cobra.Command) (string, error) {
	flag := command.Flag("base-dir")
	if flag == nil {
		return "", fmt.Errorf("flag accessed but not defined: base-dir")
	}
	baseDir := flag.Value.String()
	if baseDir == "" {
		baseDir = defaultBaseDir
	}
	return filepath.Abs(baseDir)
}

// agentConfigPath 根据全局 base dir 返回固定的 agent 配置文件路径。
func agentConfigPath(command *cobra.Command) (string, error) {
	baseDir, err := agentBaseDir(command)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, "config.yaml"), nil
}

// agentServiceManager 创建当前命令指定的服务管理器。
func agentServiceManager(command *cobra.Command) (servicemanager.Manager, error) {
	flag := command.Flag("service-manager")
	if flag == nil {
		return nil, fmt.Errorf("flag accessed but not defined: service-manager")
	}
	return agentServiceManagerFactory(flag.Value.String())
}

// groupedCommand 给根命令子命令设置 usage 分组，保持 ps-agent help 与 Python 版面板一致。
func groupedCommand(groupID string, command *cobra.Command) *cobra.Command {
	command.GroupID = groupID
	return command
}

// newVersionCommand 创建只读 version 子命令。
func newVersionCommand(binary string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(command *cobra.Command, args []string) {
			fmt.Fprintln(command.OutOrStdout(), version.Info(binary))
		},
	}
}
