package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestQueryIPInfoUsesStackClashSocksListener 验证 ipinfo 使用 stack 的 mihomo socks listener 作为查询代理。
func TestQueryIPInfoUsesStackClashSocksListener(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	calls := make([][]any, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		calls = append(calls, []any{proxyURL, url, family, timeout})
		return CurlResult{
			ReturnCode: 0,
			Stdout:     `{"ip": "198.51.100.10", "city": "Tokyo", "country": "JP", "org": "AS64500"}`,
		}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Timeout:    3.0,
		Sources:    []string{"https://ipinfo.io/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:17091", report.ProxyURL)
	require.Equal(t, "198.51.100.10", report.Families[0].IP)
	require.Equal(t, "Tokyo / JP / AS64500", report.Families[0].Region)
	require.Equal(t, [][]any{{"socks5://127.0.0.1:17091", "https://ipinfo.io/json", "ipv4", 3.0}}, calls)
	require.Contains(t, stringsJoin(FormatIPInfoReport(report)), "IP: 198.51.100.10")
}

// TestQueryIPInfoUsesFirstClashSocksUserWithEncodedCredentials 验证 Clash socks listener 会使用第一个用户并编码认证信息。
func TestQueryIPInfoUsesFirstClashSocksUserWithEncodedCredentials(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: relay-user
        password: relay-password
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
        users:
          - username: first.user
            password: "p@ss word:1"
          - username: second.user
            password: second-password
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	calls := make([]string, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		calls = append(calls, proxyURL)
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"socks5://first.user:p%40ss%20word%3A1@127.0.0.1:17091"}, calls)
	require.Equal(t, "socks5://first.user:xxxxx@127.0.0.1:17091", report.ProxyURL)
	rendered := stringsJoin(FormatIPInfoReport(report))
	require.NotContains(t, rendered, "p@ss word:1")
	require.NotContains(t, rendered, "p%40ss%20word%3A1")
	require.Contains(t, rendered, "socks5://first.user:xxxxx@127.0.0.1:17091")
}

// TestQueryIPInfoDoesNotRedactSourceBodyBeforeParsing 验证短密码不会在解析前误伤正常 IP 响应。
func TestQueryIPInfoDoesNotRedactSourceBodyBeforeParsing(t *testing.T) {
	configPath := writeIPInfoFixtureWithClashSocksUser(t, "1")
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10", "city": "Austin", "country": "US", "org": "AS64501"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "198.51.100.10", report.Families[0].IP)
	require.Equal(t, "Austin / US / AS64501", report.Families[0].Region)
	require.Equal(t, "ok", report.Families[0].Sources[0].Status)
}

// TestQueryIPInfoRedactsProxyPasswordInRawSourceBody 验证 raw 响应体进入报告前仍会脱敏。
func TestQueryIPInfoRedactsProxyPasswordInRawSourceBody(t *testing.T) {
	configPath := writeIPInfoFixtureWithClashSocksUser(t, "raw-secret")
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		return CurlResult{ReturnCode: 0, Stdout: "proxy raw-secret response without ip"}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/raw"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	rendered := stringsJoin(FormatIPInfoReport(report))
	require.NotContains(t, rendered, "raw-secret")
	require.Contains(t, rendered, "Body: proxy xxxxx response without ip")
}

// TestQueryIPInfoFallsBackToXraySocksBeforeHTTP 验证没有 Clash socks 时优先选择 Xray socks5，即使 HTTP inbound 更早配置。
func TestQueryIPInfoFallsBackToXraySocksBeforeHTTP(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: web
      protocol: http
      listen: 127.0.0.1
      port: 25080
      auth:
        type: noauth
      sub: false
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: noauth
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    http: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	proxyURL := ""
	fakeCurl := func(ctx context.Context, proxy string, url string, family string, timeout float64) (CurlResult, error) {
		proxyURL = proxy
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	_, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:24001", proxyURL)
}

// TestQueryIPInfoUsesFirstXrayInboundWithinSameProtocol 验证同协议 Xray inbound 使用配置顺序中的第一个。
func TestQueryIPInfoUsesFirstXrayInboundWithinSameProtocol(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: first-relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: noauth
      sub: false
    - name: second-relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24002
      auth:
        type: noauth
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    http: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	proxyURL := ""
	fakeCurl := func(ctx context.Context, proxy string, url string, family string, timeout float64) (CurlResult, error) {
		proxyURL = proxy
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	_, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:24001", proxyURL)
}

// TestQueryIPInfoFallsBackToXrayHTTPWhenNoSocks 验证没有 Clash socks 和 Xray socks5 时选择 Xray HTTP inbound。
func TestQueryIPInfoFallsBackToXrayHTTPWhenNoSocks(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: web
      protocol: http
      listen: 127.0.0.1
      port: 25080
      auth:
        type: noauth
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    http: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	proxyURL := ""
	fakeCurl := func(ctx context.Context, proxy string, url string, family string, timeout float64) (CurlResult, error) {
		proxyURL = proxy
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	_, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:25080", proxyURL)
}

// TestQueryIPInfoUsesXrayPasswordAuthWithEncodedCredentials 验证 Xray password auth 会进入 curl URL 并做 URL encoding。
func TestQueryIPInfoUsesXrayPasswordAuthWithEncodedCredentials(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: "xray:user"
        password: "xray p@ss:1"
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    http: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	proxyURL := ""
	fakeCurl := func(ctx context.Context, proxy string, url string, family string, timeout float64) (CurlResult, error) {
		proxyURL = proxy
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv4",
		Sources:    []string{"https://ipinfo.example/json"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "socks5://xray%3Auser:xray%20p%40ss%3A1@127.0.0.1:24001", proxyURL)
	require.Equal(t, "socks5://xray%3Auser:xxxxx@127.0.0.1:24001", report.ProxyURL)
	require.NotContains(t, stringsJoin(FormatIPInfoReport(report)), "xray p@ss:1")
}

// TestResolveProxyURLErrorWhenNoCompatibleInbound 验证 vmess/shadowsocks 不会被当作 curl 兼容入口。
func TestResolveProxyURLErrorWhenNoCompatibleInbound(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: vmess
      protocol: vmess
      listen: 127.0.0.1
      port: 24001
      network: raw
      sub: true
      users:
        - user: alice
          uuid: 11111111-1111-4111-8111-111111111111
    - name: ss
      protocol: shadowsocks
      listen: 127.0.0.1
      port: 24002
      method: aes-256-gcm
      password: ss-server-password
      sub: true
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    http: []
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)

	_, err := ResolveProxyURL(configPath, "usa1")

	require.EqualError(t, err, noCurlCompatibleProxyListenerMessage)
}

// TestQueryIPInfoRedactsProxyPasswordInReportAndLineCallback 验证报告和逐行输出不会泄漏代理密码。
func TestQueryIPInfoRedactsProxyPasswordInReportAndLineCallback(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: relay-user
        password: relay-password
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
        users:
          - username: first.user
            password: "p@ss word:1"
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	lines := make([]string, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		if stringsHasSuffix(url, "/failed") {
			return CurlResult{
				ReturnCode: 28,
				Stderr:     "proxy failed via " + proxyURL + " password p@ss word:1 encoded p%40ss%20word%3A1",
			}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath:   configPath,
		StackName:    "usa1",
		Family:       "ipv4",
		Sources:      []string{"https://ipinfo.example/failed", "https://ipinfo.example/ok"},
		CurlRunner:   fakeCurl,
		LineCallback: func(line string) { lines = append(lines, line) },
	})

	require.NoError(t, err)
	visibleOutput := stringsJoin(append(FormatIPInfoReport(report), lines...))
	require.NotContains(t, visibleOutput, "p@ss word:1")
	require.NotContains(t, visibleOutput, "p%40ss%20word%3A1")
	require.Contains(t, visibleOutput, "socks5://first.user:xxxxx@127.0.0.1:17091")
	require.Contains(t, visibleOutput, "password xxxxx")
}

// TestQueryIPInfoRedactsProxyPasswordInProgressErrors 验证进度错误不会带出真实代理密码。
func TestQueryIPInfoRedactsProxyPasswordInProgressErrors(t *testing.T) {
	configPath := writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: relay-user
        password: relay-password
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
        users:
          - username: first.user
            password: "p@ss word:1"
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
	errBoom := errors.New("proxy dial failed")
	progresses := make([]IPInfoProgress, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		return CurlResult{}, fmt.Errorf("dial failed via %s with p@ss word:1: %w", proxyURL, errBoom)
	}

	_, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath:       configPath,
		StackName:        "usa1",
		Family:           "ipv4",
		Sources:          []string{"https://ipinfo.example/json"},
		CurlRunner:       fakeCurl,
		ProgressCallback: func(progress IPInfoProgress) { progresses = append(progresses, progress) },
	})

	require.Error(t, err)
	require.ErrorIs(t, err, errBoom)
	require.NotContains(t, err.Error(), "p@ss word:1")
	require.NotContains(t, err.Error(), "p%40ss%20word%3A1")
	require.Contains(t, err.Error(), "socks5://first.user:xxxxx@127.0.0.1:17091")
	require.Len(t, progresses, 2)
	require.Error(t, progresses[1].Err)
	require.ErrorIs(t, progresses[1].Err, errBoom)
	require.NotContains(t, progresses[1].Err.Error(), "p@ss word:1")
}

// TestFormatIPInfoReportRedactsProxyPassword 验证报告格式化会隐藏手工传入代理 URL 中的密码。
func TestFormatIPInfoReportRedactsProxyPassword(t *testing.T) {
	report := IpInfoReport{
		StackName: "usa1",
		ProxyURL:  "socks5://user:p%40ss%20word@127.0.0.1:17091",
		Families: []FamilyResult{{
			Family: familyIPv4,
			Label:  "IPv4",
			IP:     "198.51.100.10",
		}},
	}

	rendered := stringsJoin(FormatIPInfoReport(report))

	require.NotContains(t, rendered, "p%40ss%20word")
	require.Contains(t, rendered, "socks5://user:xxxxx@127.0.0.1:17091")
}

// TestQueryIPInfoUsesDefaultSourcesByFamily 验证默认来源按 IPv4/IPv6 分组，并在成功解析 IP 后停止后续来源。
func TestQueryIPInfoUsesDefaultSourcesByFamily(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	calls := make([][]string, 0)
	var callsMu sync.Mutex
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		callsMu.Lock()
		calls = append(calls, []string{family, url})
		callsMu.Unlock()
		if family == "ipv4" {
			return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "2001:db8::10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{ConfigPath: configPath, StackName: "usa1", Family: "all", CurlRunner: fakeCurl})

	require.NoError(t, err)
	require.Equal(t, []string{"ipv4", "ipv6"}, []string{report.Families[0].Family, report.Families[1].Family})
	callsMu.Lock()
	recordedCalls := append([][]string(nil), calls...)
	callsMu.Unlock()
	require.ElementsMatch(t, [][]string{
		{"ipv4", "https://ipinfo.io/json"},
		{"ipv6", "https://ifconfig.me/all.json"},
	}, recordedCalls)
	require.NotContains(t, SourcesForFamily("ipv4", nil), "https://ifconfig.me/all.json")
	require.NotContains(t, SourcesForFamily("ipv6", nil), "https://ipinfo.io/json")
}

// TestQueryIPInfoQueriesAllFamiliesConcurrently 验证 all 模式会并发启动 IPv4 和 IPv6 查询。
func TestQueryIPInfoQueriesAllFamiliesConcurrently(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		started <- family
		<-release
		if family == "ipv4" {
			return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "2001:db8::10"}`}, nil
	}
	type queryResult struct {
		report IpInfoReport
		err    error
	}
	done := make(chan queryResult, 1)

	go func() {
		report, err := QueryIPInfo(context.Background(), QueryOptions{
			ConfigPath: configPath,
			StackName:  "usa1",
			Family:     "all",
			Sources:    []string{"https://ipinfo.example/json"},
			CurlRunner: fakeCurl,
		})
		done <- queryResult{report: report, err: err}
	}()

	require.ElementsMatch(t, []string{"ipv4", "ipv6"}, []string{
		receiveStartedFamily(t, started),
		receiveStartedFamily(t, started),
	})
	released = true
	close(release)

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, []string{"ipv4", "ipv6"}, []string{result.report.Families[0].Family, result.report.Families[1].Family})
		require.Equal(t, "198.51.100.10", result.report.Families[0].IP)
		require.Equal(t, "2001:db8::10", result.report.Families[1].IP)
	case <-time.After(time.Second):
		t.Fatal("ipinfo query did not finish after releasing fake curl")
	}
}

// TestQueryIPInfoFamilyErrorDoesNotCancelPeer 验证一个 family 查询错误不会提前取消另一个并发 family。
func TestQueryIPInfoFamilyErrorDoesNotCancelPeer(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	errBoom := errors.New("curl unavailable")
	started := make(chan string, 2)
	releaseIPv4 := make(chan struct{})
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		started <- family
		if family == "ipv6" {
			return CurlResult{}, errBoom
		}
		select {
		case <-releaseIPv4:
			return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
		case <-ctx.Done():
			return CurlResult{}, ctx.Err()
		}
	}
	type queryResult struct {
		err error
	}
	progresses := make([]IPInfoProgress, 0)
	done := make(chan queryResult, 1)

	go func() {
		_, err := QueryIPInfo(context.Background(), QueryOptions{
			ConfigPath:       configPath,
			StackName:        "usa1",
			Family:           "all",
			Sources:          []string{"https://ipinfo.example/json"},
			CurlRunner:       fakeCurl,
			ProgressCallback: func(progress IPInfoProgress) { progresses = append(progresses, progress) },
		})
		done <- queryResult{err: err}
	}()

	require.ElementsMatch(t, []string{"ipv4", "ipv6"}, []string{
		receiveStartedFamily(t, started),
		receiveStartedFamily(t, started),
	})
	select {
	case result := <-done:
		require.Failf(t, "query finished before peer family completed", "err=%v", result.err)
	default:
	}
	close(releaseIPv4)

	select {
	case result := <-done:
		require.ErrorIs(t, result.err, errBoom)
	case <-time.After(time.Second):
		t.Fatal("ipinfo query did not finish after releasing ipv4")
	}
	require.Contains(t, ipInfoDoneProgressSummary(progresses), "ipv4:198.51.100.10")
	require.Contains(t, ipInfoDoneProgressSummary(progresses), "ipv6:err")
}

// TestQueryIPInfoTriesNextSourceUntilIPResolved 验证来源失败或 family 不匹配时继续查询，解析到匹配 IP 后停止。
func TestQueryIPInfoTriesNextSourceUntilIPResolved(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	calls := make([]string, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		calls = append(calls, url)
		if stringsHasSuffix(url, "/failed") {
			return CurlResult{ReturnCode: 28, Stderr: "timeout"}, nil
		}
		if stringsHasSuffix(url, "/wrong-family") {
			return CurlResult{ReturnCode: 0, Stdout: `{"ip": "203.0.113.8"}`}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "2001:db8::10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath: configPath,
		StackName:  "usa1",
		Family:     "ipv6",
		Sources:    []string{"https://ipinfo.example/failed", "https://ipinfo.example/wrong-family", "https://ipinfo.example/ok", "https://ipinfo.example/skipped"},
		CurlRunner: fakeCurl,
	})

	require.NoError(t, err)
	require.Equal(t, "2001:db8::10", report.Families[0].IP)
	require.Equal(t, []string{"https://ipinfo.example/failed", "https://ipinfo.example/wrong-family", "https://ipinfo.example/ok"}, calls)
}

// TestQueryIPInfoEmitsProgressLinesPerSource 验证 ipinfo 查询每完成一个来源就通过回调输出对应结果。
func TestQueryIPInfoEmitsProgressLinesPerSource(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	progressLines := make([]string, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		if stringsHasSuffix(url, "/failed") {
			return CurlResult{ReturnCode: 28, Stderr: "timeout"}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10", "city": "Tokyo", "country": "JP"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{
		ConfigPath:   configPath,
		StackName:    "usa1",
		Family:       "ipv4",
		Sources:      []string{"https://ipinfo.example/failed", "https://ipinfo.example/ok"},
		CurlRunner:   fakeCurl,
		LineCallback: func(line string) { progressLines = append(progressLines, line) },
	})

	require.NoError(t, err)
	require.Equal(t, "198.51.100.10", report.Families[0].IP)
	require.Equal(t, []string{
		"Stack: usa1",
		"Proxy: socks5://127.0.0.1:17091",
		"",
		"IPv4:",
		"  - https://ipinfo.example/failed [failed]",
		"    Error: timeout",
	}, progressLines[:6])
	require.Contains(t, progressLines, "  - https://ipinfo.example/ok [ok]")
	require.Equal(t, []string{
		"  IPv4:",
		"    IP: 198.51.100.10",
		"    Region: Tokyo / JP",
	}, progressLines[len(progressLines)-3:])
}

// TestListenerProxyURLNormalizesWildcardAndIPv6Hosts 验证 wildcard 监听地址会转成本机地址，IPv6 地址会补方括号。
func TestListenerProxyURLNormalizesWildcardAndIPv6Hosts(t *testing.T) {
	wildcardListener := domain.SocksListener{Name: "local", Listen: "0.0.0.0", Port: 17090}
	emptyListener := domain.SocksListener{Name: "local", Listen: "", Port: 17090}
	ipv6Listener := domain.SocksListener{Name: "local", Listen: "::1", Port: 17091}

	require.Equal(t, "socks5://127.0.0.1:17090", ListenerProxyURL(wildcardListener))
	require.Equal(t, "socks5://127.0.0.1:17090", ListenerProxyURL(emptyListener))
	require.Equal(t, "socks5://[::1]:17091", ListenerProxyURL(ipv6Listener))
	require.Equal(t, "http://user:p%40ss%20word@[::1]:18080", BuildProxyURL("http", "[::1]", 18080, "user", "p@ss word"))
}

// TestParseSourceResponseHandlesTextAndWrongFamily 验证 ipinfo 能解析文本响应，并识别 family 不匹配的响应。
func TestParseSourceResponseHandlesTextAndWrongFamily(t *testing.T) {
	ipValue, regionValue, wrongFamily := ParseSourceResponse("当前 IP：203.0.113.8 来自于：中国 北京 电信", "ipv4")
	wrongIP, wrongRegion, wrongFamilyIPv6 := ParseSourceResponse(`{"ip": "203.0.113.8", "city": "Beijing"}`, "ipv6")

	require.Equal(t, "203.0.113.8", ipValue)
	require.Equal(t, "中国 北京 电信", regionValue)
	require.False(t, wrongFamily)
	require.Empty(t, wrongIP)
	require.Empty(t, wrongRegion)
	require.True(t, wrongFamilyIPv6)
}

// TestBuildCurlArgsDoesNotForceIPv6ProxyConnection 验证 IPv6 查询不强制 curl 用 IPv6 连接本机代理。
func TestBuildCurlArgsDoesNotForceIPv6ProxyConnection(t *testing.T) {
	require.Equal(t, []string{
		"-sS",
		"-L",
		"-m",
		"2.5",
		"-x",
		"socks5://127.0.0.1:17091",
		"https://api64.ipify.org?format=json",
	}, BuildCurlArgs("socks5://127.0.0.1:17091", "https://api64.ipify.org?format=json", "ipv6", 2.5))
	require.Equal(t, []string{
		"-sS",
		"-L",
		"-m",
		"8",
		"-x",
		"socks5://127.0.0.1:17091",
		"-4",
		"https://ipinfo.io/json",
	}, BuildCurlArgs("socks5://127.0.0.1:17091", "https://ipinfo.io/json", "ipv4", 8.0))
}

// writeIPInfoFixture 写入最小可用 agent 配置和 stack。
func writeIPInfoFixture(t *testing.T, socksListen string) string {
	t.Helper()
	return writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: clash
    ref: usa1.clash.socks
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: usa1
        password: relay-password
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: `+socksListen+`
        port: 17091
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
}

// writeIPInfoFixtureWithClashSocksUser 写入带单个 Clash socks 认证用户的 stack 配置。
func writeIPInfoFixtureWithClashSocksUser(t *testing.T, password string) string {
	t.Helper()
	return writeIPInfoFixtureWithStack(t, `name: usa1
enabled: true
role: edge
xray:
  enabled: true
  outbound:
    type: direct
  inbounds:
    - name: relay
      protocol: socks5
      listen: 127.0.0.1
      port: 24001
      auth:
        type: password
        username: relay-user
        password: relay-password
      sub: false
clash:
  enabled: true
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
        users:
          - username: first.user
            password: "`+password+`"
  upstreams:
    - name: direct
      type: raw
      config:
        type: direct
        server: 127.0.0.1
        port: 1
  groups:
    - name: AllProxy
      type: select
      proxies: [direct, DIRECT]
  rules:
    profile: default
`)
}

// writeIPInfoFixtureWithStack 写入最小可用 agent 配置和指定 stack 内容。
func writeIPInfoFixtureWithStack(t *testing.T, stackYAML string) string {
	t.Helper()
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`version: 1
paths:
  bin: bin
  geo: geo
  stacks: stacks
  runtime: runtime
  generated: runtime/generated
  publish: publish
  downloads: downloads
  sub: sub
external_host: proxy.example.com
subscription:
  source: local
port_ranges:
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
security:
  require_auth_for_public_socks_http: true
  allow_noauth_public: false
install:
  mihomo:
    version: latest
    source: auto
  xray:
    version: latest
    source: auto
  geo:
    version: latest
    source: auto
`), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "usa1.yaml"), []byte(stackYAML), 0o640))
	return configPath
}

// stringsJoin 用换行拼接字符串切片，避免测试引入额外格式逻辑。
func stringsJoin(values []string) string {
	return strings.Join(values, "\n")
}

// stringsHasSuffix 判断字符串后缀，保持测试辅助函数局部化。
func stringsHasSuffix(value string, suffix string) bool {
	if len(value) < len(suffix) {
		return false
	}
	return value[len(value)-len(suffix):] == suffix
}

// receiveStartedFamily 等待 fake curl 启动一个 family 查询。
func receiveStartedFamily(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case family := <-started:
		return family
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for concurrent family query")
		return ""
	}
}

// ipInfoDoneProgressSummary 提取完成事件摘要，便于验证并发查询的最终状态。
func ipInfoDoneProgressSummary(progresses []IPInfoProgress) []string {
	values := make([]string, 0)
	for _, progress := range progresses {
		if progress.State != IPInfoProgressDone {
			continue
		}
		if progress.Err != nil {
			values = append(values, progress.Family+":err")
			continue
		}
		values = append(values, progress.Family+":"+progress.Result.IP)
	}
	return values
}
