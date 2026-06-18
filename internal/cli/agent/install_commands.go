package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/install"
	"github.com/spf13/cobra"
)

// newInstallCommand 创建 install 或 update 命令。
func newInstallCommand(update bool) *cobra.Command {
	name := "install"
	if update {
		name = "update"
	}
	var version string
	var source string
	var sha256 string
	var archiveMember string
	var wheel string
	command := &cobra.Command{
		Use:   name + " mihomo|xray|geo|all",
		Short: name + " managed proxystack binaries or geo data",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if args[0] == install.TargetSelf {
				if !update {
					return fmt.Errorf("self is only supported by update")
				}
				return install.SelfUpdate(context.Background(), wheel, sha256, nil)
			}
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return err
			}
			progressPrinter := newInstallProgressPrinter(command.ErrOrStderr())
			defer progressPrinter.Finish()
			results, err := (install.Installer{Progress: progressPrinter.Print}).InstallTarget(context.Background(), install.Request{
				Config:        cfg,
				Target:        args[0],
				Version:       version,
				Source:        source,
				SHA256:        sha256,
				ArchiveMember: archiveMember,
				Force:         update,
			})
			if err != nil {
				return err
			}
			if err := repairServiceMetadata(cfg); err != nil {
				return err
			}
			for _, result := range results {
				if result.Skipped {
					fmt.Fprintf(command.OutOrStdout(), "%s skipped\n", result.Target)
					continue
				}
				fmt.Fprintf(command.OutOrStdout(), "%s installed: %v\n", result.Target, result.Written)
			}
			return nil
		},
	}
	command.Flags().StringVar(&version, "version", "", "Managed source version")
	command.Flags().StringVar(&source, "source", "", "Managed source or URL")
	command.Flags().StringVar(&sha256, "sha256", "", "Expected SHA256 for ordinary remote sources")
	command.Flags().StringVar(&archiveMember, "archive-member", "", "Archive member to install")
	if update {
		command.Use = "update mihomo|xray|geo|all|self"
		command.Flags().StringVar(&wheel, "wheel", "", "Wheel file or package spec for self update")
	}
	return command
}

type installProgressPrinter struct {
	writer               io.Writer
	currentProgressWidth int
}

func newInstallProgressPrinter(writer io.Writer) *installProgressPrinter {
	return &installProgressPrinter{writer: writer}
}

// Print 输出 install/update 进度；下载进度在交互式终端内复用单行刷新。
func (p *installProgressPrinter) Print(message string) {
	if isDownloadProgressMessage(message) && p.isInteractive() {
		p.writeDownloadProgress(message)
		return
	}
	p.Finish()
	fmt.Fprintln(p.writer, message)
}

func (p *installProgressPrinter) isInteractive() bool {
	file, ok := p.writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p *installProgressPrinter) writeDownloadProgress(message string) {
	paddedMessage := message
	if len(message) < p.currentProgressWidth {
		paddedMessage = message + strings.Repeat(" ", p.currentProgressWidth-len(message))
	}
	fmt.Fprintf(p.writer, "\r%s", paddedMessage)
	if len(message) > p.currentProgressWidth {
		p.currentProgressWidth = len(message)
	}
	if strings.HasPrefix(message, "download: complete ") {
		p.Finish()
	}
}

// Finish 结束尚未换行的交互式下载进度，避免后续输出粘连。
func (p *installProgressPrinter) Finish() {
	if p.currentProgressWidth == 0 {
		return
	}
	fmt.Fprintln(p.writer)
	p.currentProgressWidth = 0
}

func isDownloadProgressMessage(message string) bool {
	return strings.HasPrefix(message, "download: start ") ||
		strings.HasPrefix(message, "download: progress ") ||
		strings.HasPrefix(message, "download: complete ")
}
