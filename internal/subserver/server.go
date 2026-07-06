package subserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Server 封装 pssub HTTP 服务和 watcher 生命周期。
type Server struct {
	Config     config.SubServerConfig
	State      *State
	HTTP       *http.Server
	ImportHTTP *http.Server
	Watch      *Watcher
	ImportMu   sync.Mutex
}

// NewRouter 创建订阅 HTTP router。
func NewRouter(state *State, subConfig config.SubServerConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(requestLogger())
	router.GET("/health", healthHandler(state))
	registerSubscriptionRoutes(router, state, subConfig, "/sub", "clash")
	registerSubscriptionRoutes(router, state, subConfig, "/premium_sub", "premium")
	registerSubscriptionRoutes(router, state, subConfig, "/surge_sub", "surge")
	return router
}

// newImportRouter 创建独立导入接口 router，只挂载 admin 导入路由。
func newImportRouter(state *State, subConfig config.SubServerConfig, importMu *sync.Mutex) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(requestLogger())
	router.POST("/admin/import-bundle", importBundleHandler(state, subConfig, importMu))
	return router
}

// NewServer 创建 HTTP server 和 watcher，但不启动监听。
func NewServer(subConfig config.SubServerConfig, now func() string) *Server {
	access := accessFromConfig(subConfig.Access)
	state := NewState(subConfig.DataDir, access, now)
	router := NewRouter(state, subConfig)
	server := &Server{
		Config: subConfig,
		State:  state,
		HTTP: &http.Server{
			Addr:              subConfig.Listen,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	if subConfig.ImportAPI.Enabled {
		server.ImportHTTP = &http.Server{
			Addr:              subConfig.ImportAPI.Listen,
			Handler:           newImportRouter(state, subConfig, &server.ImportMu),
			ReadHeaderTimeout: 5 * time.Second,
		}
	}
	return server
}

// Start 启动 HTTP server 和 watcher。
func (s *Server) Start(ctx context.Context) error {
	if err := s.State.Load(); err != nil {
		return err
	}
	s.Watch = NewWatcher(s.Config.DataDir, time.Duration(s.Config.WatchInterval*float64(time.Second)), time.Duration(s.Config.WatchDebounce*float64(time.Second)), s.State.Reload)
	if err := s.Watch.Start(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.Config.Listen)
	if err != nil {
		s.Watch.Stop()
		return err
	}
	var importListener net.Listener
	if s.ImportHTTP != nil {
		importListener, err = net.Listen("tcp", s.Config.ImportAPI.Listen)
		if err != nil {
			_ = listener.Close()
			s.Watch.Stop()
			return err
		}
	}
	s.logLoaded(listener.Addr().String())
	if importListener != nil {
		s.logImportLoaded(importListener.Addr().String())
	}
	errCh := make(chan error, 2)
	go func() {
		errCh <- s.HTTP.Serve(listener)
	}()
	if importListener != nil {
		go func() {
			errCh <- s.ImportHTTP.Serve(importListener)
		}()
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.shutdownHTTP(shutdownCtx)
		s.Watch.Stop()
		return ctx.Err()
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.shutdownHTTP(shutdownCtx)
		s.Watch.Stop()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// shutdownHTTP 优雅停止订阅和导入 HTTP server。
func (s *Server) shutdownHTTP(ctx context.Context) {
	_ = s.HTTP.Shutdown(ctx)
	if s.ImportHTTP != nil {
		_ = s.ImportHTTP.Shutdown(ctx)
	}
}

// importBundleHandler 导入 psctl sub export 生成的订阅 bundle，并在成功后立即 reload。
func importBundleHandler(state *State, subConfig config.SubServerConfig, importMu *sync.Mutex) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isLoopbackRemoteAddr(c.Request.RemoteAddr) {
			writeError(c, http.StatusForbidden, "forbidden", "import api requires loopback client")
			return
		}
		replaceAll, err := parseImportReplaceAll(c.Request.URL.Query())
		if err != nil {
			writeError(c, http.StatusBadRequest, "bad_request", "replace_all must be true or false")
			return
		}

		importMu.Lock()
		defer importMu.Unlock()

		bundlePath, err := writeImportTempBundle(c.Request, c.Writer, subConfig.DataDir, subConfig.ImportAPI.MaxBundleBytes)
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeError(c, http.StatusRequestEntityTooLarge, "payload_too_large", "subscription bundle is too large")
				return
			}
			log.Error().Err(err).Msg("Subscription import upload failed")
			writeError(c, http.StatusInternalServerError, "upload_failed", "subscription bundle upload failed")
			return
		}
		defer os.Remove(bundlePath)

		result, err := subgen.ExtractBundleInputsWithLimit(bundlePath, subConfig.DataDir, replaceAll, subConfig.ImportAPI.MaxBundleBytes)
		if err != nil {
			writeImportBundleError(c, err)
			return
		}
		if err := state.Reload(); err != nil {
			log.Error().Err(err).Msg("Subscription import reload failed")
			writeError(c, http.StatusInternalServerError, "reload_failed", "subscription inputs reload failed")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":       "ok",
			"source":       result.Manifest.Source,
			"generated_at": result.Manifest.GeneratedAt,
			"inputs":       len(result.WrittenInputs),
			"written":      result.WrittenInputs,
			"replaced":     result.ReplacedInputs,
			"removed":      result.RemovedInputs,
			"replace_all":  result.ReplaceAll,
			"reloaded":     true,
		})
	}
}

// parseImportReplaceAll 解析导入接口 replace_all 参数，缺省时按 false 处理。
func parseImportReplaceAll(values url.Values) (bool, error) {
	rawValues, ok := values["replace_all"]
	if !ok {
		return false, nil
	}
	if len(rawValues) != 1 {
		return false, errors.New("replace_all must be provided once")
	}
	switch rawValues[0] {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("replace_all must be true or false")
	}
}

// writeImportTempBundle 把上传 body 限流后写入 base dir 下的临时 zip 文件。
func writeImportTempBundle(request *http.Request, writer http.ResponseWriter, dataDir string, maxBundleBytes int64) (string, error) {
	importsDir := filepath.Join(dataDir, ".imports")
	if err := os.MkdirAll(importsDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(importsDir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(importsDir, "bundle-*.zip")
	if err != nil {
		return "", err
	}
	bundlePath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(bundlePath)
		return "", err
	}
	body := http.MaxBytesReader(writer, request.Body, maxBundleBytes)
	_, copyErr := io.Copy(file, body)
	closeBodyErr := body.Close()
	var syncErr error
	if copyErr == nil {
		syncErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(bundlePath)
		return "", copyErr
	}
	if closeBodyErr != nil {
		_ = os.Remove(bundlePath)
		return "", closeBodyErr
	}
	if syncErr != nil {
		_ = os.Remove(bundlePath)
		return "", syncErr
	}
	if closeErr != nil {
		_ = os.Remove(bundlePath)
		return "", closeErr
	}
	return bundlePath, nil
}

// writeImportBundleError 把 bundle 校验错误映射为安全的 HTTP 响应。
func writeImportBundleError(c *gin.Context, err error) {
	var generatorError subgen.GeneratorError
	if errors.As(err, &generatorError) {
		writeError(c, http.StatusBadRequest, "invalid_bundle", "subscription bundle is invalid")
		return
	}
	log.Error().Err(err).Msg("Subscription import failed")
	writeError(c, http.StatusInternalServerError, "import_failed", "subscription bundle import failed")
}

// isLoopbackRemoteAddr 只根据 RemoteAddr 判断调用方是否来自本机回环地址。
func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if zoneIndex := strings.LastIndex(host, "%"); zoneIndex >= 0 {
		host = host[:zoneIndex]
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// requestLogger 输出不含 token 和 query 的 HTTP 访问日志。
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		event := log.Info()
		if status >= http.StatusInternalServerError {
			event = log.Error()
		} else if status >= http.StatusBadRequest {
			event = log.Warn()
		}
		if user := subscriptionLogUser(c); user != "" {
			event = event.Str("user", user)
		}
		event.
			Str("method", c.Request.Method).
			Str("route", route).
			Int("status", status).
			Str("client_ip", c.ClientIP()).
			Dur("latency", time.Since(startedAt)).
			Int("bytes", c.Writer.Size()).
			Msg("HTTP request completed")
	}
}

// subscriptionLogUser 从订阅路由参数中提取 user，避免记录原始 path 中的 token。
func subscriptionLogUser(c *gin.Context) string {
	_, user, ok := parseSubscriptionPath(c.Param("rest"))
	if !ok {
		return ""
	}
	return user
}

// logLoaded 输出订阅服务启动摘要，便于 systemd/docker 日志确认加载状态。
func (s *Server) logLoaded(listen string) {
	index, users, _ := s.State.Snapshot()
	sources := 0
	nodes := 0
	if index != nil {
		sources = len(index.Sources)
		nodes = len(index.Nodes)
	}
	access := accessFromConfig(s.Config.Access)
	log.Info().
		Str("data_dir", s.Config.DataDir).
		Str("input_dir", filepath.Join(s.Config.DataDir, "inputs")).
		Str("listen", listen).
		Str("access", access.Type).
		Int("sources", sources).
		Int("nodes", nodes).
		Int("users", len(users)).
		Msg("Subscription server loaded")
}

// logImportLoaded 输出导入接口启动摘要，便于确认 admin listener 隔离监听。
func (s *Server) logImportLoaded(listen string) {
	log.Info().
		Str("data_dir", s.Config.DataDir).
		Str("listen", listen).
		Int64("max_bundle_bytes", s.Config.ImportAPI.MaxBundleBytes).
		Msg("Subscription import API loaded")
}

func healthHandler(state *State) gin.HandlerFunc {
	return func(c *gin.Context) {
		index, _, lastError := state.Snapshot()
		status := "ok"
		if lastError != "" {
			status = "error"
		}
		response := gin.H{
			"status": status,
			"index":  index != nil,
		}
		c.JSON(http.StatusOK, response)
	}
}

func registerSubscriptionRoutes(router *gin.Engine, state *State, subConfig config.SubServerConfig, prefix string, kind string) {
	handler := subscriptionHandler(state, subConfig, kind)
	router.GET(prefix+"/*rest", handler)
}

func subscriptionHandler(state *State, subConfig config.SubServerConfig, kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		pathToken, user, ok := parseSubscriptionPath(c.Param("rest"))
		if !ok {
			writeError(c, http.StatusNotFound, "not_found", "subscription not found")
			return
		}
		if !authorize(c, subConfig.Access, pathToken) {
			return
		}
		index, _, _ := state.Snapshot()
		if index == nil {
			writeError(c, http.StatusServiceUnavailable, "index_unavailable", "subscription index unavailable")
			return
		}
		if len(index.Users[user]) == 0 {
			writeError(c, http.StatusNotFound, "not_found", "subscription not found")
			return
		}
		var (
			body string
			err  error
		)
		switch kind {
		case "clash":
			body, err = subgen.RenderClashSubscription(*index, user, subConfig.TemplatesDir, subConfig.DataDir)
		case "premium":
			body, err = subgen.RenderPremiumClashSubscription(*index, user, subConfig.TemplatesDir, subConfig.DataDir)
		case "surge":
			managedURL := ""
			if subConfig.ManagedConfig.EnabledValue() {
				managedURL = managedConfigURL(c, subConfig, user)
			}
			body, err = subgen.RenderSurgeSubscription(*index, user, subConfig.TemplatesDir, subConfig.DataDir, managedURL, subConfig.ManagedConfig.Interval, subConfig.ManagedConfig.StrictValue())
		}
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, "template_error", "subscription template unavailable")
			return
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
	}
}

func authorize(c *gin.Context, access config.AccessConfig, pathToken string) bool {
	if access.Type == "" || access.Type == "none" {
		return true
	}
	token := pathToken
	if token == "" {
		writeError(c, http.StatusUnauthorized, "unauthorized", "subscription token required")
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(access.Token)) != 1 {
		writeError(c, http.StatusForbidden, "forbidden", "subscription token invalid")
		return false
	}
	return true
}

func parseSubscriptionPath(rest string) (string, string, bool) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return "", parts[0], true
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func managedConfigURL(c *gin.Context, subConfig config.SubServerConfig, user string) string {
	baseURL := strings.TrimRight(subConfig.ManagedConfig.PublicBaseURL, "/")
	if baseURL != "" {
		if subConfig.Access.Type == "token" {
			return baseURL + "/surge_sub/" + url.PathEscape(subConfig.Access.Token) + "/" + url.PathEscape(user)
		}
		return baseURL + "/surge_sub/" + url.PathEscape(user)
	}
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + c.Request.Host + c.Request.URL.RequestURI()
}

func writeError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}
