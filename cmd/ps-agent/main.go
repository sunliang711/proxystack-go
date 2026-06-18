package main

import (
	"os"

	"github.com/eagle/proxystack-go/internal/cli/agent"
	"github.com/eagle/proxystack-go/internal/cli/runtime"
)

// main 只负责启动 ps-agent 命令树，业务逻辑由 internal/cli/agent 承载。
func main() {
	os.Exit(runtime.Run(agent.NewRootCommand()))
}
