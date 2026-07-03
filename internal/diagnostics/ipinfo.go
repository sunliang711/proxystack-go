package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
)

const (
	familyAll  = "all"
	familyIPv4 = "ipv4"
	familyIPv6 = "ipv6"

	noCurlCompatibleProxyListenerMessage = "no curl-compatible proxy listener found: need clash socks listener or xray socks5/http inbound"
)

var (
	defaultIPv4Sources = []string{
		"https://ipinfo.io/json",
		"https://myip.ipip.net",
	}
	defaultIPv6Sources = []string{
		"https://ifconfig.me/all.json",
		"https://ifconfig.co/json",
		"https://api64.ipify.org?format=json",
	}
	familyLabels = map[string]string{
		familyIPv4: "IPv4",
		familyIPv6: "IPv6",
	}
	ipCandidatePattern = regexp.MustCompile(`[0-9A-Fa-f:.]+`)
	curlLookPath       = exec.LookPath
)

// CurlResult 保存一次 curl 查询结果，便于单元测试替换外部命令。
type CurlResult struct {
	ReturnCode int
	Stdout     string
	Stderr     string
}

// SourceResult 保存单个 IP 信息来源的解析结果。
type SourceResult struct {
	URL    string
	Status string
	IP     string
	Region string
	Body   string
	Error  string
}

// FamilyResult 保存 IPv4 或 IPv6 一组来源的查询结果。
type FamilyResult struct {
	Family  string
	Label   string
	Sources []SourceResult
	IP      string
	Region  string
}

// IpInfoReport 保存一次 stack 出口 IP 查询报告。
type IpInfoReport struct {
	StackName string
	ProxyURL  string
	Families  []FamilyResult
}

// CurlRunner 抽象 curl 执行器，测试可注入 fake runner。
type CurlRunner func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error)

// LineCallback 接收渐进式输出的单行文本。
type LineCallback func(line string)

// IPInfoProgressState 表示单个 IP family 查询的进度状态。
type IPInfoProgressState string

const (
	IPInfoProgressDetecting IPInfoProgressState = "detecting"
	IPInfoProgressDone      IPInfoProgressState = "done"
)

// IPInfoProgress 保存单个 IP family 的进度事件。
type IPInfoProgress struct {
	State  IPInfoProgressState
	Family string
	Label  string
	Result FamilyResult
	Err    error
}

// ProgressCallback 接收单个 IP family 查询开始和完成事件。
type ProgressCallback func(progress IPInfoProgress)

// QueryOptions 保存 ipinfo 查询所需输入。
type QueryOptions struct {
	ConfigPath       string
	StackName        string
	Family           string
	Timeout          float64
	Sources          []string
	CurlRunner       CurlRunner
	LineCallback     LineCallback
	ProgressCallback ProgressCallback
}

// proxyEndpoint 保存真实代理 URL、展示 URL 和需要脱敏的敏感片段。
type proxyEndpoint struct {
	CurlURL    string
	DisplayURL string
	Secrets    []string
}

// sanitizedError 保留原始错误链，同时只向 Error() 暴露脱敏后的消息。
type sanitizedError struct {
	message string
	cause   error
}

// Error 返回脱敏后的错误消息，供 CLI 和进度输出展示。
func (e sanitizedError) Error() string {
	return e.message
}

// Unwrap 返回原始错误，保留 errors.Is/As 语义。
func (e sanitizedError) Unwrap() error {
	return e.cause
}

// QueryIPInfo 查询指定 stack 的出口 IP，默认同时检查 IPv4 和 IPv6。
func QueryIPInfo(ctx context.Context, options QueryOptions) (IpInfoReport, error) {
	family := strings.ToLower(options.Family)
	if family == "" {
		family = familyAll
	}
	if family != familyAll && family != familyIPv4 && family != familyIPv6 {
		return IpInfoReport{}, fmt.Errorf("family must be one of: all, ipv4, ipv6")
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 8.0
	}
	if timeout <= 0 {
		return IpInfoReport{}, fmt.Errorf("timeout must be greater than 0")
	}
	if options.StackName == "" {
		return IpInfoReport{}, fmt.Errorf("stack name is required")
	}
	runner := options.CurlRunner
	if runner == nil {
		if err := ensureCurlCommandAvailable(); err != nil {
			return IpInfoReport{}, err
		}
		runner = RunCurl
	}
	endpoint, err := resolveProxyEndpoint(options.ConfigPath, options.StackName)
	if err != nil {
		return IpInfoReport{}, err
	}
	sanitizeText := endpoint.sanitizeText
	runner = endpoint.sanitizedRunner(runner)
	families := []string{family}
	if family == familyAll {
		families = []string{familyIPv4, familyIPv6}
	}
	if options.LineCallback != nil {
		emitLines(options.LineCallback, formatIPInfoHeader(options.StackName, endpoint.DisplayURL))
	}
	results, err := queryRequestedFamilies(ctx, endpoint.CurlURL, families, timeout, options.Sources, runner, options.LineCallback, options.ProgressCallback, sanitizeText)
	if err != nil {
		return IpInfoReport{}, err
	}
	report := IpInfoReport{StackName: options.StackName, ProxyURL: endpoint.DisplayURL, Families: results}
	if options.LineCallback != nil {
		if len(families) > 1 {
			for _, result := range results {
				emitFamilyReportLines(options.LineCallback, result)
			}
		}
		emitLines(options.LineCallback, FormatIPInfoSummary(report))
	}
	return report, nil
}

// SourcesForFamily 返回指定 family 默认来源；显式传入来源时不做过滤。
func SourcesForFamily(family string, overrideSources []string) []string {
	if len(overrideSources) > 0 {
		return append([]string(nil), overrideSources...)
	}
	if family == familyIPv4 {
		return append([]string(nil), defaultIPv4Sources...)
	}
	return append([]string(nil), defaultIPv6Sources...)
}

// ResolveProxyURL 从 stack 可用代理入口生成 curl 可用的代理 URL。
func ResolveProxyURL(configPath string, stackName string) (string, error) {
	endpoint, err := resolveProxyEndpoint(configPath, stackName)
	if err != nil {
		return "", err
	}
	return endpoint.CurlURL, nil
}

// resolveProxyEndpoint 按 Clash socks、Xray socks5、Xray http 顺序选择 curl 兼容代理入口。
func resolveProxyEndpoint(configPath string, stackName string) (proxyEndpoint, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return proxyEndpoint{}, err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return proxyEndpoint{}, err
	}
	stack, ok := stackSet.ByName()[stackName]
	if !ok {
		return proxyEndpoint{}, fmt.Errorf("stack does not exist: %s", stackName)
	}
	if !stack.Enabled {
		return proxyEndpoint{}, fmt.Errorf("stack is disabled: %s", stackName)
	}
	endpoint, ok := selectProxyEndpoint(stack)
	if !ok {
		return proxyEndpoint{}, errors.New(noCurlCompatibleProxyListenerMessage)
	}
	return endpoint, nil
}

// selectProxyEndpoint 按配置优先级从 stack 中选择第一个可供 curl 使用的代理入口。
func selectProxyEndpoint(stack *domain.Stack) (proxyEndpoint, bool) {
	if stack.Clash.Enabled && len(stack.Clash.Listeners.Socks) > 0 {
		return clashSocksProxyEndpoint(stack.Clash.Listeners.Socks[0]), true
	}
	if !stack.Xray.Enabled {
		return proxyEndpoint{}, false
	}
	if endpoint, ok := firstXrayInboundProxyEndpoint(stack.Xray.Inbounds, "socks5"); ok {
		return endpoint, true
	}
	return firstXrayInboundProxyEndpoint(stack.Xray.Inbounds, "http")
}

// ListenerProxyURL 把 mihomo socks listener 转换为本机可连接的 socks5 URL。
func ListenerProxyURL(listener domain.SocksListener) string {
	return clashSocksProxyEndpoint(listener).CurlURL
}

// clashSocksProxyEndpoint 把 mihomo socks listener 转换为 curl 代理端点。
func clashSocksProxyEndpoint(listener domain.SocksListener) proxyEndpoint {
	username := ""
	password := ""
	if len(listener.Users) > 0 {
		username = listener.Users[0].Username
		password = listener.Users[0].Password
	}
	return newProxyEndpoint("socks5", listener.Listen, listener.Port, username, password)
}

// firstXrayInboundProxyEndpoint 返回指定协议的第一个 Xray inbound 代理端点。
func firstXrayInboundProxyEndpoint(inbounds []domain.Inbound, protocol string) (proxyEndpoint, bool) {
	for _, inbound := range inbounds {
		if inbound.Protocol != protocol {
			continue
		}
		username := ""
		password := ""
		if inbound.Auth != nil && inbound.Auth.Type == "password" {
			username = inbound.Auth.Username
			password = inbound.Auth.Password
		}
		return newProxyEndpoint(proxySchemeForInboundProtocol(protocol), inbound.Listen, inbound.Port, username, password), true
	}
	return proxyEndpoint{}, false
}

// proxySchemeForInboundProtocol 将 Xray inbound 协议映射为 curl proxy URL scheme。
func proxySchemeForInboundProtocol(protocol string) string {
	if protocol == "http" {
		return "http"
	}
	return "socks5"
}

// newProxyEndpoint 构造真实代理 URL 和对外展示用脱敏 URL。
func newProxyEndpoint(scheme string, listen string, port int, username string, password string) proxyEndpoint {
	curlURL := BuildProxyURL(scheme, listen, port, username, password)
	displayURL := curlURL
	secrets := make([]string, 0, 1)
	if password != "" {
		displayURL = BuildProxyURL(scheme, listen, port, username, "xxxxx")
		secrets = append(secrets, password)
	}
	return proxyEndpoint{CurlURL: curlURL, DisplayURL: displayURL, Secrets: secrets}
}

// BuildProxyURL 构造 curl 可用代理 URL，并对用户名密码做 URL encoding。
func BuildProxyURL(scheme string, listen string, port int, username string, password string) string {
	host := NormalizeConnectHost(listen)
	proxyURL := url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", FormatProxyHost(host), port),
	}
	if username != "" || password != "" {
		proxyURL.User = url.UserPassword(username, password)
	}
	return proxyURL.String()
}

// ensureCurlCommandAvailable 在默认 curl runner 启动前检查系统 curl 命令是否可用。
func ensureCurlCommandAvailable() error {
	if _, err := curlLookPath("curl"); err != nil {
		return fmt.Errorf("curl command not found: please install curl before running ipinfo: %w", err)
	}
	return nil
}

// sanitizedRunner 包装 curl 执行器，保留 stdout 原文用于解析，仅脱敏 stderr 和错误。
func (p proxyEndpoint) sanitizedRunner(runner CurlRunner) CurlRunner {
	return func(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
		result, err := runner(ctx, proxyURL, url, family, timeout)
		result.Stderr = p.sanitizeText(result.Stderr)
		return result, p.sanitizeError(err)
	}
}

// sanitizeError 对可能展示给 CLI 的错误进行密码脱敏，未变化时保留原错误链。
func (p proxyEndpoint) sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	message := p.sanitizeText(err.Error())
	if message == err.Error() {
		return err
	}
	return sanitizedError{message: message, cause: err}
}

// sanitizeText 替换真实代理 URL、明文密码和常见 URL 编码密码片段。
func (p proxyEndpoint) sanitizeText(text string) string {
	if text == "" {
		return ""
	}
	sanitized := text
	if p.CurlURL != "" && p.DisplayURL != "" {
		sanitized = strings.ReplaceAll(sanitized, p.CurlURL, p.DisplayURL)
	}
	for _, secret := range p.Secrets {
		for _, value := range secretRedactionVariants(secret) {
			sanitized = strings.ReplaceAll(sanitized, value, "xxxxx")
		}
	}
	return sanitized
}

// secretRedactionVariants 返回密码明文和常见 URL 编码形式，供输出脱敏使用。
func secretRedactionVariants(secret string) []string {
	if secret == "" {
		return nil
	}
	values := []string{secret, url.QueryEscape(secret), url.PathEscape(secret), proxyPasswordEscape(secret)}
	uniqueValues := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		uniqueValues = append(uniqueValues, value)
	}
	return uniqueValues
}

// proxyPasswordEscape 使用 net/url 的 userinfo 编码规则生成密码编码形式。
func proxyPasswordEscape(password string) string {
	proxyURL := url.URL{
		Scheme: "socks5",
		Host:   "127.0.0.1:1",
		User:   url.UserPassword("user", password),
	}
	encoded := proxyURL.String()
	encoded = strings.TrimPrefix(encoded, "socks5://user:")
	encoded = strings.TrimSuffix(encoded, "@127.0.0.1:1")
	return encoded
}

// NormalizeConnectHost 把监听地址转换为本机连接地址，wildcard listener 使用 127.0.0.1。
func NormalizeConnectHost(host string) string {
	normalizedHost := strings.Trim(strings.TrimSpace(host), "[]")
	switch normalizedHost {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return normalizedHost
	}
}

// FormatProxyHost 为 curl proxy URL 格式化 host，IPv6 地址需要方括号。
func FormatProxyHost(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// RunCurl 调用 curl 通过指定代理查询一个 IP 信息来源。
func RunCurl(ctx context.Context, proxyURL string, url string, family string, timeout float64) (CurlResult, error) {
	args := BuildCurlArgs(proxyURL, url, family, timeout)
	command := exec.CommandContext(ctx, "curl", args...)
	output, err := command.Output()
	stderr := ""
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = string(exitErr.Stderr)
		return CurlResult{ReturnCode: exitErr.ExitCode(), Stdout: string(output), Stderr: stderr}, nil
	}
	if err != nil {
		return CurlResult{}, fmt.Errorf("curl command not found or failed to start: %w", err)
	}
	return CurlResult{ReturnCode: 0, Stdout: string(output), Stderr: stderr}, nil
}

// BuildCurlArgs 构造 curl 参数数组，避免 shell 拼接和 IPv6 代理连接误用。
func BuildCurlArgs(proxyURL string, url string, family string, timeout float64) []string {
	args := []string{"-sS", "-L", "-m", FormatTimeout(timeout), "-x", proxyURL}
	if family == familyIPv4 {
		args = append(args, "-4")
	}
	return append(args, url)
}

// FormatTimeout 格式化 curl 超时参数，整数秒避免输出无意义的小数。
func FormatTimeout(timeout float64) string {
	if timeout == float64(int64(timeout)) {
		return strconv.FormatInt(int64(timeout), 10)
	}
	return strconv.FormatFloat(timeout, 'f', -1, 64)
}

// ParseSourceResponse 解析 JSON 或文本来源响应，并识别 IP family 是否匹配。
func ParseSourceResponse(body string, family string) (string, string, bool) {
	strippedBody := strings.TrimSpace(body)
	if strippedBody == "" {
		return "", "", false
	}
	var data any
	if err := json.Unmarshal([]byte(strippedBody), &data); err == nil {
		ipValue := extractIPFromJSON(data)
		regionValue := extractRegionFromJSON(data)
		if ipValue != "" && !ipMatchesFamily(ipValue, family) {
			return "", "", true
		}
		return ipValue, regionValue, false
	}
	ipValue := ""
	candidates := extractIPCandidates(strippedBody)
	if len(candidates) > 0 {
		ipValue = candidates[0]
	}
	if ipValue != "" && !ipMatchesFamily(ipValue, family) {
		return "", "", true
	}
	return ipValue, extractRegionFromText(strippedBody), false
}

type familyQueryResponse struct {
	Index  int
	Family string
	Result FamilyResult
	Err    error
}

// queryRequestedFamilies 查询请求涉及的 IP family，all 模式下并发查询 IPv4 和 IPv6。
func queryRequestedFamilies(ctx context.Context, proxyURL string, families []string, timeout float64, overrideSources []string, runner CurlRunner, lineCallback LineCallback, progressCallback ProgressCallback, sanitizeText func(string) string) ([]FamilyResult, error) {
	if len(families) == 1 {
		return querySingleFamily(ctx, proxyURL, families[0], timeout, overrideSources, runner, lineCallback, progressCallback, sanitizeText)
	}
	return queryFamiliesConcurrently(ctx, proxyURL, families, timeout, overrideSources, runner, progressCallback, sanitizeText)
}

// querySingleFamily 保持单 family 内部来源串行 fallback，并保留逐来源流式输出。
func querySingleFamily(ctx context.Context, proxyURL string, queryFamily string, timeout float64, overrideSources []string, runner CurlRunner, lineCallback LineCallback, progressCallback ProgressCallback, sanitizeText func(string) string) ([]FamilyResult, error) {
	emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDetecting, Family: queryFamily, Label: familyLabels[queryFamily]})
	if lineCallback != nil {
		lineCallback(familyLabels[queryFamily] + ":")
	}
	result, err := queryFamilySources(ctx, proxyURL, queryFamily, SourcesForFamily(queryFamily, overrideSources), timeout, runner, lineCallback, sanitizeText)
	if err != nil {
		emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDone, Family: queryFamily, Label: familyLabels[queryFamily], Result: emptyFamilyResult(queryFamily), Err: err})
		return nil, err
	}
	emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDone, Family: queryFamily, Label: familyLabels[queryFamily], Result: result})
	if lineCallback != nil {
		emitLines(lineCallback, formatFamilyFooter(result))
		lineCallback("")
	}
	return []FamilyResult{result}, nil
}

// queryFamiliesConcurrently 并发查询多个 IP family，并按输入顺序返回结果。
func queryFamiliesConcurrently(ctx context.Context, proxyURL string, families []string, timeout float64, overrideSources []string, runner CurlRunner, progressCallback ProgressCallback, sanitizeText func(string) string) ([]FamilyResult, error) {
	results := make([]FamilyResult, len(families))
	resultCh := make(chan familyQueryResponse, len(families))
	for index, queryFamily := range families {
		emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDetecting, Family: queryFamily, Label: familyLabels[queryFamily]})
		go func(index int, queryFamily string) {
			result, err := queryFamilySources(ctx, proxyURL, queryFamily, SourcesForFamily(queryFamily, overrideSources), timeout, runner, nil, sanitizeText)
			resultCh <- familyQueryResponse{Index: index, Family: queryFamily, Result: result, Err: err}
		}(index, queryFamily)
	}
	var firstErr error
	for range families {
		response := <-resultCh
		if response.Err != nil {
			if firstErr == nil {
				firstErr = response.Err
			}
			emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDone, Family: response.Family, Label: familyLabels[response.Family], Result: emptyFamilyResult(response.Family), Err: response.Err})
			continue
		}
		results[response.Index] = response.Result
		emitIPInfoProgress(progressCallback, IPInfoProgress{State: IPInfoProgressDone, Family: response.Family, Label: familyLabels[response.Family], Result: response.Result})
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// emitIPInfoProgress 调用进度回调，便于调用方集中控制终端输出。
func emitIPInfoProgress(progressCallback ProgressCallback, progress IPInfoProgress) {
	if progressCallback != nil {
		progressCallback(progress)
	}
}

// emptyFamilyResult 生成只包含 family 元信息的空结果，供错误状态输出。
func emptyFamilyResult(family string) FamilyResult {
	return FamilyResult{Family: family, Label: familyLabels[family]}
}

// queryFamilySources 按 IP family 逐个查询来源，拿到匹配 IP 后停止后续来源。
func queryFamilySources(ctx context.Context, proxyURL string, family string, sources []string, timeout float64, runner CurlRunner, lineCallback LineCallback, sanitizeText func(string) string) (FamilyResult, error) {
	bestIP := ""
	bestRegion := ""
	sourceResults := make([]SourceResult, 0, len(sources))
	if len(sources) == 0 {
		return FamilyResult{}, fmt.Errorf("at least one ipinfo source is required")
	}
	for _, url := range sources {
		result, err := runner(ctx, proxyURL, url, family, timeout)
		if err != nil {
			return FamilyResult{}, err
		}
		body := strings.TrimSpace(result.Stdout)
		displayBody := sanitizeText(body)
		if result.ReturnCode != 0 {
			sourceResult := SourceResult{
				URL:    url,
				Status: "failed",
				Body:   displayBody,
				Error:  sanitizeText(firstNonEmptyString(strings.TrimSpace(result.Stderr), fmt.Sprintf("curl exited with code %d", result.ReturnCode))),
			}
			recordSourceResult(&sourceResults, sourceResult, lineCallback)
			continue
		}
		ipValue, regionValue, wrongFamily := ParseSourceResponse(body, family)
		if ipValue != "" && bestIP == "" {
			bestIP = ipValue
		}
		if regionValue != "" && bestRegion == "" {
			bestRegion = regionValue
		}
		status := "raw"
		if wrongFamily {
			status = "wrong-family"
		} else if ipValue != "" || regionValue != "" {
			status = "ok"
		}
		sourceResult := SourceResult{URL: url, Status: status, IP: ipValue, Region: regionValue, Body: displayBody}
		recordSourceResult(&sourceResults, sourceResult, lineCallback)
		if ipValue != "" {
			break
		}
	}
	return FamilyResult{Family: family, Label: familyLabels[family], Sources: sourceResults, IP: bestIP, Region: bestRegion}, nil
}

// recordSourceResult 保存单个来源结果，并在流式模式下立即输出该来源的格式化行。
func recordSourceResult(sourceResults *[]SourceResult, sourceResult SourceResult, lineCallback LineCallback) {
	*sourceResults = append(*sourceResults, sourceResult)
	if lineCallback != nil {
		emitLines(lineCallback, formatSourceResult(sourceResult))
	}
}

// ipMatchesFamily 判断 IP 字符串是否属于指定 family。
func ipMatchesFamily(ipValue string, family string) bool {
	ipAddress, err := netip.ParseAddr(ipValue)
	if err != nil {
		return false
	}
	if family == familyIPv4 {
		return ipAddress.Is4()
	}
	if family == familyIPv6 {
		return ipAddress.Is6()
	}
	return false
}

// extractIPCandidates 从文本中提取可被 netip 识别的 IP 候选值。
func extractIPCandidates(text string) []string {
	candidates := make([]string, 0)
	seen := map[string]bool{}
	for _, token := range ipCandidatePattern.FindAllString(text, -1) {
		ipAddress, err := netip.ParseAddr(token)
		if err != nil {
			continue
		}
		ipValue := ipAddress.String()
		if seen[ipValue] {
			continue
		}
		seen[ipValue] = true
		candidates = append(candidates, ipValue)
	}
	return candidates
}

// extractIPFromJSON 从常见 JSON 字段或嵌套结构中提取 IP。
func extractIPFromJSON(data any) string {
	switch value := data.(type) {
	case map[string]any:
		for _, key := range []string{"ip", "ip_addr", "query", "address"} {
			if raw, ok := value[key].(string); ok {
				ipAddress, err := netip.ParseAddr(strings.TrimSpace(raw))
				if err == nil {
					return ipAddress.String()
				}
			}
		}
		for _, child := range value {
			if found := extractIPFromJSON(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := extractIPFromJSON(child); found != "" {
				return found
			}
		}
	}
	return ""
}

// extractRegionFromJSON 从常见 JSON 字段中提取城市、地区、国家和运营商信息。
func extractRegionFromJSON(data any) string {
	value, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, keys := range [][]string{
		{"city"},
		{"region", "region_name", "province", "state"},
		{"country", "country_name", "countryCode", "country_code", "country_iso"},
		{"org", "asn_org", "isp"},
	} {
		for _, key := range keys {
			raw, ok := value[key].(string)
			if !ok {
				continue
			}
			part := strings.TrimSpace(raw)
			if part != "" && !containsString(parts, part) {
				parts = append(parts, part)
			}
			break
		}
	}
	return strings.Join(parts, " / ")
}

// extractRegionFromText 从 myip.ipip.net 这类文本响应中提取地域信息。
func extractRegionFromText(text string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(text), "\n", 2)[0])
	if line == "" {
		return ""
	}
	const marker = "来自于："
	if index := strings.LastIndex(line, marker); index >= 0 {
		return strings.TrimSpace(line[index+len(marker):])
	}
	return ""
}

// emitLines 把格式化后的多行文本逐行交给调用方输出。
func emitLines(lineCallback LineCallback, lines []string) {
	for _, line := range lines {
		lineCallback(line)
	}
}

// formatIPInfoHeader 格式化 ipinfo 报告头部，供完整输出和流式输出复用。
func formatIPInfoHeader(stackName string, proxyURL string) []string {
	return []string{
		"Stack: " + stackName,
		"Proxy: " + redactProxyURL(proxyURL),
		"",
	}
}

// redactProxyURL 隐藏代理 URL 中的密码，防止手工构造报告时误输出明文。
func redactProxyURL(proxyURL string) string {
	parsedURL, err := url.Parse(proxyURL)
	if err != nil || parsedURL.User == nil {
		return proxyURL
	}
	username := parsedURL.User.Username()
	if _, ok := parsedURL.User.Password(); !ok {
		return proxyURL
	}
	parsedURL.User = url.UserPassword(username, "xxxxx")
	return parsedURL.String()
}

// formatSourceResult 格式化单个来源结果，流式模式下每个来源完成后立即输出。
func formatSourceResult(source SourceResult) []string {
	lines := []string{fmt.Sprintf("  - %s [%s]", source.URL, source.Status)}
	if source.IP != "" {
		lines = append(lines, "    IP: "+source.IP)
	}
	if source.Region != "" {
		lines = append(lines, "    Region: "+source.Region)
	}
	if source.Status == "failed" && source.Error != "" {
		lines = append(lines, "    Error: "+source.Error)
	}
	if source.Status == "raw" && source.Body != "" {
		lines = append(lines, "    Body: "+source.Body)
	}
	return lines
}

// formatFamilyFooter 格式化单个 IP family 的兜底解析结果提示。
func formatFamilyFooter(family FamilyResult) []string {
	lines := make([]string, 0, 2)
	if family.IP == "" {
		lines = append(lines, "  IP: 未解析到")
	}
	if family.Region == "" {
		lines = append(lines, "  Region: 未解析到")
	}
	return lines
}

// FormatIPInfoStatusLine 格式化单个 IP family 的一行状态摘要。
func FormatIPInfoStatusLine(family FamilyResult) string {
	return fmt.Sprintf("%s: %s Region: %s", family.Label, firstNonEmptyString(family.IP, "IP not resolved"), firstNonEmptyString(family.Region, "not resolved"))
}

// FormatIPInfoSummary 格式化 ipinfo 最终汇总。
func FormatIPInfoSummary(report IpInfoReport) []string {
	lines := []string{"Summary:"}
	for _, family := range report.Families {
		lines = append(lines, "  "+family.Label+":")
		lines = append(lines, "    IP: "+firstNonEmptyString(family.IP, "未解析到"))
		lines = append(lines, "    Region: "+firstNonEmptyString(family.Region, "未解析到"))
	}
	return lines
}

// emitFamilyReportLines 输出单个 IP family 的完整来源明细。
func emitFamilyReportLines(lineCallback LineCallback, family FamilyResult) {
	lineCallback(family.Label + ":")
	for _, source := range family.Sources {
		emitLines(lineCallback, formatSourceResult(source))
	}
	emitLines(lineCallback, formatFamilyFooter(family))
	lineCallback("")
}

// FormatIPInfoReport 把查询报告格式化为 CLI 友好的多行文本。
func FormatIPInfoReport(report IpInfoReport) []string {
	lines := formatIPInfoHeader(report.StackName, report.ProxyURL)
	for _, family := range report.Families {
		lines = append(lines, formatFamilyReportLines(family)...)
	}
	return append(lines, FormatIPInfoSummary(report)...)
}

// formatFamilyReportLines 格式化单个 IP family 的完整来源明细。
func formatFamilyReportLines(family FamilyResult) []string {
	lines := []string{family.Label + ":"}
	for _, source := range family.Sources {
		lines = append(lines, formatSourceResult(source)...)
	}
	lines = append(lines, formatFamilyFooter(family)...)
	return append(lines, "")
}

// firstNonEmptyString 返回第一段非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// containsString 判断字符串切片中是否包含指定值。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
