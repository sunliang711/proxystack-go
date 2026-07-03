package agent

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/stretchr/testify/require"
)

// TestRenderXraySkipsSystemPortProbeByDefault 验证 render 默认不因运行中端口占用而失败。
func TestRenderXraySkipsSystemPortProbeByDefault(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	listener, occupiedPort := listenOnLoopbackPort(t)
	defer listener.Close()
	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	data, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	updated := strings.Replace(string(data), "port: 24000", "port: "+strconv.Itoa(occupiedPort), 1)
	require.NotEqual(t, string(data), updated)
	require.NoError(t, os.WriteFile(stackPath, []byte(updated), 0o640))

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "render", "xray", "usa1")

	require.Contains(t, output, `"inbounds"`)
	require.Contains(t, output, fmt.Sprintf(`"port": %d`, occupiedPort))
}

// listenOnLoopbackPort 占用一个本机 TCP 端口，用于模拟运行中服务已绑定端口。
func listenOnLoopbackPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	return listener, port
}
