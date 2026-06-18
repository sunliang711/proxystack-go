package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
)

const (
	TargetMihomo = "mihomo"
	TargetXray   = "xray"
	TargetGeo    = "geo"
	TargetAll    = "all"
	TargetSelf   = "self"

	SourceAuto   = "auto"
	SourceGitHub = "github"
	SourceR2     = "r2"

	downloadChunkSize        = 64 * 1024
	downloadProgressInterval = 200 * time.Millisecond
	progressBarWidth         = 30
)

// Request 描述一次 install/update 操作。
type Request struct {
	Config        domain.GlobalConfig
	Target        string
	Version       string
	Source        string
	SHA256        string
	ArchiveMember string
	Force         bool
}

// Result 描述一次 install/update 写入或跳过的文件。
type Result struct {
	Target  string
	Written []string
	Skipped bool
	Source  string
}

// Progress 接收安装和下载过程中的阶段性进度消息。
type Progress func(message string)

// Downloader 抽象下载行为，测试可注入 fake downloader。
type Downloader interface {
	Download(ctx context.Context, source string) ([]byte, error)
}

// HTTPDownloader 支持本地文件、file://、http:// 和 https:// 下载。
type HTTPDownloader struct {
	Client   *http.Client
	Progress Progress
}

// Download 下载 source 内容，并对远端地址执行 SSRF 防护。
func (d HTTPDownloader) Download(ctx context.Context, source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "":
		info, err := os.Stat(source)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("source directory is not supported: %s", source)
		}
		return os.ReadFile(source)
	case "file":
		if parsed.Path == "" {
			return nil, fmt.Errorf("file source path is required")
		}
		info, err := os.Stat(parsed.Path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("source directory is not supported: %s", parsed.Path)
		}
		return os.ReadFile(parsed.Path)
	case "http", "https":
		if err := ValidateRemoteURL(source); err != nil {
			return nil, err
		}
		client := d.Client
		if client == nil {
			client = secureHTTPClient()
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("download failed: GET %s returned %s", response.Request.URL.String(), response.Status)
		}
		return readDownloadBody(response, downloadName(parsed), d.Progress)
	default:
		return nil, fmt.Errorf("unsupported source scheme: %s", parsed.Scheme)
	}
}

func secureHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: secureDialContextWithProxy,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return ValidateRemoteURL(req.URL.String())
		},
	}
}

// secureDialContextWithProxy 允许连接显式配置的代理地址，其余目标仍执行 SSRF 拨号防护。
func secureDialContextWithProxy(ctx context.Context, network string, address string) (net.Conn, error) {
	if isEnvironmentProxyAddress(address) {
		dialer := net.Dialer{}
		return dialer.DialContext(ctx, network, address)
	}
	return secureDialContext(ctx, network, address)
}

// isEnvironmentProxyAddress 判断本次拨号地址是否来自标准代理环境变量。
func isEnvironmentProxyAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	for _, rawProxy := range environmentProxyValues() {
		proxyHost, proxyPort, ok := proxyHostPort(rawProxy)
		if !ok {
			continue
		}
		if strings.EqualFold(host, proxyHost) && port == proxyPort {
			return true
		}
	}
	return false
}

// environmentProxyValues 返回 Go 标准代理环境变量的当前值。
func environmentProxyValues() []string {
	names := []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"}
	values := make([]string, 0, len(names))
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

// proxyHostPort 解析代理 URL，并补齐缺省端口。
func proxyHostPort(rawProxy string) (string, string, bool) {
	parsed, err := url.Parse(rawProxy)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", "", false
		}
	}
	return host, port, host != ""
}

func secureDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	dialer := net.Dialer{}
	for _, ipAddr := range ips {
		if isBlockedIP(ipAddr.IP) {
			return nil, fmt.Errorf("remote URL resolves to blocked address: %s", ipAddr.IP.String())
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("remote host has no addresses: %s", host)
}

func readDownloadBody(response *http.Response, filename string, progress Progress) ([]byte, error) {
	var buffer bytes.Buffer
	totalSize := response.ContentLength
	if totalSize < 0 {
		totalSize = 0
	}
	downloadedSize := int64(0)
	start := time.Now()
	nextProgress := start
	emitProgress(progress, formatDownloadProgress("download: start", filename, downloadedSize, totalSize, start))
	chunk := make([]byte, downloadChunkSize)
	for {
		count, readErr := response.Body.Read(chunk)
		if count > 0 {
			buffer.Write(chunk[:count])
			downloadedSize += int64(count)
			now := time.Now()
			if !now.Before(nextProgress) || (totalSize > 0 && downloadedSize >= totalSize) {
				emitProgress(progress, formatDownloadProgress("download: progress", filename, downloadedSize, totalSize, start))
				nextProgress = now.Add(downloadProgressInterval)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	emitProgress(progress, formatDownloadProgress("download: complete", filename, downloadedSize, totalSize, start))
	return buffer.Bytes(), nil
}

func formatDownloadProgress(prefix string, filename string, downloadedSize int64, totalSize int64, start time.Time) string {
	speed := downloadSpeed(downloadedSize, start)
	if totalSize <= 0 {
		return fmt.Sprintf("%s %s %s %s/s", prefix, filename, formatByteCount(float64(downloadedSize)), formatByteCount(speed))
	}
	percent := float64(downloadedSize) * 100 / float64(totalSize)
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%s %s [%s] %5.1f%% %s/%s %s/s", prefix, filename, formatProgressBar(downloadedSize, totalSize), percent, formatByteCount(float64(downloadedSize)), formatByteCount(float64(totalSize)), formatByteCount(speed))
}

func formatProgressBar(downloadedSize int64, totalSize int64) string {
	if totalSize <= 0 {
		return strings.Repeat("-", progressBarWidth)
	}
	filledWidth := int(int64(progressBarWidth) * downloadedSize / totalSize)
	if filledWidth > progressBarWidth {
		filledWidth = progressBarWidth
	}
	return strings.Repeat("#", filledWidth) + strings.Repeat("-", progressBarWidth-filledWidth)
}

func downloadSpeed(downloadedSize int64, start time.Time) float64 {
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(downloadedSize) / elapsed
}

func formatByteCount(size float64) string {
	value := size
	for _, unit := range []string{"B", "KiB", "MiB", "GiB"} {
		if value < 1024 || unit == "GiB" {
			if unit == "B" {
				return fmt.Sprintf("%d %s", int64(value), unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.1f GiB", value)
}

// Installer 执行 install/update 的下载、校验、解包和原子替换。
type Installer struct {
	Downloader Downloader
	Progress   Progress
}

// InstallTarget 安装或更新一个目标，Force=false 时跳过已存在目标。
func (i Installer) InstallTarget(ctx context.Context, request Request) ([]Result, error) {
	targets, err := ExpandTargets(request.Target, request.Force)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(targets))
	for _, target := range targets {
		nextRequest := request
		nextRequest.Target = target
		result, err := i.installOne(ctx, nextRequest)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ExpandTargets 展开 all 目标；self 只允许 update 路径使用。
func ExpandTargets(target string, allowSelf bool) ([]string, error) {
	switch target {
	case TargetMihomo, TargetXray, TargetGeo:
		return []string{target}, nil
	case TargetAll:
		return []string{TargetMihomo, TargetXray, TargetGeo}, nil
	case TargetSelf:
		if allowSelf {
			return []string{TargetSelf}, nil
		}
		return nil, fmt.Errorf("self is only supported by update")
	default:
		return nil, fmt.Errorf("unsupported install target: %s", target)
	}
}

// ManagedSources 为托管源生成候选下载 URL。
func ManagedSources(target string, version string, source string) ([]string, error) {
	if version == "" {
		version = "latest"
	}
	switch source {
	case "", SourceAuto:
		return ManagedSources(target, version, SourceGitHub)
	case SourceGitHub:
		return nil, fmt.Errorf("github source requires release metadata resolution")
	case SourceR2:
		return nil, fmt.Errorf("r2 managed source is not configured; use a concrete http(s) URL in --source")
	default:
		return []string{source}, nil
	}
}

// ValidateRemoteURL 拒绝本机、私网、link-local 和 metadata 下载目标。
func ValidateRemoteURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("remote URL must use http or https")
	}
	if parsed.User != nil {
		return fmt.Errorf("remote URL must not include userinfo")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("remote URL host is required")
	}
	if strings.EqualFold(host, "metadata.google.internal") {
		return fmt.Errorf("remote URL points to metadata host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("remote URL resolves to blocked address: %s", ip.String())
		}
	}
	return nil
}

func (i Installer) installOne(ctx context.Context, request Request) (Result, error) {
	operation := "install"
	if request.Force {
		operation = "update"
	}
	emitProgress(i.Progress, fmt.Sprintf("%s: prepare %s", operation, request.Target))
	paths := targetPaths(request.Config, request.Target)
	if !request.Force && allExist(paths) {
		emitProgress(i.Progress, fmt.Sprintf("%s: skip %s already installed", operation, request.Target))
		return Result{Target: request.Target, Skipped: true}, nil
	}
	downloader := i.downloader()
	targetConfig := requestConfig(request.Config, request.Target)
	version := firstNonEmpty(request.Version, targetConfig.Version)
	sourceMode := firstNonEmpty(request.Source, targetConfig.Source)
	sha256 := firstNonEmpty(request.SHA256, targetConfig.SHA256)
	archiveMember := firstNonEmpty(request.ArchiveMember, targetConfig.ArchiveMember)
	emitProgress(i.Progress, fmt.Sprintf("download: resolve %s %s via %s", request.Target, firstNonEmpty(version, "latest"), firstNonEmpty(sourceMode, SourceAuto)))
	sources, err := resolveSources(ctx, downloader, request.Target, version, sourceMode)
	if err != nil {
		return Result{}, err
	}
	managedSource := isManagedSource(sourceMode)
	var lastErr error
	for _, source := range sources {
		if !managedSource && isPlainRemoteSource(source) && sha256 == "" {
			return Result{}, fmt.Errorf("sha256 is required for remote source: %s", source)
		}
		emitProgress(i.Progress, fmt.Sprintf("download: try %s %s", sourceLabel(sourceMode), downloadNameForSource(source)))
		data, err := downloader.Download(ctx, source)
		if err != nil {
			lastErr = fmt.Errorf("%s download from %s failed: %w", request.Target, source, err)
			emitProgress(i.Progress, fmt.Sprintf("download: failed %s: %s", sourceLabel(sourceMode), err))
			continue
		}
		emitProgress(i.Progress, fmt.Sprintf("download: selected %s", sourceLabel(sourceMode)))
		if sha256 != "" {
			emitProgress(i.Progress, fmt.Sprintf("%s: verify %s", operation, downloadNameForSource(source)))
			if err := verifySHA256(data, sha256); err != nil {
				return Result{}, err
			}
		} else {
			emitProgress(i.Progress, fmt.Sprintf("%s: verify %s", operation, downloadNameForSource(source)))
		}
		emitProgress(i.Progress, fmt.Sprintf("%s: install %s", operation, request.Target))
		files, err := ExtractFiles(source, data, request.Target, archiveMember)
		if err != nil {
			return Result{}, err
		}
		written, err := writeTargetFiles(paths, files)
		if err != nil {
			return Result{}, err
		}
		emitProgress(i.Progress, fmt.Sprintf("%s: complete %s", operation, request.Target))
		return Result{Target: request.Target, Written: written, Source: source}, nil
	}
	if lastErr != nil {
		return Result{}, lastErr
	}
	return Result{}, fmt.Errorf("no source candidates for target: %s", request.Target)
}

func (i Installer) downloader() Downloader {
	if i.Downloader != nil {
		return i.Downloader
	}
	return HTTPDownloader{Progress: i.Progress}
}

func resolveSources(ctx context.Context, downloader Downloader, target string, version string, source string) ([]string, error) {
	if version == "" {
		version = "latest"
	}
	switch source {
	case "", SourceAuto:
		return resolveSources(ctx, downloader, target, version, SourceGitHub)
	case SourceGitHub:
		if version == "latest" {
			return resolveGitHubLatestSources(ctx, downloader, target)
		}
		return resolveGitHubVersionSources(ctx, downloader, target, version)
	case SourceR2:
		return nil, fmt.Errorf("r2 managed source is not configured; use a concrete http(s) URL in --source")
	default:
		return []string{source}, nil
	}
}

type githubReleaseResponse struct {
	TagName string              `json:"tag_name"`
	Assets  []githubAssetResult `json:"assets"`
}

type githubAssetResult struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func resolveGitHubLatestSources(ctx context.Context, downloader Downloader, target string) ([]string, error) {
	data, err := downloader.Download(ctx, githubLatestAPIURL(target))
	if err != nil {
		return nil, fmt.Errorf("github latest metadata for %s could not be downloaded: %w", target, err)
	}
	var release githubReleaseResponse
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("github latest metadata for %s could not be parsed: %w", target, err)
	}
	source, err := selectGitHubReleaseAsset(target, release)
	if err != nil {
		return nil, fmt.Errorf("github latest release for %s has no compatible asset: %w", target, err)
	}
	return []string{source}, nil
}

// resolveGitHubVersionSources 从 GitHub release tag metadata 中选择指定版本的真实资产 URL。
func resolveGitHubVersionSources(ctx context.Context, downloader Downloader, target string, version string) ([]string, error) {
	failures := []string{}
	for _, tag := range githubReleaseTagCandidates(target, version) {
		release, err := resolveGitHubReleaseByTag(ctx, downloader, target, tag)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", tag, err))
			continue
		}
		source, err := selectGitHubReleaseAsset(target, release)
		if err != nil {
			return nil, fmt.Errorf("github release for %s version %s has no compatible asset: %w", target, version, err)
		}
		return []string{source}, nil
	}
	return nil, fmt.Errorf("github release for %s version %s was not found in metadata: %s", target, version, strings.Join(failures, "; "))
}

// resolveGitHubReleaseByTag 读取单个 tag 的 release metadata，避免固定版本受 releases 列表分页影响。
func resolveGitHubReleaseByTag(ctx context.Context, downloader Downloader, target string, tag string) (githubReleaseResponse, error) {
	metadataURL := githubReleaseTagAPIURL(target, tag)
	if metadataURL == "" {
		return githubReleaseResponse{}, fmt.Errorf("github release metadata URL is not mapped for %s", target)
	}
	data, err := downloader.Download(ctx, metadataURL)
	if err != nil {
		return githubReleaseResponse{}, fmt.Errorf("metadata could not be downloaded: %w", err)
	}
	var release githubReleaseResponse
	if err := json.Unmarshal(data, &release); err != nil {
		return githubReleaseResponse{}, fmt.Errorf("metadata could not be parsed: %w", err)
	}
	return release, nil
}

// githubReleaseTagCandidates 返回固定版本 metadata 查询的候选 tag。
func githubReleaseTagCandidates(target string, version string) []string {
	rawVersion := strings.TrimSpace(version)
	trimmedVersion := trimVersionPrefix(rawVersion)
	candidates := []string{}
	addCandidate := func(candidate string) {
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	switch target {
	case TargetMihomo, TargetXray:
		if rawVersion == trimmedVersion {
			addCandidate("v" + trimmedVersion)
			addCandidate(trimmedVersion)
		} else {
			addCandidate(rawVersion)
			addCandidate("v" + trimmedVersion)
			addCandidate(trimmedVersion)
		}
	default:
		addCandidate(rawVersion)
		if rawVersion != trimmedVersion {
			addCandidate(trimmedVersion)
		}
	}
	return candidates
}

// trimVersionPrefix 去掉语义化版本前缀 v/V，用于 metadata 匹配。
func trimVersionPrefix(version string) string {
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}

// ExtractFiles 从普通文件或归档中提取目标文件内容。
func ExtractFiles(source string, data []byte, target string, member string) (map[string][]byte, error) {
	lower := strings.ToLower(source)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGzip(data, target, member)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(data, target, member)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(data, target, member)
	case strings.HasSuffix(lower, ".gz"):
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		return singleTargetFile(target, filepath.Base(strings.TrimSuffix(source, ".gz")), content)
	default:
		return singleTargetFile(target, filepath.Base(source), data)
	}
}

func extractZip(data []byte, target string, member string) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("archive links are not supported: %s", file.Name)
		}
		if err := validateArchivePath(file.Name); err != nil {
			return nil, err
		}
		if member != "" && file.Name != member && filepath.Base(file.Name) != member {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			return nil, err
		}
		files[file.Name] = content
	}
	return selectExtractedFiles(files, target, member)
}

func extractTarGzip(data []byte, target string, member string) (map[string][]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return readTar(reader, target, member)
}

func extractTar(data []byte, target string, member string) (map[string][]byte, error) {
	return readTar(bytes.NewReader(data), target, member)
}

func readTar(reader io.Reader, target string, member string) (map[string][]byte, error) {
	tarReader := tar.NewReader(reader)
	files := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.FileInfo().IsDir() {
			continue
		}
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("archive links are not supported: %s", header.Name)
		}
		if err := validateArchivePath(header.Name); err != nil {
			return nil, err
		}
		if member != "" && header.Name != member && filepath.Base(header.Name) != member {
			continue
		}
		content, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, err
		}
		files[header.Name] = content
	}
	return selectExtractedFiles(files, target, member)
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func selectExtractedFiles(files map[string][]byte, target string, member string) (map[string][]byte, error) {
	if target == TargetGeo {
		selected := map[string][]byte{}
		for name, content := range files {
			baseName := filepath.Base(name)
			if baseName == "geoip.dat" || baseName == "geosite.dat" {
				if _, exists := selected[baseName]; exists {
					return nil, fmt.Errorf("archive contains duplicate geo file: %s", baseName)
				}
				selected[baseName] = content
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("geo archive must contain geoip.dat or geosite.dat")
		}
		return selected, nil
	}
	if member != "" {
		name, content, ok, err := selectArchiveMember(files, member)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("archive member not found: %s", member)
		}
		return singleTargetFile(target, filepath.Base(name), content)
	}
	if binaryName := binaryTargetName(target); binaryName != "" {
		matches := matchingArchiveBaseNames(files, binaryName)
		if len(matches) != 1 {
			return nil, fmt.Errorf("archive-member is required for %s archive", target)
		}
		return singleTargetFile(target, binaryName, files[matches[0]])
	}
	if len(files) != 1 {
		return nil, fmt.Errorf("archive member is required when archive has %d files", len(files))
	}
	for name, content := range files {
		return singleTargetFile(target, filepath.Base(name), content)
	}
	return nil, fmt.Errorf("archive contains no files")
}

func selectArchiveMember(files map[string][]byte, member string) (string, []byte, bool, error) {
	if content, ok := files[member]; ok {
		return member, content, true, nil
	}
	matches := matchingArchiveBaseNames(files, filepath.Base(member))
	if len(matches) == 0 {
		return "", nil, false, nil
	}
	if len(matches) > 1 {
		return "", nil, false, fmt.Errorf("archive member is ambiguous: %s", member)
	}
	return matches[0], files[matches[0]], true, nil
}

func matchingArchiveBaseNames(files map[string][]byte, baseName string) []string {
	matches := make([]string, 0)
	for name := range files {
		if filepath.Base(name) == baseName {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

func binaryTargetName(target string) string {
	switch target {
	case TargetMihomo:
		return "mihomo"
	case TargetXray:
		return "xray"
	default:
		return ""
	}
}

func singleTargetFile(target string, name string, content []byte) (map[string][]byte, error) {
	switch target {
	case TargetMihomo:
		return map[string][]byte{"mihomo": content}, nil
	case TargetXray:
		return map[string][]byte{"xray": content}, nil
	case TargetGeo:
		return map[string][]byte{name: content}, nil
	default:
		return nil, fmt.Errorf("unsupported extract target: %s", target)
	}
}

func targetPaths(config domain.GlobalConfig, target string) map[string]string {
	switch target {
	case TargetMihomo:
		return map[string]string{"mihomo": filepath.Join(config.ResolvePath(config.Paths.Bin), "mihomo")}
	case TargetXray:
		return map[string]string{"xray": filepath.Join(config.ResolvePath(config.Paths.Bin), "xray")}
	case TargetGeo:
		return map[string]string{
			"geoip.dat":   filepath.Join(config.ResolvePath(config.Paths.Geo), "geoip.dat"),
			"geosite.dat": filepath.Join(config.ResolvePath(config.Paths.Geo), "geosite.dat"),
		}
	default:
		return nil
	}
}

func writeTargetFiles(paths map[string]string, files map[string][]byte) ([]string, error) {
	written := make([]string, 0, len(files))
	backups := map[string]backupFile{}
	created := make([]string, 0)
	names := make([]string, 0, len(files))
	for name := range files {
		if _, ok := paths[name]; !ok {
			return nil, fmt.Errorf("unexpected extracted file for target: %s", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := files[name]
		path, ok := paths[name]
		if !ok {
			return nil, fmt.Errorf("unexpected extracted file for target: %s", name)
		}
		if existing, err := os.ReadFile(path); err == nil {
			mode := os.FileMode(0o640)
			if info, statErr := os.Stat(path); statErr == nil {
				mode = info.Mode().Perm()
			}
			backups[path] = backupFile{Content: existing, Mode: mode}
		} else if os.IsNotExist(err) {
			created = append(created, path)
		} else {
			rollback(backups, created)
			return nil, err
		}
		mode := os.FileMode(0o640)
		if name == TargetMihomo || name == TargetXray || name == "mihomo" || name == "xray" {
			mode = 0o750
		}
		if err := writeFileAtomic(path, content, mode); err != nil {
			rollback(backups, created)
			return nil, err
		}
		written = append(written, path)
	}
	sort.Strings(written)
	return written, nil
}

type backupFile struct {
	Content []byte
	Mode    os.FileMode
}

func rollback(backups map[string]backupFile, created []string) {
	for _, path := range created {
		_ = os.Remove(path)
	}
	for path, backup := range backups {
		_ = writeFileAtomic(path, backup.Content, backup.Mode)
	}
}

func allExist(paths map[string]string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func requestConfig(config domain.GlobalConfig, target string) domain.InstallToolConfig {
	switch target {
	case TargetMihomo:
		return config.Install.Mihomo
	case TargetXray:
		return config.Install.Xray
	case TargetGeo:
		return config.Install.Geo
	default:
		return domain.InstallToolConfig{}
	}
}

func isPlainRemoteSource(source string) bool {
	parsed, err := url.Parse(source)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func isManagedSource(source string) bool {
	return source == "" || source == SourceAuto || source == SourceGitHub || source == SourceR2
}

func verifySHA256(data []byte, expected string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", actual, expected)
	}
	return nil
}

func validateArchivePath(name string) error {
	if filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return fmt.Errorf("archive member path is unsafe: %s", name)
	}
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "..") || strings.Contains(clean, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return fmt.Errorf("archive member path is unsafe: %s", name)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}

// githubLatestAPIURL 返回指定目标的 GitHub latest release metadata URL。
func githubLatestAPIURL(target string) string {
	switch target {
	case TargetMihomo:
		return "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	case TargetXray:
		return "https://api.github.com/repos/XTLS/Xray-core/releases/latest"
	case TargetGeo:
		return "https://api.github.com/repos/Loyalsoldier/v2ray-rules-dat/releases/latest"
	default:
		return ""
	}
}

// githubReleaseTagAPIURL 返回指定目标和 tag 的 GitHub release metadata URL。
func githubReleaseTagAPIURL(target string, tag string) string {
	escapedTag := url.PathEscape(tag)
	switch target {
	case TargetMihomo:
		return "https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/" + escapedTag
	case TargetXray:
		return "https://api.github.com/repos/XTLS/Xray-core/releases/tags/" + escapedTag
	case TargetGeo:
		return "https://api.github.com/repos/Loyalsoldier/v2ray-rules-dat/releases/tags/" + escapedTag
	default:
		return ""
	}
}

// githubAssetName 返回当前平台优先匹配的 GitHub 资产名。
func githubAssetName(target string, version string) (string, error) {
	return githubAssetNameForPlatform(target, version, runtime.GOOS, runtime.GOARCH)
}

// githubAssetNameForPlatform 返回指定平台优先匹配的 GitHub 资产名。
func githubAssetNameForPlatform(target string, version string, goos string, goarch string) (string, error) {
	switch target {
	case TargetMihomo:
		return mihomoAssetName(goos, goarch, version), nil
	case TargetXray:
		platform, err := xrayPlatformNameForPlatform(goos, goarch)
		if err != nil {
			return "", err
		}
		return "Xray-" + platform + ".zip", nil
	case TargetGeo:
		return "rules.zip", nil
	default:
		return "", fmt.Errorf("unsupported install target: %s", target)
	}
}

// selectGitHubReleaseAsset 从 release assets 中选择当前平台最合适的真实下载 URL。
func selectGitHubReleaseAsset(target string, release githubReleaseResponse) (string, error) {
	return selectGitHubReleaseAssetForPlatform(target, release, runtime.GOOS, runtime.GOARCH)
}

// selectGitHubReleaseAssetForPlatform 从 release assets 中选择指定平台最合适的真实下载 URL。
func selectGitHubReleaseAssetForPlatform(target string, release githubReleaseResponse, goos string, goarch string) (string, error) {
	assetName, err := githubAssetNameForPlatform(target, release.TagName, goos, goarch)
	if err != nil {
		return "", err
	}
	for _, asset := range release.Assets {
		if asset.Name == assetName && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, nil
		}
	}
	if target == TargetMihomo {
		for _, asset := range release.Assets {
			if matchesMihomoAsset(asset.Name, release.TagName, goos, goarch) && asset.BrowserDownloadURL != "" {
				return asset.BrowserDownloadURL, nil
			}
		}
	}
	return "", fmt.Errorf("expected asset %s, available assets: %s", assetName, strings.Join(githubAssetNames(release.Assets), ", "))
}

// githubAssetNames 返回 release metadata 中的资产名，用于错误提示。
func githubAssetNames(assets []githubAssetResult) []string {
	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.Name)
	}
	sort.Strings(names)
	return names
}

// mihomoAssetName 返回当前平台优先使用的 mihomo GitHub 资产名。
func mihomoAssetName(goos string, goarch string, version string) string {
	if goos == "linux" && goarch == "amd64" {
		return fmt.Sprintf("mihomo-linux-amd64-compatible-%s.gz", version)
	}
	return fmt.Sprintf("mihomo-%s-%s-%s.gz", goos, goarch, version)
}

// matchesMihomoAsset 判断 mihomo 资产名是否同时匹配版本、平台和兼容性关键字。
func matchesMihomoAsset(name string, version string, goos string, goarch string) bool {
	lowerName := strings.ToLower(name)
	required := []string{"mihomo", strings.ToLower(goos), strings.ToLower(goarch)}
	for _, token := range required {
		if token == "" || !strings.Contains(lowerName, token) {
			return false
		}
	}
	if goos == "linux" && goarch == "amd64" && !strings.Contains(lowerName, "compatible") {
		return false
	}
	lowerVersion := strings.ToLower(version)
	trimmedVersion := strings.ToLower(trimVersionPrefix(version))
	return strings.Contains(lowerName, lowerVersion) || strings.Contains(lowerName, trimmedVersion)
}

// xrayPlatformName 返回当前平台对应的 Xray release 资产平台名。
func xrayPlatformName() (string, error) {
	return xrayPlatformNameForPlatform(runtime.GOOS, runtime.GOARCH)
}

// xrayPlatformNameForPlatform 返回指定平台对应的 Xray release 资产平台名。
func xrayPlatformNameForPlatform(goos string, goarch string) (string, error) {
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			return "macos-64", nil
		case "arm64":
			return "macos-arm64-v8a", nil
		}
	case "linux":
		switch goarch {
		case "386":
			return "linux-32", nil
		case "amd64":
			return "linux-64", nil
		case "arm":
			return "linux-arm32-v7a", nil
		case "arm64":
			return "linux-arm64-v8a", nil
		case "loong64":
			return "linux-loong64", nil
		case "mips":
			return "linux-mips32", nil
		case "mipsle":
			return "linux-mips32le", nil
		case "mips64":
			return "linux-mips64", nil
		case "mips64le":
			return "linux-mips64le", nil
		case "ppc64":
			return "linux-ppc64", nil
		case "ppc64le":
			return "linux-ppc64le", nil
		case "riscv64":
			return "linux-riscv64", nil
		case "s390x":
			return "linux-s390x", nil
		}
	case "windows":
		switch goarch {
		case "386":
			return "windows-32", nil
		case "amd64":
			return "windows-64", nil
		case "arm64":
			return "windows-arm64-v8a", nil
		}
	case "freebsd":
		switch goarch {
		case "386":
			return "freebsd-32", nil
		case "amd64":
			return "freebsd-64", nil
		case "arm":
			return "freebsd-arm32-v7a", nil
		case "arm64":
			return "freebsd-arm64-v8a", nil
		}
	case "openbsd":
		switch goarch {
		case "386":
			return "openbsd-32", nil
		case "amd64":
			return "openbsd-64", nil
		case "arm":
			return "openbsd-arm32-v7a", nil
		case "arm64":
			return "openbsd-arm64-v8a", nil
		}
	}
	return "", fmt.Errorf("xray github asset is not mapped for %s/%s", goos, goarch)
}

func downloadName(parsed *url.URL) string {
	name := filepath.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	return name
}

func downloadNameForSource(source string) string {
	parsed, err := url.Parse(source)
	if err != nil {
		return filepath.Base(source)
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "file" {
		return downloadName(parsed)
	}
	name := filepath.Base(source)
	if name == "." || name == "/" || name == "" {
		return "download"
	}
	return name
}

func sourceLabel(source string) string {
	switch source {
	case "", SourceAuto:
		return SourceGitHub
	case SourceGitHub, SourceR2:
		return source
	default:
		parsed, err := url.Parse(source)
		if err == nil && parsed.Scheme != "" {
			return parsed.Scheme
		}
		return "local"
	}
}

func emitProgress(progress Progress, message string) {
	if progress == nil {
		return
	}
	progress(message)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	writeErr := func() error {
		if _, err := temp.Write(data); err != nil {
			return err
		}
		if err := temp.Chmod(mode); err != nil {
			return err
		}
		return temp.Sync()
	}()
	closeErr := temp.Close()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
