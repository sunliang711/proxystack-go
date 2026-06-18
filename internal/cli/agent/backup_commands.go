package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	backupgen "github.com/eagle/proxystack-go/internal/generator/backup"
	"github.com/spf13/cobra"
)

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
