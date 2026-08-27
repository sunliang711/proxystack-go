// Package xrayapi 通过受管 xray 二进制的 api 子命令修改运行中实例的用户。
//
// 这里不直接引入 xray-core 的 gRPC stub：xray-core 的依赖树对一个配置管理
// CLI 来说太重，而且编译期固定的 protobuf 版本会和用户实际安装的 xray 版本
// 脱节。调用 {bin}/xray 自带的 api 子命令天然与运行中的实例同版本。
package xrayapi

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/fsperm"
)

// DefaultTimeout 是单次 api 调用的等待上限。
const DefaultTimeout = 3 * time.Second

// successPattern 匹配 xray api adu/rmu 的结尾统计行，例如 "Removed 1 user(s) in total."。
//
// 这两个子命令对单个用户的失败只是 fmt.Println 出来就继续，进程照样退出 0，
// 所以退出码不能当成功信号，必须读这一行的计数。
var successPattern = regexp.MustCompile(`(?m)^(?:Removed|Added)\s+(\d+)\s+user\(s\) in total\.`)

// Client 调用某个 xray 实例的 API 端点。
type Client struct {
	Binary  string
	Server  string
	Timeout time.Duration
}

// CallError 表示一次 api 子命令调用失败，保留原始输出便于排查。
type CallError struct {
	Args   []string
	Output string
	Err    error
}

// Error 输出失败的命令、失败原因和 xray 的原始提示。
//
// 原因和原始输出都要保留：判定失败的往往是统计行（"Removed 0 user(s)"），
// 而真正说明为什么失败的是 xray 打在前面的那几行。
func (e CallError) Error() string {
	parts := []string{fmt.Sprintf("xray %s failed", strings.Join(e.Args, " "))}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if output := strings.TrimSpace(e.Output); output != "" {
		parts = append(parts, output)
	}
	return strings.Join(parts, ": ")
}

// Unwrap 暴露底层 exec 错误。
func (e CallError) Unwrap() error { return e.Err }

// BinaryPath 返回受管 xray 二进制路径。
func BinaryPath(config domain.GlobalConfig) string {
	return filepath.Join(config.ResolvePath(config.Paths.Bin), "xray")
}

// NewClient 按 stack 解析出的 API 监听地址构造客户端。
func NewClient(config domain.GlobalConfig, server string) Client {
	return Client{Binary: BinaryPath(config), Server: server, Timeout: DefaultTimeout}
}

// RemoveUser 从运行中的 inbound 上移除一个用户。
//
// 只影响新建连接，已经建立的连接会跑到自然结束。
func (c Client) RemoveUser(ctx context.Context, inboundTag string, email string) error {
	if err := checkArgumentValue("inbound tag", inboundTag); err != nil {
		return err
	}
	if err := checkArgumentValue("user email", email); err != nil {
		return err
	}
	return c.runCounted(ctx, "api", "rmu", "--server="+c.Server, "--timeout="+c.timeoutSeconds(), "-tag="+inboundTag, email)
}

// AddUser 把一段 inbound 用户片段加回运行中的实例。
func (c Client) AddUser(ctx context.Context, dir string, inboundPatch string) error {
	// patch 里带着 UUID / 密码，落在受管的 runtime 目录而不是共享的 /tmp；
	// 文件本身由 os.CreateTemp 以 0600 + 随机名创建，组也读不到。
	if err := fsperm.MkdirManaged(dir, fsperm.SharedDirMode); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".psctl-adu-*.json")
	if err != nil {
		return err
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := file.WriteString(inboundPatch); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return c.runCounted(ctx, "api", "adu", "--server="+c.Server, "--timeout="+c.timeoutSeconds(), path)
}

// checkArgumentValue 拒绝会被 xray 的 flag 解析吃掉的参数值。
//
// inbound tag 和 email 都来自 stack YAML，其中 email 是裸位置参数，以 `-` 开头
// 会被当成 flag，把 rmu 打到别的 inbound 上或者变成空操作。
func checkArgumentValue(label string, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", label)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s must not start with '-': %s", label, value)
	}
	return nil
}

func (c Client) timeoutSeconds() string {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

// runCounted 执行 api 子命令，并要求结尾统计行报告至少一个用户被处理。
func (c Client) runCounted(ctx context.Context, args ...string) error {
	output, err := c.run(ctx, args...)
	if err != nil {
		return err
	}
	return checkSuccessCount(args, output)
}

// checkSuccessCount 解析 adu/rmu 的结尾统计行。
func checkSuccessCount(args []string, output string) error {
	match := successPattern.FindStringSubmatch(output)
	if match == nil {
		return CallError{Args: args, Output: output, Err: fmt.Errorf("xray did not report a user count")}
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return CallError{Args: args, Output: output, Err: err}
	}
	if count < 1 {
		return CallError{Args: args, Output: output, Err: fmt.Errorf("xray processed 0 users")}
	}
	return nil
}

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// 比 xray 自己的 --timeout 多留一点，让它先返回自己的错误文本。
	ctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, c.Binary, args...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.String(), CallError{Args: args, Output: output.String(), Err: err}
	}
	return output.String(), nil
}

// APIEndpoint 解析某个 stack 的 Xray API 地址，并说明能否用于用户启停。
type APIEndpoint struct {
	Server         string
	Enabled        bool
	HandlerService bool
}

// Ready 判断该 stack 是否可以热改用户。
func (e APIEndpoint) Ready() bool {
	return e.Enabled && e.HandlerService && e.Server != ""
}

// Reason 返回不可用的原因，供命令输出提示。
func (e APIEndpoint) Reason() string {
	switch {
	case !e.Enabled:
		return "xray.api.enabled is false"
	case e.Server == "":
		return "xray.api.listen is empty"
	case !e.HandlerService:
		return "xray.api.services does not include HandlerService"
	default:
		return ""
	}
}

// ResolveAPIEndpoint 合并 defaults 和 stack 覆盖，得到该 stack 的 API 端点。
func ResolveAPIEndpoint(config domain.GlobalConfig, stack domain.Stack) APIEndpoint {
	apiConfig := domain.ResolveXrayAPIConfig(config.Defaults.Xray, stack.Xray)
	return APIEndpoint{
		Server:         apiConfig.Listen,
		Enabled:        apiConfig.Enabled,
		HandlerService: apiConfig.HasService("HandlerService"),
	}
}
