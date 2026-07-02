package subserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Server 封装 pssub HTTP 服务和 watcher 生命周期。
type Server struct {
	Config config.SubServerConfig
	State  *State
	HTTP   *http.Server
	Watch  *Watcher
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

// NewServer 创建 HTTP server 和 watcher，但不启动监听。
func NewServer(subConfig config.SubServerConfig, now func() string) *Server {
	access := accessFromConfig(subConfig.Access)
	state := NewState(subConfig.DataDir, access, now)
	router := NewRouter(state, subConfig)
	return &Server{
		Config: subConfig,
		State:  state,
		HTTP: &http.Server{
			Addr:              subConfig.Listen,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
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
	s.logLoaded(listener.Addr().String())
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.HTTP.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.HTTP.Shutdown(shutdownCtx)
		s.Watch.Stop()
		return ctx.Err()
	case err := <-errCh:
		s.Watch.Stop()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
