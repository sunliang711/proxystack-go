package agent

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/spf13/cobra"
)

// newSubCommand 创建 agent 侧订阅发布子命令集合。
func newSubCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "sub",
		Short: "Export and validate subscription data",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newSubExportCommand())
	command.AddCommand(newSubValidateInputsCommand())
	return command
}

// newSubExportCommand 创建订阅发布包导出命令。
func newSubExportCommand() *cobra.Command {
	var outputPath string
	var summary bool
	var dryRun bool
	command := &cobra.Command{
		Use:   "export [STACK]",
		Short: "Export a subscription bundle",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			stackName := optionalArg(args)
			result, err := buildSubExport(configPath, stackName, outputPath, time.Now().Local().Format(time.RFC3339))
			if err != nil {
				return err
			}
			if summary || dryRun {
				fmt.Fprintf(command.OutOrStdout(), "Subscription bundle summary: output=%s inputs=%d\n", result.OutputPath, len(result.InputFiles))
				for _, inputFile := range result.InputFiles {
					fmt.Fprintf(command.OutOrStdout(), "- %s bytes=%d\n", inputFile.Name, len(inputFile.Content))
				}
				return nil
			}
			manifest, err := subgen.WriteBundle(result.OutputPath, result.Source, result.GeneratedAt, result.InputFiles)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Subscription bundle exported: %s inputs=%d schema=%s\n", result.OutputPath, len(result.InputFiles), manifest.BundleSchema)
			return nil
		},
	}
	command.Flags().StringVarP(&outputPath, "output", "o", "", "Output bundle path")
	command.Flags().BoolVar(&summary, "summary", false, "Print bundle summary without writing zip")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Validate export without writing zip")
	return command
}

// newExportConfigCommand 创建 agent 顶层订阅文本渲染命令。
func newExportConfigCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "export-config sub|premium_sub|surge_sub USER",
		Short: "Render one user's subscription text",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			text, err := renderSubConfig(configPath, args[0], args[1], time.Now().Local().Format(time.RFC3339))
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(command.OutOrStdout(), text)
			return err
		},
	}
}

// newSubValidateInputsCommand 创建订阅 input 目录校验命令。
func newSubValidateInputsCommand() *cobra.Command {
	var inputDir string
	command := &cobra.Command{
		Use:   "validate-inputs",
		Short: "Validate subscription input files",
		RunE: func(command *cobra.Command, args []string) error {
			if inputDir == "" {
				return fmt.Errorf("--input-dir is required")
			}
			index, err := subgen.MergeInputFiles(inputDir, subgen.Access{Type: "none"}, time.Now().Local().Format(time.RFC3339))
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Subscription inputs OK: sources=%d nodes=%d users=%d\n", len(index.Sources), len(index.Nodes), len(index.Users))
			return nil
		},
	}
	command.Flags().StringVar(&inputDir, "input-dir", "", "Subscription input directory")
	return command
}

// subExportResult 保存一次 sub export 的中间结果。
type subExportResult struct {
	OutputPath  string
	Source      string
	GeneratedAt string
	InputFiles  []subgen.BundleInputFile
}

// buildSubExport 生成订阅发布包输入并计算默认输出路径。
func buildSubExport(configPath string, stackName string, outputPath string, generatedAt string) (subExportResult, error) {
	cfg, stackSet, err := loadSubExportStackSet(configPath)
	if err != nil {
		return subExportResult{}, err
	}
	if cfg.ExternalHost == "" {
		return subExportResult{}, fmt.Errorf("external_host is required for sub export")
	}
	inputFiles, source, err := buildSubBundleInputs(stackSet, stackName, generatedAt)
	if err != nil {
		return subExportResult{}, err
	}
	if len(inputFiles) == 0 {
		return subExportResult{}, fmt.Errorf("subscription export has no nodes")
	}
	if outputPath == "" {
		outputPath = defaultSubBundlePath(cfg, stackName)
	}
	return subExportResult{OutputPath: outputPath, Source: source, GeneratedAt: generatedAt, InputFiles: inputFiles}, nil
}

// loadSubExportStackSet 加载 agent 配置和 stacks，sub export 不探测系统端口。
func loadSubExportStackSet(configPath string) (domain.GlobalConfig, domain.StackSet, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, err
	}
	return cfg, stackSet, nil
}

// buildSubBundleInputs 按 all 或单 stack 生成 bundle inputs。
func buildSubBundleInputs(stackSet domain.StackSet, stackName string, generatedAt string) ([]subgen.BundleInputFile, string, error) {
	if stackName != "" {
		input, err := subgen.RenderSingleStackInputAt(stackSet, stackName, generatedAt)
		if err != nil {
			return nil, "", err
		}
		if len(input.Nodes) == 0 {
			return nil, "", fmt.Errorf("subscription input has no nodes: %s", stackName)
		}
		return []subgen.BundleInputFile{{Name: stackName + ".yaml", Content: []byte(subgen.InputToYAML(input))}}, stackName, nil
	}
	inputFiles := make([]subgen.BundleInputFile, 0)
	mergeInputs := make([]subgen.InputFile, 0)
	for _, stack := range stackSet.Stacks {
		input, err := subgen.RenderSingleStackInputAt(stackSet, stack.Name, generatedAt)
		if err != nil {
			return nil, "", err
		}
		if len(input.Nodes) == 0 {
			continue
		}
		name := stack.Name + ".yaml"
		inputFiles = append(inputFiles, subgen.BundleInputFile{Name: name, Content: []byte(subgen.InputToYAML(input))})
		mergeInputs = append(mergeInputs, subgen.InputFile{Name: name, Input: input})
	}
	if _, err := subgen.MergeInputs(mergeInputs, subgen.Access{Type: "none"}, generatedAt); err != nil {
		return nil, "", err
	}
	return inputFiles, stackSet.Config.Subscription.Source, nil
}

// defaultSubBundlePath 返回 sub export 的默认输出路径。
func defaultSubBundlePath(cfg domain.GlobalConfig, stackName string) string {
	name := "sub-bundle.zip"
	if stackName != "" {
		name = stackName + "-sub-bundle.zip"
	}
	return filepath.Join(cfg.ResolvePath(cfg.Paths.Publish), name)
}

// renderSubConfig 渲染指定用户的一类订阅文本。
func renderSubConfig(configPath string, configType string, user string, generatedAt string) (string, error) {
	_, stackSet, err := loadSubExportStackSet(configPath)
	if err != nil {
		return "", err
	}
	input, err := subgen.RenderStackInputAt(stackSet, stackSet.Config.Subscription.Source, generatedAt)
	if err != nil {
		return "", err
	}
	index, err := subgen.MergeInputs([]subgen.InputFile{{Name: "local.yaml", Input: input}}, subgen.Access{Type: "none"}, generatedAt)
	if err != nil {
		return "", err
	}
	switch configType {
	case "sub":
		return subgen.RenderClashSubscription(index, user, "", "")
	case "premium_sub":
		return subgen.RenderPremiumClashSubscription(index, user, "", "")
	case "surge_sub":
		return subgen.RenderSurgeSubscription(index, user, "", "", "", 86400, true)
	default:
		return "", fmt.Errorf("subscription config type must be one of: sub, premium_sub, surge_sub")
	}
}
