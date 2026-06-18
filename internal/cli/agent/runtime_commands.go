package agent

import (
	"fmt"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	mihomogen "github.com/eagle/proxystack-go/internal/generator/mihomo"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	xraygen "github.com/eagle/proxystack-go/internal/generator/xray"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newValidateCommand 创建只读配置校验命令。
func newValidateCommand() *cobra.Command {
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "validate [TARGET]",
		Short: "Validate agent configuration and stacks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			target := optionalArg(args)
			if _, err := agentruntime.BuildPlan(agentruntime.BuildOptions{ConfigPath: configPath, Target: target, SkipSystemPorts: skipSystemPorts}); err != nil {
				return err
			}
			fmt.Fprintln(command.OutOrStdout(), "Validation OK")
			return nil
		},
	}
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// newCheckCommand 创建只读 runtime plan diff 命令。
func newCheckCommand() *cobra.Command {
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "check [TARGET]",
		Short: "Preview generated runtime file changes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{ConfigPath: configPath, Target: optionalArg(args), SkipSystemPorts: skipSystemPorts})
			if err != nil {
				return err
			}
			for _, change := range plan.Changes {
				fmt.Fprintf(command.OutOrStdout(), "%s %s %s %s -> %s\n", change.Action, change.RelativePath, change.Service, shortHash(change.OldSHA256), shortHash(change.NewSHA256))
			}
			services := plan.ChangedServices()
			if len(services) > 0 {
				fmt.Fprintf(command.OutOrStdout(), "changed_services: %v\n", services)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// newRenderCommand 创建只读模型和生成物渲染命令。
func newRenderCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "render",
		Short: "Render model or generated config to stdout",
	}
	command.AddCommand(newRenderModelCommand())
	command.AddCommand(newRenderXrelayCommand())
	command.AddCommand(newRenderClashCommand())
	command.AddCommand(newRenderSubCommand())
	return command
}

// newRenderModelCommand 创建 render model 子命令。
func newRenderModelCommand() *cobra.Command {
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "model",
		Short: "Render effective stack model",
		RunE: func(command *cobra.Command, args []string) error {
			stackSet, err := loadAgentStackSet(command, skipSystemPorts)
			if err != nil {
				return err
			}
			data, err := yaml.Marshal(stackSet)
			if err != nil {
				return err
			}
			_, err = command.OutOrStdout().Write(data)
			return err
		},
	}
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// newRenderXrelayCommand 创建 render xrelay 子命令。
func newRenderXrelayCommand() *cobra.Command {
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "xrelay STACK",
		Short: "Render Xray config for a stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			stackSet, err := loadAgentStackSet(command, skipSystemPorts)
			if err != nil {
				return err
			}
			output, err := xraygen.DumpsConfig(stackSet, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(command.OutOrStdout(), output)
			return err
		},
	}
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// newRenderClashCommand 创建 render clash 子命令。
func newRenderClashCommand() *cobra.Command {
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "clash STACK",
		Short: "Render mihomo config for a stack",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			stackSet, err := loadAgentStackSet(command, skipSystemPorts)
			if err != nil {
				return err
			}
			output, err := mihomogen.DumpsConfig(stackSet, args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(command.OutOrStdout(), output)
			return err
		},
	}
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// newRenderSubCommand 创建 render sub 子命令。
func newRenderSubCommand() *cobra.Command {
	var inputDir string
	var skipSystemPorts bool
	command := &cobra.Command{
		Use:   "sub",
		Short: "Render subscription index",
		RunE: func(command *cobra.Command, args []string) error {
			generatedAt := time.Now().Local().Format(time.RFC3339)
			var index subgen.Index
			var err error
			if inputDir != "" {
				index, err = subgen.MergeInputFiles(inputDir, subgen.Access{Type: "none"}, generatedAt)
			} else {
				stackSet, loadErr := loadAgentStackSet(command, skipSystemPorts)
				if loadErr != nil {
					return loadErr
				}
				input, renderErr := subgen.RenderStackInputAt(stackSet, stackSet.Config.Subscription.Source, generatedAt)
				if renderErr != nil {
					return renderErr
				}
				index, err = subgen.MergeInputs([]subgen.InputFile{{Name: "local.yaml", Input: input}}, subgen.Access{Type: "none"}, generatedAt)
			}
			if err != nil {
				return err
			}
			output, err := subgen.IndexToJSON(index)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(command.OutOrStdout(), output)
			return err
		},
	}
	command.Flags().StringVar(&inputDir, "input-dir", "", "Read subscription inputs from a directory")
	command.Flags().BoolVar(&skipSystemPorts, "skip-system-ports", false, "Skip probing live system ports")
	return command
}

// loadAgentStackSet 加载 agent 全局配置和所有 stack。
func loadAgentStackSet(command *cobra.Command, skipSystemPorts bool) (domain.StackSet, error) {
	configPath, err := agentConfigPath(command)
	if err != nil {
		return domain.StackSet{}, err
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return domain.StackSet{}, err
	}
	return config.LoadStacks(cfg, !skipSystemPorts)
}

// optionalArg 返回可选 target 参数。
func optionalArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// shortHash 输出 hash 前 8 位，空 hash 用短横线占位。
func shortHash(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
