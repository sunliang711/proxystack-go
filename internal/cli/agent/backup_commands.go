package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	backupgen "github.com/eagle/proxystack-go/internal/generator/backup"
	"github.com/eagle/proxystack-go/internal/graph"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/spf13/cobra"
)

type importServiceCandidate struct {
	node    graph.ServiceNode
	service string
}

// newExportCommand 创建原生 agent 配置备份导出命令。
func newExportCommand() *cobra.Command {
	var outputPath string
	command := &cobra.Command{
		Use:   "export",
		Short: "Export a native agent backup",
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return err
			}
			output := nativeBackupOutputPath(cfg, outputPath)
			manifest, err := backupgen.WriteNativeBackup(output, configPath, cfg.StacksDir(), time.Now().Local().Format(time.RFC3339))
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Native backup exported: %s files=%d schema=%s\n", output, len(manifest.FilesSHA256), manifest.BackupSchema)
			return nil
		},
	}
	command.Flags().StringVarP(&outputPath, "output", "o", "", "Output backup path")
	return command
}

// newImportCommand 创建原生 agent 配置备份恢复命令。
func newImportCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "import BACKUP",
		Short: "Import a native agent backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			targetBaseDir, err := agentBaseDir(command)
			if err != nil {
				return err
			}
			if !force {
				if err := ensureNativeBackupTargetIsEmpty(targetBaseDir); err != nil {
					return err
				}
			}
			if err := stopRunningServicesBeforeImport(command, targetBaseDir); err != nil {
				return err
			}
			manifest, err := backupgen.RestoreNativeBackup(args[0], targetBaseDir)
			if err != nil {
				return err
			}
			if err := repairServiceMetadataForConfigPath(filepath.Join(targetBaseDir, "config.yaml")); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Native backup imported: %s base-dir=%s files=%d schema=%s\n", args[0], targetBaseDir, len(manifest.FilesSHA256), manifest.BackupSchema)
			return nil
		},
	}
	command.Flags().BoolVar(&force, "force", false, "Overwrite existing config and stacks")
	return command
}

// nativeBackupOutputPath 返回原生备份默认输出路径或用户指定路径。
func nativeBackupOutputPath(cfg domain.GlobalConfig, outputPath string) string {
	if outputPath != "" {
		return outputPath
	}
	return filepath.Join(cfg.ResolvePath(cfg.Paths.Publish), "proxystack-backup.zip")
}

// ensureNativeBackupTargetIsEmpty 在未传 --force 时保护已有 config 和 stack 文件。
func ensureNativeBackupTargetIsEmpty(baseDir string) error {
	configPath := filepath.Join(baseDir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("target config already exists; use --force to overwrite: %s", configPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	stackPaths, err := filepath.Glob(filepath.Join(baseDir, "stacks", "*.yaml"))
	if err != nil {
		return err
	}
	if len(stackPaths) > 0 {
		return fmt.Errorf("target stacks already exist; use --force to overwrite: %s", filepath.Join(baseDir, "stacks"))
	}
	return nil
}

// stopRunningServicesBeforeImport 基于导入前的旧配置停止仍处于 active 的 stack 服务。
func stopRunningServicesBeforeImport(command *cobra.Command, baseDir string) error {
	configPath := filepath.Join(baseDir, "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	ctx := context.Background()
	candidates, err := importServiceCandidates(configPath, manager)
	if err != nil {
		return err
	}
	activeCandidates := make([]importServiceCandidate, 0)
	activeServices := make([]string, 0)
	for _, candidate := range candidates {
		active, err := manager.IsActive(ctx, candidate.service)
		if err != nil {
			return fmt.Errorf("check running service before import failed: %s: %w", candidate.service, err)
		}
		if active {
			activeCandidates = append(activeCandidates, candidate)
			activeServices = append(activeServices, candidate.service)
		}
	}
	if len(activeServices) == 0 {
		fmt.Fprintln(command.OutOrStdout(), "No running services to stop before import.")
		return nil
	}
	fmt.Fprintln(command.OutOrStdout(), "Stopping running services before import:")
	for _, candidate := range activeCandidates {
		fmt.Fprintf(command.OutOrStdout(), "- %s -> %s\n", candidate.node.Label(), candidate.service)
	}
	if err := manager.Stop(ctx, activeServices); err != nil {
		return fmt.Errorf("stop running services before import failed: %w", err)
	}
	return nil
}

// importServiceCandidates 从旧 stack 文件推导导入前需要检查的服务候选集合。
func importServiceCandidates(configPath string, manager servicemanager.Manager) ([]importServiceCandidate, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	stackPaths, err := filepath.Glob(filepath.Join(cfg.StacksDir(), "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(stackPaths)
	nodes := make([]graph.ServiceNode, 0, len(stackPaths)*2)
	for _, stackPath := range stackPaths {
		stack, err := config.LoadStack(stackPath)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes,
			graph.ServiceNode{Stack: stack.Name, Component: "xrelay"},
			graph.ServiceNode{Stack: stack.Name, Component: "clash"},
		)
	}
	services := manager.ServicesForNodes(nodes)
	if len(services) != len(nodes) {
		return nil, fmt.Errorf("service manager returned %d services for %d nodes", len(services), len(nodes))
	}
	candidates := make([]importServiceCandidate, 0, len(nodes))
	for index, node := range nodes {
		candidates = append(candidates, importServiceCandidate{node: node, service: services[index]})
	}
	return candidates, nil
}
