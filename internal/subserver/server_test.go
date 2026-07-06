package subserver_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
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

// TestServerStartEnablesImportAPIListener 验证 serve 会启动独立导入 listener 并可通过 HTTP 导入。
func TestServerStartEnablesImportAPIListener(t *testing.T) {
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
		ImportAPI: config.ImportAPIConfig{
			Enabled:        true,
			Listen:         freeListenAddress(t),
			MaxBundleBytes: 67108864,
		},
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
		return strings.Contains(output.String(), "Subscription import API loaded")
	}, time.Second, 20*time.Millisecond)

	request, err := http.NewRequest(http.MethodPost, "http://"+subConfig.ImportAPI.Listen+"/admin/import-bundle", bytes.NewReader(writeImportBundleData(t, "imported.yaml")))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.FileExists(t, filepath.Join(dataDir, "inputs", "imported.yaml"))

	cancel()
	err = <-done

	require.ErrorIs(t, err, context.Canceled)
}

// TestImportAPIDisabledDoesNotRegisterAdminRoute 验证默认不启动独立导入 listener，订阅 router 也不挂载 admin 路由。
func TestImportAPIDisabledDoesNotRegisterAdminRoute(t *testing.T) {
	dataDir := prepareDataDir(t)
	subConfig := testSubConfig(t, dataDir, config.AccessConfig{Type: "none"}, "", config.ManagedConfig{})
	server := subserver.NewServer(subConfig, func() string { return fixedGeneratedAt })

	require.Nil(t, server.ImportHTTP)
	response := performRequest(server.HTTP.Handler, http.MethodPost, "/admin/import-bundle")
	require.Equal(t, http.StatusNotFound, response.Code)
}

// TestImportAPIPostBundleWritesInputsAndReloads 验证成功 POST bundle 后写入 inputs、reload 并返回摘要。
func TestImportAPIPostBundleWritesInputsAndReloads(t *testing.T) {
	dataDir := prepareDataDir(t)
	server := testImportServer(t, dataDir, 67108864)
	require.NoError(t, server.State.Load())
	bundleData := writeImportBundleData(t, "imported.yaml")

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=false", bundleData, "127.0.0.1:1234")

	require.Equal(t, http.StatusOK, response.Code)
	var summary struct {
		Status      string   `json:"status"`
		Source      string   `json:"source"`
		GeneratedAt string   `json:"generated_at"`
		Inputs      int      `json:"inputs"`
		Written     []string `json:"written"`
		Replaced    []string `json:"replaced"`
		Removed     []string `json:"removed"`
		ReplaceAll  bool     `json:"replace_all"`
		Reloaded    bool     `json:"reloaded"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
	require.Equal(t, "ok", summary.Status)
	require.Equal(t, "imported", summary.Source)
	require.Equal(t, fixedGeneratedAt, summary.GeneratedAt)
	require.Equal(t, 1, summary.Inputs)
	require.Equal(t, []string{"imported.yaml"}, summary.Written)
	require.Empty(t, summary.Replaced)
	require.Empty(t, summary.Removed)
	require.False(t, summary.ReplaceAll)
	require.True(t, summary.Reloaded)
	require.FileExists(t, filepath.Join(dataDir, "inputs", "imported.yaml"))
	importEntries, err := os.ReadDir(filepath.Join(dataDir, ".imports"))
	require.NoError(t, err)
	require.Empty(t, importEntries)
	importDirInfo, err := os.Stat(filepath.Join(dataDir, ".imports"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), importDirInfo.Mode().Perm())
	index, _, lastError := server.State.Snapshot()
	require.Empty(t, lastError)
	require.NotNil(t, index)
	require.Len(t, index.Sources, 2)
}

// TestImportAPIReplaceAllRemovesOldInputs 验证 replace_all=true 会在校验成功后删除旧 input。
func TestImportAPIReplaceAllRemovesOldInputs(t *testing.T) {
	dataDir := prepareDataDir(t)
	server := testImportServer(t, dataDir, 67108864)
	require.NoError(t, server.State.Load())
	bundleData := writeImportBundleData(t, "imported.yaml")

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=true", bundleData, "127.0.0.1:1234")

	require.Equal(t, http.StatusOK, response.Code)
	var summary struct {
		Removed    []string `json:"removed"`
		ReplaceAll bool     `json:"replace_all"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
	require.True(t, summary.ReplaceAll)
	require.Equal(t, []string{"manual.yaml"}, summary.Removed)
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "manual.yaml"))
	require.FileExists(t, filepath.Join(dataDir, "inputs", "imported.yaml"))
	index, _, lastError := server.State.Snapshot()
	require.Empty(t, lastError)
	require.NotNil(t, index)
	require.Len(t, index.Sources, 1)
}

// TestImportAPIRejectsNonLoopbackRemoteAddr 验证导入接口不信任代理头，非 RemoteAddr 回环来源会被拒绝。
func TestImportAPIRejectsNonLoopbackRemoteAddr(t *testing.T) {
	dataDir := prepareDataDir(t)
	server := testImportServer(t, dataDir, 67108864)
	bundleData := writeImportBundleData(t, "imported.yaml")

	response := performRequestWithBodyAndHeaders(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=false", bundleData, "203.0.113.10:4321", map[string]string{
		"X-Forwarded-For": "127.0.0.1",
		"X-Real-IP":       "127.0.0.1",
	})

	require.Equal(t, http.StatusForbidden, response.Code)
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "imported.yaml"))
}

// TestImportAPIRejectsOversizedBundle 验证超过 max_bundle_bytes 时返回 413 且不写入 inputs。
func TestImportAPIRejectsOversizedBundle(t *testing.T) {
	dataDir := prepareDataDir(t)
	server := testImportServer(t, dataDir, 10)
	bundleData := writeImportBundleData(t, "imported.yaml")

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=false", bundleData, "127.0.0.1:1234")

	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "imported.yaml"))
}

// TestImportAPIRejectsExpandedBundleOverLimit 验证小 zip 解压后超过限制时不会写入 inputs。
func TestImportAPIRejectsExpandedBundleOverLimit(t *testing.T) {
	dataDir := prepareDataDir(t)
	bundleData := writeLargeImportBundleData(t, "large.yaml")
	server := testImportServer(t, dataDir, int64(len(bundleData)+512))

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=false", bundleData, "127.0.0.1:1234")

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "large.yaml"))
}

// TestImportAPIRejectsExpandedManifestOverLimit 验证小 zip 中的超大 manifest 也会被拒绝。
func TestImportAPIRejectsExpandedManifestOverLimit(t *testing.T) {
	dataDir := prepareDataDir(t)
	bundleData := writeLargeManifestBundleData(t)
	server := testImportServer(t, dataDir, int64(len(bundleData)+512))

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=false", bundleData, "127.0.0.1:1234")

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "large.yaml"))
}

// TestImportAPIRejectsInvalidBundleWithoutReplacingInputs 验证非法 bundle 不会覆盖旧 inputs。
func TestImportAPIRejectsInvalidBundleWithoutReplacingInputs(t *testing.T) {
	dataDir := prepareDataDir(t)
	server := testImportServer(t, dataDir, 67108864)

	response := performRequestWithBody(server.ImportHTTP.Handler, http.MethodPost, "/admin/import-bundle?replace_all=true", []byte("not a zip"), "127.0.0.1:1234")

	require.Equal(t, http.StatusBadRequest, response.Code)
	content, err := os.ReadFile(filepath.Join(dataDir, "inputs", "manual.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(content), "Manual Relay")
}

// TestServerStartImportListenFailureClosesMainListener 验证导入 listener 启动失败时会释放主订阅 listener。
func TestServerStartImportListenFailureClosesMainListener(t *testing.T) {
	dataDir := prepareDataDir(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	subListen := freeListenAddress(t)
	subConfig := config.SubServerConfig{
		DataDir:       dataDir,
		Listen:        subListen,
		Access:        config.AccessConfig{Type: "none"},
		WatchInterval: 1,
		WatchDebounce: 0,
		ImportAPI: config.ImportAPIConfig{
			Enabled:        true,
			Listen:         occupied.Addr().String(),
			MaxBundleBytes: 67108864,
		},
	}
	subConfig.ApplyDefaults()
	require.NoError(t, subConfig.Validate())
	server := subserver.NewServer(subConfig, func() string { return fixedGeneratedAt })

	err = server.Start(context.Background())

	require.Error(t, err)
	released, listenErr := net.Listen("tcp", subListen)
	require.NoError(t, listenErr)
	require.NoError(t, released.Close())
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
	var output bytes.Buffer
	restoreLogger := withTestLogger(&output)
	defer restoreLogger()
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
	logText := output.String()
	require.Contains(t, logText, `"message":"Subscription inputs reload failed"`)
	require.Contains(t, logText, `"input_dir":"`+filepath.Join(dataDir, "inputs")+`"`)
	require.Contains(t, logText, `"error":`)
}

// TestStateReloadWritesSummaryLog 验证运行期 reload 成功后输出 inputs 摘要日志。
func TestStateReloadWritesSummaryLog(t *testing.T) {
	var output bytes.Buffer
	restoreLogger := withTestLogger(&output)
	defer restoreLogger()
	dataDir := prepareDataDir(t)
	state := subserver.NewState(dataDir, subgen.Access{Type: "none"}, func() string { return fixedGeneratedAt })
	require.NoError(t, state.Load())

	require.NoError(t, state.Reload())

	logText := output.String()
	require.Contains(t, logText, `"message":"Subscription inputs reloaded"`)
	require.Contains(t, logText, `"input_dir":"`+filepath.Join(dataDir, "inputs")+`"`)
	require.Contains(t, logText, `"inputs":1`)
	require.Contains(t, logText, `"sources":1`)
	require.Contains(t, logText, `"nodes":1`)
	require.Contains(t, logText, `"users":1`)
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

func testSubConfig(t *testing.T, dataDir string, access config.AccessConfig, templateDir string, managedConfig config.ManagedConfig) config.SubServerConfig {
	t.Helper()
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
	return subConfig
}

func testImportServer(t *testing.T, dataDir string, maxBundleBytes int64) *subserver.Server {
	t.Helper()
	subConfig := testSubConfig(t, dataDir, config.AccessConfig{Type: "none"}, "", config.ManagedConfig{})
	subConfig.ImportAPI = config.ImportAPIConfig{
		Enabled:        true,
		Listen:         "127.0.0.1:3004",
		MaxBundleBytes: maxBundleBytes,
	}
	require.NoError(t, subConfig.Validate())
	server := subserver.NewServer(subConfig, func() string { return fixedGeneratedAt })
	require.NotNil(t, server.ImportHTTP)
	return server
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

func performRequestWithBody(handler http.Handler, method string, path string, body []byte, remoteAddr string) *httptest.ResponseRecorder {
	return performRequestWithBodyAndHeaders(handler, method, path, body, remoteAddr, nil)
}

func performRequestWithBodyAndHeaders(handler http.Handler, method string, path string, body []byte, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Host = "127.0.0.1"
	request.RemoteAddr = remoteAddr
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func writeImportBundleData(t *testing.T, inputName string) []byte {
	t.Helper()
	input := loadManualInput(t)
	input.Source = "imported"
	input.GeneratedAt = fixedGeneratedAt
	input.Nodes[0].ID = "imported:relay"
	input.Nodes[0].Port = 25001
	input.Nodes[0].Tag = "socks5:25001:relay"
	input.Nodes[0].Remark = "Imported Relay"
	bundlePath := filepath.Join(t.TempDir(), "sub-bundle.zip")
	_, err := subgen.WriteBundle(bundlePath, "imported", fixedGeneratedAt, []subgen.BundleInputFile{
		{Name: inputName, Content: []byte(subgen.InputToYAML(input))},
	})
	require.NoError(t, err)
	data, err := os.ReadFile(bundlePath)
	require.NoError(t, err)
	return data
}

func writeLargeImportBundleData(t *testing.T, inputName string) []byte {
	t.Helper()
	input := loadManualInput(t)
	input.Source = "large"
	input.GeneratedAt = fixedGeneratedAt
	input.Nodes[0].ID = "large:relay"
	input.Nodes[0].Port = 25002
	input.Nodes[0].Tag = "socks5:25002:relay"
	input.Nodes[0].Remark = "Large Relay"
	content := subgen.InputToYAML(input) + strings.Repeat("# padding padding padding padding padding padding padding\n", 4096)
	bundlePath := filepath.Join(t.TempDir(), "sub-bundle.zip")
	_, err := subgen.WriteBundle(bundlePath, "large", fixedGeneratedAt, []subgen.BundleInputFile{
		{Name: inputName, Content: []byte(content)},
	})
	require.NoError(t, err)
	data, err := os.ReadFile(bundlePath)
	require.NoError(t, err)
	return data
}

func writeLargeManifestBundleData(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	manifestWriter, err := writer.Create("manifest.json")
	require.NoError(t, err)
	_, err = manifestWriter.Write([]byte(strings.Repeat(" ", 2<<20)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
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
