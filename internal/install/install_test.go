package install

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/stretchr/testify/require"
)

// fakeDownloader 为安装测试返回固定内容。
type fakeDownloader struct {
	data         []byte
	dataBySource map[string][]byte
	err          error
}

// Download 返回预设内容，不访问网络。
func (f fakeDownloader) Download(ctx context.Context, source string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.dataBySource != nil {
		data, ok := f.dataBySource[source]
		if !ok {
			return nil, errors.New("unexpected source: " + source)
		}
		return data, nil
	}
	return f.data, nil
}

// TestInstallSkipsExistingAndUpdateReplaces 验证 install 跳过已有目标，update 强制替换。
func TestInstallSkipsExistingAndUpdateReplaces(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	installer := Installer{Downloader: fakeDownloader{data: gzipBytes(t, []byte("first"))}}

	results, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetMihomo, Source: "mihomo.gz"})
	require.NoError(t, err)
	require.False(t, results[0].Skipped)
	require.FileExists(t, cfg.ResolvePath("bin/mihomo"))

	results, err = installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetMihomo, Source: "mihomo.gz"})
	require.NoError(t, err)
	require.True(t, results[0].Skipped)

	installer.Downloader = fakeDownloader{data: gzipBytes(t, []byte("second"))}
	results, err = installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetMihomo, Source: "mihomo.gz", Force: true})
	require.NoError(t, err)
	require.False(t, results[0].Skipped)
	require.FileExists(t, results[0].Written[0])
}

// TestRemoteURLRequiresSHA 验证普通远端 URL 必须显式提供 sha256。
func TestRemoteURLRequiresSHA(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	installer := Installer{Downloader: fakeDownloader{data: gzipBytes(t, []byte("xray"))}}

	_, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetXray, Source: "https://127.0.0.1/xray.gz"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "sha256 is required")
}

// TestManagedSourcesGitHubRequiresMetadata 验证 GitHub 托管源不再手动拼接下载 URL。
func TestManagedSourcesGitHubRequiresMetadata(t *testing.T) {
	_, err := ManagedSources(TargetMihomo, "v1.0.0", SourceAuto)

	require.Error(t, err)
	require.Contains(t, err.Error(), "github source requires release metadata resolution")
}

// TestManagedSourcesR2ReturnsClearError 验证显式 r2 源在未配置时返回清晰错误。
func TestManagedSourcesR2ReturnsClearError(t *testing.T) {
	_, err := ManagedSources(TargetMihomo, "v1.0.0", SourceR2)

	require.Error(t, err)
	require.Contains(t, err.Error(), "r2 managed source is not configured")
}

// TestResolveSourcesLatestGitHubUsesReleaseAsset 验证 latest 会解析 GitHub release 元数据里的真实资产 URL。
func TestResolveSourcesLatestGitHubUsesReleaseAsset(t *testing.T) {
	assetName, err := githubAssetName(TargetMihomo, "v9.9.9")
	require.NoError(t, err)
	assetURL := "https://github.com/MetaCubeX/mihomo/releases/download/v9.9.9/" + assetName
	releaseJSON := `{"tag_name":"v9.9.9","assets":[{"name":"` + assetName + `","browser_download_url":"` + assetURL + `"}]}`

	sources, err := resolveSources(context.Background(), fakeDownloader{data: []byte(releaseJSON)}, TargetMihomo, "latest", SourceGitHub)

	require.NoError(t, err)
	require.Equal(t, []string{assetURL}, sources)
}

// TestResolveSourcesVersionGitHubUsesReleaseAsset 验证固定版本会从 GitHub metadata 选择真实资产 URL。
func TestResolveSourcesVersionGitHubUsesReleaseAsset(t *testing.T) {
	assetName, err := githubAssetName(TargetMihomo, "v1.19.27")
	require.NoError(t, err)
	assetURL := "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.27/" + assetName
	releaseJSON := `{"tag_name":"v1.19.27","assets":[{"name":"` + assetName + `","browser_download_url":"` + assetURL + `"}]}`

	sources, err := resolveSources(context.Background(), fakeDownloader{dataBySource: map[string][]byte{
		githubReleaseTagAPIURL(TargetMihomo, "v1.19.27"): []byte(releaseJSON),
	}}, TargetMihomo, "1.19.27", SourceGitHub)

	require.NoError(t, err)
	require.Equal(t, []string{assetURL}, sources)
}

// TestResolveSourcesVersionGitHubMatchesTagWithoutV 验证配置带 v 时也能匹配不带 v 的 metadata tag。
func TestResolveSourcesVersionGitHubMatchesTagWithoutV(t *testing.T) {
	assetName, err := githubAssetName(TargetMihomo, "1.19.27")
	require.NoError(t, err)
	assetURL := "https://github.com/MetaCubeX/mihomo/releases/download/1.19.27/" + assetName
	releaseJSON := `{"tag_name":"1.19.27","assets":[{"name":"` + assetName + `","browser_download_url":"` + assetURL + `"}]}`

	sources, err := resolveSources(context.Background(), fakeDownloader{dataBySource: map[string][]byte{
		githubReleaseTagAPIURL(TargetMihomo, "1.19.27"): []byte(releaseJSON),
	}}, TargetMihomo, "v1.19.27", SourceGitHub)

	require.NoError(t, err)
	require.Equal(t, []string{assetURL}, sources)
}

// TestSelectMihomoLinuxAMD64RequiresCompatible 验证 mihomo Linux amd64 必须选择 compatible 资产。
func TestSelectMihomoLinuxAMD64RequiresCompatible(t *testing.T) {
	release := githubReleaseResponse{
		TagName: "v1.19.27",
		Assets: []githubAssetResult{
			{Name: "mihomo-linux-amd64-v1.19.27.gz", BrowserDownloadURL: "https://example.com/plain.gz"},
			{Name: "mihomo-linux-amd64-compatible-v1.19.27.gz", BrowserDownloadURL: "https://example.com/compatible.gz"},
		},
	}

	source, err := selectGitHubReleaseAssetForPlatform(TargetMihomo, release, "linux", "amd64")

	require.NoError(t, err)
	require.Equal(t, "https://example.com/compatible.gz", source)
}

// TestSelectMihomoLinuxAMD64RejectsNonCompatible 验证 mihomo Linux amd64 不接受非 compatible 资产。
func TestSelectMihomoLinuxAMD64RejectsNonCompatible(t *testing.T) {
	release := githubReleaseResponse{
		TagName: "v1.19.27",
		Assets: []githubAssetResult{
			{Name: "mihomo-linux-amd64-v1.19.27.gz", BrowserDownloadURL: "https://example.com/plain.gz"},
		},
	}

	_, err := selectGitHubReleaseAssetForPlatform(TargetMihomo, release, "linux", "amd64")

	require.Error(t, err)
	require.Contains(t, err.Error(), "compatible")
}

// TestSelectGitHubReleaseAssetsForXrayAndGeo 验证 xray 和 geo 继续按 metadata 资产名选择 URL。
func TestSelectGitHubReleaseAssetsForXrayAndGeo(t *testing.T) {
	xraySource, err := selectGitHubReleaseAssetForPlatform(TargetXray, githubReleaseResponse{
		TagName: "v26.3.27",
		Assets: []githubAssetResult{
			{Name: "Xray-linux-64.zip", BrowserDownloadURL: "https://example.com/Xray-linux-64.zip"},
		},
	}, "linux", "amd64")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/Xray-linux-64.zip", xraySource)

	geoSource, err := selectGitHubReleaseAssetForPlatform(TargetGeo, githubReleaseResponse{
		TagName: "202606162319",
		Assets: []githubAssetResult{
			{Name: "rules.zip", BrowserDownloadURL: "https://example.com/rules.zip"},
		},
	}, "linux", "amd64")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/rules.zip", geoSource)
}

// TestHTTPDownloaderStatusErrorIncludesURL 验证 HTTP 状态错误包含 URL 和状态码说明。
func TestHTTPDownloaderStatusErrorIncludesURL(t *testing.T) {
	downloader := HTTPDownloader{Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}}

	_, err := downloader.Download(context.Background(), "https://example.com/missing.gz")

	require.Error(t, err)
	require.Contains(t, err.Error(), "GET https://example.com/missing.gz")
	require.Contains(t, err.Error(), "404 Not Found")
}

// TestInstallDownloadErrorIncludesTargetAndSource 验证安装下载失败会带目标和来源，便于定位。
func TestInstallDownloadErrorIncludesTargetAndSource(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	installer := Installer{Downloader: fakeDownloader{err: errors.New("boom")}}

	_, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetMihomo, Source: "local.gz"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "mihomo download from local.gz failed")
	require.Contains(t, err.Error(), "boom")
}

// TestInstallerEmitsProgress 验证安装流程会输出关键阶段进度。
func TestInstallerEmitsProgress(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	assetName, err := githubAssetName(TargetMihomo, "v1.0.0")
	require.NoError(t, err)
	assetURL := "https://github.com/MetaCubeX/mihomo/releases/download/v1.0.0/" + assetName
	releaseJSON := `{"tag_name":"v1.0.0","assets":[{"name":"` + assetName + `","browser_download_url":"` + assetURL + `"}]}`
	messages := []string{}
	installer := Installer{
		Downloader: fakeDownloader{dataBySource: map[string][]byte{
			githubReleaseTagAPIURL(TargetMihomo, "v1.0.0"): []byte(releaseJSON),
			assetURL: gzipBytes(t, []byte("mihomo")),
		}},
		Progress: func(message string) {
			messages = append(messages, message)
		},
	}

	_, err = installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetMihomo, Version: "v1.0.0", Source: SourceGitHub})

	require.NoError(t, err)
	require.Contains(t, messages, "install: prepare mihomo")
	require.Contains(t, messages, "download: resolve mihomo v1.0.0 via github")
	require.Contains(t, messages, "download: selected github")
	require.Contains(t, messages, "install: verify "+assetName)
	require.Contains(t, messages, "install: install mihomo")
	require.Contains(t, messages, "install: complete mihomo")
}

// TestHTTPDownloaderEmitsByteProgress 验证 HTTP 下载会输出 start/progress/complete 进度。
func TestHTTPDownloaderEmitsByteProgress(t *testing.T) {
	messages := []string{}
	downloader := HTTPDownloader{
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := []byte("test")
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Body:          io.NopCloser(bytes.NewReader(body)),
				ContentLength: int64(len(body)),
				Header:        make(http.Header),
				Request:       request,
			}, nil
		})},
		Progress: func(message string) {
			messages = append(messages, message)
		},
	}

	data, err := downloader.Download(context.Background(), "https://example.com/file.gz")

	require.NoError(t, err)
	require.Equal(t, []byte("test"), data)
	require.True(t, containsPrefix(messages, "download: start file.gz"))
	require.True(t, containsPrefix(messages, "download: progress file.gz"))
	require.True(t, containsPrefix(messages, "download: complete file.gz"))
}

// TestHTTPDownloaderUsesLowercaseHTTPProxy 验证默认下载器会读取 http_proxy 环境变量。
func TestHTTPDownloaderUsesLowercaseHTTPProxy(t *testing.T) {
	proxyAddress, requests := startOneShotHTTPProxy(t)
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("http_proxy", "http://"+proxyAddress)
	t.Setenv("https_proxy", "")
	t.Setenv("no_proxy", "")

	data, err := HTTPDownloader{}.Download(context.Background(), "http://example.com/file.gz")

	require.NoError(t, err)
	require.Equal(t, []byte("test"), data)
	require.Contains(t, <-requests, "GET http://example.com/file.gz HTTP/1.1")
}

// TestValidateRemoteURLRejectsLoopback 验证 SSRF 防护拒绝本机地址。
func TestValidateRemoteURLRejectsLoopback(t *testing.T) {
	err := ValidateRemoteURL("https://127.0.0.1/xray.gz")

	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked address")
}

// TestSecureDialRejectsLoopback 验证实际 dial 前也会拒绝 loopback 地址。
func TestSecureDialRejectsLoopback(t *testing.T) {
	_, err := secureDialContext(context.Background(), "tcp", "127.0.0.1:443")

	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked address")
}

// TestExtractZipRejectsUnsafePath 验证归档成员路径不能逃逸。
func TestExtractZipRejectsUnsafePath(t *testing.T) {
	data := zipBytes(t, map[string][]byte{"../xray": []byte("bad")})

	_, err := ExtractFiles("xray.zip", data, TargetXray, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe")
}

// TestExtractZipAutoSelectsXrayBinary 验证 Xray 官方多文件 zip 会自动选择 xray 成员。
func TestExtractZipAutoSelectsXrayBinary(t *testing.T) {
	data := zipBytes(t, map[string][]byte{
		"LICENSE":   []byte("license"),
		"README.md": []byte("readme"),
		"xray":      []byte("binary"),
	})

	files, err := ExtractFiles("Xray-linux-64.zip", data, TargetXray, "")

	require.NoError(t, err)
	require.Equal(t, map[string][]byte{"xray": []byte("binary")}, files)
}

// TestExtractZipRejectsAmbiguousXrayBinary 验证多个 xray 成员时仍要求显式 archive-member。
func TestExtractZipRejectsAmbiguousXrayBinary(t *testing.T) {
	data := zipBytes(t, map[string][]byte{
		"linux/xray":  []byte("linux"),
		"darwin/xray": []byte("darwin"),
	})

	_, err := ExtractFiles("Xray-linux-64.zip", data, TargetXray, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "archive-member is required for xray archive")
}

// TestInstallUsesConfiguredSHAAndArchiveMember 验证安装流程会读取配置中的 sha256 和 archive_member。
func TestInstallUsesConfiguredSHAAndArchiveMember(t *testing.T) {
	data := zipBytes(t, map[string][]byte{
		"bin/xray":  []byte("configured-binary"),
		"README.md": []byte("readme"),
	})
	sum := sha256.Sum256(data)
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	cfg.Install.Xray = domain.InstallToolConfig{
		Source:        "https://example.com/xray.zip",
		SHA256:        hex.EncodeToString(sum[:]),
		ArchiveMember: "bin/xray",
	}
	installer := Installer{Downloader: fakeDownloader{data: data}}

	results, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetXray})

	require.NoError(t, err)
	require.Equal(t, []string{cfg.ResolvePath("bin/xray")}, results[0].Written)
	content, err := os.ReadFile(cfg.ResolvePath("bin/xray"))
	require.NoError(t, err)
	require.Equal(t, []byte("configured-binary"), content)
}

// TestGeoZipWritesMultipleFiles 验证 geo 归档可事务写入多个文件。
func TestGeoZipWritesMultipleFiles(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	data := zipBytes(t, map[string][]byte{"geoip.dat": []byte("ip"), "geosite.dat": []byte("site")})
	sum := sha256.Sum256(data)
	installer := Installer{Downloader: fakeDownloader{data: data}}

	results, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetGeo, Source: "https://example.com/geo.zip", SHA256: hex.EncodeToString(sum[:])})

	require.NoError(t, err)
	require.ElementsMatch(t, []string{cfg.ResolvePath("geo/geoip.dat"), cfg.ResolvePath("geo/geosite.dat")}, results[0].Written)
}

// TestGeoRollbackRemovesCreatedFiles 验证 geo 多文件写入失败会删除本轮新建文件。
func TestGeoRollbackRemovesCreatedFiles(t *testing.T) {
	cfg := domain.GlobalConfig{BaseDir: t.TempDir(), Paths: domain.DefaultConfigPaths()}
	require.NoError(t, os.MkdirAll(cfg.ResolvePath("geo/geosite.dat"), 0o750))
	data := zipBytes(t, map[string][]byte{"geoip.dat": []byte("ip"), "geosite.dat": []byte("site")})
	installer := Installer{Downloader: fakeDownloader{data: data}}

	_, err := installer.InstallTarget(context.Background(), Request{Config: cfg, Target: TargetGeo, Source: "geo.zip", Force: true})

	require.Error(t, err)
	_, statErr := os.Stat(cfg.ResolvePath("geo/geoip.dat"))
	require.True(t, os.IsNotExist(statErr))
}

type roundTripFunc func(request *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func startOneShotHTTPProxy(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	requests := make(chan string, 1)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			requests <- "accept error: " + err.Error()
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 4096)
		count, err := conn.Read(buffer)
		if err != nil {
			requests <- "read error: " + err.Error()
			return
		}
		requests <- string(buffer[:count])
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ntest"))
	}()
	return listener.Addr().String(), requests
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, err := writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
