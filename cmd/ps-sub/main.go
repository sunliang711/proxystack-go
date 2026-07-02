package main

import (
	"os"

	"github.com/eagle/proxystack-go/internal/cli/runtime"
	"github.com/eagle/proxystack-go/internal/cli/sub"
)

// main 只负责启动 pssub 命令树，业务逻辑由 internal/cli/sub 承载。
func main() {
	os.Exit(runtime.Run(sub.NewRootCommand()))
}
