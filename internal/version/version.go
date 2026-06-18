package version

const (
	// Name 是当前 Go 重写项目的产品名。
	Name = "proxystack"
	// Version 是未发布开发构建的默认版本号。
	Version = "0.1.0-dev"
)

// Info 返回 CLI version 命令展示的稳定版本字符串。
func Info(binary string) string {
	return binary + " " + Version
}
