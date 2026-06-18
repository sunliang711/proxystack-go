package runtime

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const (
	// ExitOK 表示命令成功完成。
	ExitOK = 0
	// ExitError 表示命令执行失败。
	ExitError = 1
)

// Run 统一执行 Cobra 命令并转换为进程 exit code。
func Run(command *cobra.Command) int {
	setupLogger()
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitError
	}
	return ExitOK
}

// setupLogger 初始化最小 Zerolog 配置，保持 CLI 日志输出为英文消息。
func setupLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}
