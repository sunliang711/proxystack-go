package subserver_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/eagle/proxystack-go/internal/subserver"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

const fixedGeneratedAt = "2026-06-05T12:00:00+08:00"

// TestHTTPRoutesAndAuth 验证健康检查、token 鉴权和订阅响应。
func TestHTTPRoutesAndAuth(t *testing.T) {
	router, _ := testRouter(t, config.AccessConfig{Type: "token", Token: "demo-token"}, "")

	response := performRequest(router, http.MethodGet, "/health")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"index":true`)
	require.NotContains(t, response.Body.String(), `"users"`)
	require.NotContains(t, response.Body.String(), `"alice"`)
	require.NotContains(t, response.Body.String(), `"last_error"`)

	response = performRequest(router, http.MethodGet, "/sub/alice")
	require.Equal(t, http.StatusUnauthorized, response.Code)

	response = performRequest(router, http.MethodGet, "/sub/alice?token=demo-token")
	require.Equal(t, http.StatusUnauthorized, response.Code)

	response = performRequest(router, http.MethodGet, "/sub/bad-token/alice")
	require.Equal(t, http.StatusForbidden, response.Code)

	response = performRequest(router, http.MethodGet, "/sub/demo-token/alice")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "text/plain; charset=utf-8", response.Header().Get("Content-Type"))
	require.Contains(t, response.Body.String(), "Manual Relay")

	response = performRequest(router, http.MethodGet, "/sub/demo-token/bob")
	require.Equal(t, http.StatusNotFound, response.Code)
}

// TestHTTPAccessLogIncludesUserAndRedactsTokens 验证访问日志包含 user 但不打印 path/query 中的订阅 token。
func TestHTTPAccessLogIncludesUserAndRedactsTokens(t *testing.T) {
	var output bytes.Buffer
	restoreLogger := withTestLogger(&output)
	defer restoreLogger()
	router, _ := testRouter(t, config.AccessConfig{Type: "token", Token: "demo-token"}, "")

	response := performRequest(router, http.MethodGet, "/sub/demo-token/alice?token=demo-token")

	require.Equal(t, http.StatusOK, response.Code)
	logText := output.String()
	require.Contains(t, logText, `"message":"HTTP request completed"`)
	require.Contains(t, logText, `"route":"/sub/*rest"`)
	require.Contains(t, logText, `"user":"alice"`)
	require.Contains(t, logText, `"status":200`)
	require.NotContains(t, logText, "demo-token")
}

// TestServerStartWritesStartupLog 验证 serve 成功启动后输出加载摘要。
func TestServerStartWritesStartupLog(t *testing.T) {
	var output bytes.Buffer
	restoreLogger := withTestLogger(&output)
	defer restoreLogger()
	dataDir := prepareDataDir(t)
	subConfig := config.SubServerConfig{
		DataDir:       dataDir,
		Listen:        freeListenAddress(t),
		Access:        config.AccessConfig{Type: "none"},
		WatchInterval: 1,
		WatchDebounce: 0,
	}
	subConfig.ApplyDefaults()
	require.NoError(t, subConfig.Validate())
	server := subserver.NewServer(subConfig, func() string { return fixedGeneratedAt })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Start(ctx)
	}()
	require.Eventually(t, func() bool {
		return strings.Contains(output.String(), "Subscription server loaded")
	}, time.Second, 20*time.Millisecond)

	cancel()
	err := <-done

	require.ErrorIs(t, err, context.Canceled)
	logText := output.String()
	require.Contains(t, logText, `"data_dir":"`+dataDir+`"`)
	require.Contains(t, logText, `"input_dir":"`+filepath.Join(dataDir, "inputs")+`"`)
	require.Contains(t, logText, `"users":1`)
	require.Contains(t, logText, `"nodes":1`)
}

// TestHTTPHealthRedactsReloadError 验证健康检查不暴露用户列表和 reload 错误明文。
func TestHTTPHealthRedactsReloadError(t *testing.T) {
	dataDir := prepareDataDir(t)
	state := subserver.NewState(dataDir, subgen.Access{Type: "none"}, func() string { return fixedGeneratedAt })
	require.NoError(t, state.Load())
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "inputs", "manual.yaml"), []byte("bad: ["), 0o644))
	require.Error(t, state.Reload())
	subConfig := config.SubServerConfig{
		DataDir:       dataDir,
		Listen:        "127.0.0.1:3003",
		Access:        config.AccessConfig{Type: "none"},
		WatchInterval: 1,
		WatchDebounce: 0,
	}
	subConfig.ApplyDefaults()
	require.NoError(t, subConfig.Validate())
	router := subserver.NewRouter(state, subConfig)

	response := performRequest(router, http.MethodGet, "/health")

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"status":"error"`)
	require.Contains(t, response.Body.String(), `"index":true`)
	require.NotContains(t, response.Body.String(), `"users"`)
	require.NotContains(t, response.Body.String(), `"alice"`)
	require.NotContains(t, response.Body.String(), `"last_error"`)
	require.NotContains(t, response.Body.String(), "invalid YAML")
}

// TestHTTPTemplateErrorReturns503 验证坏模板映射为 template_error。
func TestHTTPTemplateErrorReturns503(t *testing.T) {
	templateDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(templateDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(templateDir, "sub", subgen.ClashTemplateName), []byte("{{ missing_value }}\n"), 0o644))
	router, _ := testRouter(t, config.AccessConfig{Type: "none"}, templateDir)

	response := performRequest(router, http.MethodGet, "/sub/alice")

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), "template_error")
}

// TestHTTPSurgeManagedConfigURL 验证 public_base_url + token 模式的 managed config 行。
func TestHTTPSurgeManagedConfigURL(t *testing.T) {
	router, _ := testRouterWithManagedConfig(t, config.AccessConfig{Type: "token", Token: "demo-token"}, "https://sub.example.com/base")

	response := performRequest(router, http.MethodGet, "/surge_sub/demo-token/alice")

	require.Equal(t, http.StatusOK, response.Code)
	firstLine := strings.SplitN(response.Body.String(), "\n", 2)[0]
	require.Equal(t, "#!MANAGED-CONFIG https://sub.example.com/base/surge_sub/demo-token/alice interval=86400 strict=true", firstLine)
}

// TestStateReloadFailureKeepsOldIndex 验证运行期 reload 失败不会替换旧 index。
func TestStateReloadFailureKeepsOldIndex(t *testing.T) {
	dataDir := prepareDataDir(t)
	state := subserver.NewState(dataDir, subgen.Access{Type: "none"}, func() string { return fixedGeneratedAt })
	require.NoError(t, state.Load())
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "inputs", "manual.yaml"), []byte("bad: ["), 0o644))

	err := state.Reload()

	require.Error(t, err)
	index, users, lastError := state.Snapshot()
	require.NotNil(t, index)
	require.Equal(t, []string{"alice"}, users)
	require.NotEmpty(t, lastError)
}

// TestWatcherCanStopAndReload 验证 watcher 可停止，且轮询 fallback 能触发 reload。
func TestWatcherCanStopAndReload(t *testing.T) {
	dataDir := prepareDataDir(t)
	var calls atomic.Int32
	watcher := subserver.NewWatcher(dataDir, 20*time.Millisecond, 0, func() error {
		calls.Add(1)
		return nil
	})
	require.NoError(t, watcher.Start())
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "inputs", "manual.yaml"), []byte(subgen.InputToYAML(loadManualInput(t))), 0o644))
	require.Eventually(t, func() bool { return calls.Load() > 0 }, time.Second, 20*time.Millisecond)
	watcher.Stop()
}

func testRouter(t *testing.T, access config.AccessConfig, templateDir string) (http.Handler, string) {
	t.Helper()
	return testRouterWithConfig(t, access, templateDir, config.ManagedConfig{})
}

func testRouterWithManagedConfig(t *testing.T, access config.AccessConfig, publicBaseURL string) (http.Handler, string) {
	t.Helper()
	enabled := true
	strict := true
	return testRouterWithConfig(t, access, "", config.ManagedConfig{
		Enabled:       &enabled,
		PublicBaseURL: publicBaseURL,
		Interval:      86400,
		Strict:        &strict,
	})
}

func testRouterWithConfig(t *testing.T, access config.AccessConfig, templateDir string, managedConfig config.ManagedConfig) (http.Handler, string) {
	t.Helper()
	dataDir := prepareDataDir(t)
	subConfig := config.SubServerConfig{
		DataDir:       dataDir,
		Listen:        "127.0.0.1:3003",
		Access:        access,
		TemplatesDir:  templateDir,
		WatchInterval: 1,
		WatchDebounce: 0,
		ManagedConfig: managedConfig,
	}
	subConfig.ApplyDefaults()
	require.NoError(t, subConfig.Validate())
	state := subserver.NewState(dataDir, subgen.Access{Type: access.Type, Token: access.Token}.Normalized(), func() string { return fixedGeneratedAt })
	require.NoError(t, state.Load())
	return subserver.NewRouter(state, subConfig), dataDir
}

func prepareDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	inputDir := filepath.Join(dataDir, "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o755))
	inputData, err := os.ReadFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "manual.yaml"), inputData, 0o644))
	return dataDir
}

func performRequest(handler http.Handler, method string, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func freeListenAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func withTestLogger(output *bytes.Buffer) func() {
	original := log.Logger
	log.Logger = zerolog.New(output)
	return func() {
		log.Logger = original
	}
}

func loadManualInput(t *testing.T) subgen.Input {
	t.Helper()
	input, err := subgen.LoadInputFile(testutil.RepoPath(t, "tests", "fixtures", "sub", "manual.yaml"))
	require.NoError(t, err)
	return input
}
