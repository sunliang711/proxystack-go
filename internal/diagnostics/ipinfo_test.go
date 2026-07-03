package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

// TestQueryIPInfoUsesDefaultSourcesByFamily 验证默认来源按 IPv4/IPv6 分组，并在成功解析 IP 后停止后续来源。
func TestQueryIPInfoUsesDefaultSourcesByFamily(t *testing.T) {
	configPath := writeIPInfoFixture(t, "127.0.0.1")
	calls := make([][]string, 0)
	fakeCurl := func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		calls = append(calls, []string{family, url})
		if family == "ipv4" {
			return CurlResult{ReturnCode: 0, Stdout: `{"ip": "198.51.100.10"}`}, nil
		}
		return CurlResult{ReturnCode: 0, Stdout: `{"ip": "2001:db8::10"}`}, nil
	}

	report, err := QueryIPInfo(context.Background(), QueryOptions{ConfigPath: configPath, StackName: "usa1", Family: "all", CurlRunner: fakeCurl})

	require.NoError(t, err)
	require.Equal(t, []string{"ipv4", "ipv6"}, []string{report.Families[0].Family, report.Families[1].Family})
	require.Equal(t, [][]string{
		{"ipv4", "https://ipinfo.io/json"},
		{"ipv6", "https://ifconfig.me/all.json"},
	}, calls)
	require.NotContains(t, SourcesForFamily("ipv4", nil), "https://ifconfig.me/all.json")
	require.NotContains(t, SourcesForFamily("ipv6", nil), "https://ipinfo.io/json")
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
	ipv6Listener := domain.SocksListener{Name: "local", Listen: "::1", Port: 17091}

	require.Equal(t, "socks5://127.0.0.1:17090", ListenerProxyURL(wildcardListener))
	require.Equal(t, "socks5://[::1]:17091", ListenerProxyURL(ipv6Listener))
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
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "usa1.yaml"), []byte(`name: usa1
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
`), 0o640))
	return configPath
}

// stringsJoin 用换行拼接字符串切片，避免测试引入额外格式逻辑。
func stringsJoin(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += "\n"
		}
		result += value
	}
	return result
}

// stringsHasSuffix 判断字符串后缀，保持测试辅助函数局部化。
func stringsHasSuffix(value string, suffix string) bool {
	if len(value) < len(suffix) {
		return false
	}
	return value[len(value)-len(suffix):] == suffix
}
