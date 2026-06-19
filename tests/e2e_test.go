package tests

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentcli "github.com/eagle/proxystack-go/internal/cli/agent"
	subcli "github.com/eagle/proxystack-go/internal/cli/sub"
	"github.com/stretchr/testify/require"
)

// TestMigrationMainFlowE2E 验证 Go 版主流程可用 fake systemd 和本地 HTTP 订阅服务跑通。
func TestMigrationMainFlowE2E(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	runAgentCommand(t, baseDir, "init", "--external-host", "proxy.example.com")
	runAgentCommand(t, baseDir, "add", "usa1", "--no-edit")
	runAgentCommand(t, baseDir, "validate")
	checkOutput := runAgentCommand(t, baseDir, "check")
	require.Contains(t, checkOutput, "create generated/mihomo/usa1.yaml")
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "generated", "xray", "usa1.json"))
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "generated", "mihomo", "usa1.yaml"))

	prepareFakeBinaries(t, baseDir)
	systemdCalls := prepareFakeSystemd(t)
	runAgentCommand(t, baseDir, "start")
	require.FileExists(t, filepath.Join(baseDir, "runtime", "generated", "xray", "usa1.json"))
	require.FileExists(t, filepath.Join(baseDir, "runtime", "generated", "mihomo", "usa1.yaml"))
	require.FileExists(t, filepath.Join(baseDir, "runtime", "manifest.json"))
	systemdOutput := readFile(t, systemdCalls)
	require.Contains(t, systemdOutput, "systemctl start")
	require.Contains(t, systemdOutput, "proxystack-xray@usa1.service")
	require.Contains(t, systemdOutput, "proxystack-clash@usa1.service")

	runAgentCommand(t, baseDir, "sub", "export")
	bundlePath := filepath.Join(baseDir, "publish", "sub-bundle.zip")
	require.FileExists(t, bundlePath)
	subBaseDir := t.TempDir()
	runSubCommand(t, "--base-dir", subBaseDir, "import", bundlePath)
	require.FileExists(t, filepath.Join(subBaseDir, "inputs", "usa1.yaml"))

	port := reserveTCPPort(t)
	writeSubConfig(t, filepath.Join(subBaseDir, "config.yaml"), port)
	require.NoError(t, os.Rename(configPath, configPath+".moved"))
	require.NoError(t, os.Rename(filepath.Join(baseDir, "stacks"), filepath.Join(baseDir, "stacks.moved")))
	cancel, done := startSubServer(t, subBaseDir)
	defer stopSubServer(t, cancel, done)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		response, err := http.Get(baseURL + "/health")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 3*time.Second, 50*time.Millisecond)
	requireHTTPContains(t, baseURL+"/sub/user1", "proxies:")
	requireHTTPContains(t, baseURL+"/premium_sub/user1", "proxy-groups:")
	requireHTTPContains(t, baseURL+"/surge_sub/user1", "[Proxy]")
}

// runAgentCommand 执行 ps-agent 命令并返回合并输出。
func runAgentCommand(t *testing.T, baseDir string, args ...string) string {
	t.Helper()
	command := agentcli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(append([]string{"--base-dir", baseDir, "--service-manager", "systemd"}, args...))
	err := command.Execute()
	require.NoError(t, err, output.String())
	return output.String()
}

// runSubCommand 执行 ps-sub 命令并返回合并输出。
func runSubCommand(t *testing.T, args ...string) string {
	t.Helper()
	command := subcli.NewRootCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	require.NoError(t, err, output.String())
	return output.String()
}

// prepareFakeBinaries 写入 start 命令需要的 fake mihomo/xray 可执行文件。
func prepareFakeBinaries(t *testing.T, baseDir string) {
	t.Helper()
	binDir := filepath.Join(baseDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o750))
	writeExecutable(t, filepath.Join(binDir, "xray"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "mihomo"), "#!/bin/sh\nexit 0\n")
}

// prepareFakeSystemd 在 PATH 前置 fake systemctl/journalctl，并返回调用记录路径。
func prepareFakeSystemd(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	callsPath := filepath.Join(binDir, "calls.log")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$(basename \"$0\")\" \"$*\" >> \"" + callsPath + "\"\nexit 0\n"
	writeExecutable(t, filepath.Join(binDir, "systemctl"), script)
	writeExecutable(t, filepath.Join(binDir, "journalctl"), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsPath
}

// writeExecutable 写入测试用可执行脚本。
func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o750))
}

// reserveTCPPort 预留一个本机端口并立即释放给后续 HTTP server 使用。
func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// writeSubConfig 写入 E2E 使用的 sub 服务配置。
func writeSubConfig(t *testing.T, configPath string, port int) {
	t.Helper()
	require.NoError(t, os.WriteFile(configPath, []byte(fmt.Sprintf(`listen: 127.0.0.1:%d
access:
  type: none
watch_interval: 0.1
watch_debounce: 0
managed_config:
  enabled: true
  interval: 86400
  strict: true
`, port)), 0o640))
}

// startSubServer 启动 ps-sub serve 命令并返回取消函数和完成通道。
func startSubServer(t *testing.T, baseDir string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	command := subcli.NewRootCommand()
	command.SetContext(ctx)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--base-dir", baseDir, "serve"})
	go func() {
		err := command.Execute()
		if err == context.Canceled {
			err = nil
		}
		done <- err
	}()
	return cancel, done
}

// stopSubServer 停止 E2E 启动的 ps-sub serve 命令。
func stopSubServer(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("ps-sub serve did not stop")
	}
}

// requireHTTPContains 请求指定 URL 并断言响应正文包含目标文本。
func requireHTTPContains(t *testing.T, url string, want string) {
	t.Helper()
	response, err := http.Get(url)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	require.Contains(t, string(body), want)
}

// readFile 读取测试文件内容。
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
