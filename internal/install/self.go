package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// SelfRunner 抽象 self update 执行器，测试可注入 fake runner。
type SelfRunner interface {
	Run(ctx context.Context, wheel string) error
}

// CommandSelfRunner 使用 python3 -m pip install --upgrade 执行 self update。
type CommandSelfRunner struct{}

// Run 使用参数数组执行 pip 升级命令。
func (CommandSelfRunner) Run(ctx context.Context, wheel string) error {
	command := exec.CommandContext(ctx, "python3", "-m", "pip", "install", "--upgrade", wheel)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// SelfUpdate 校验本地 wheel 或 package spec 后执行 self update runner。
func SelfUpdate(ctx context.Context, wheel string, sha256 string, runner SelfRunner) error {
	if wheel == "" {
		return fmt.Errorf("self update requires --wheel")
	}
	if sha256 != "" {
		data, err := os.ReadFile(wheel)
		if err != nil {
			return err
		}
		if err := verifySHA256(data, sha256); err != nil {
			return err
		}
	}
	if runner == nil {
		runner = CommandSelfRunner{}
	}
	return runner.Run(ctx, wheel)
}
