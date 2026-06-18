package templates

import "embed"

// SubFS 保存订阅默认模板，供订阅生成器作为内置模板读取。
//
//go:embed sub/*.j2
var SubFS embed.FS
