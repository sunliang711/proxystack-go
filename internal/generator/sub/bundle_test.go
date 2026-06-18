package sub_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backupgen "github.com/eagle/proxystack-go/internal/generator/backup"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/stretchr/testify/require"
)

// TestBundleWriteAndExtract 验证 bundle 写入和导入 inputs。
func TestBundleWriteAndExtract(t *testing.T) {
	input := loadManualInput(t)
	content := []byte(subgen.InputToYAML(input))
	bundlePath := filepath.Join(t.TempDir(), "sub-bundle.zip")

	manifest, err := subgen.WriteBundle(bundlePath, "manual", fixedGeneratedAt, []subgen.BundleInputFile{{Name: "manual.yaml", Content: content}})
	require.NoError(t, err)
	require.Equal(t, subgen.BundleSchema, manifest.BundleSchema)

	dataDir := t.TempDir()
	result, err := subgen.ExtractBundleInputs(bundlePath, dataDir, false)
	require.NoError(t, err)
	require.Equal(t, []string{"manual.yaml"}, result.WrittenInputs)
	require.FileExists(t, filepath.Join(dataDir, "inputs", "manual.yaml"))
}

// TestBundleReplaceAllValidationFailureKeepsOldInputs 验证 replace-all 只有完整校验成功才替换旧 inputs。
func TestBundleReplaceAllValidationFailureKeepsOldInputs(t *testing.T) {
	input := loadManualInput(t)
	content := []byte(subgen.InputToYAML(input))
	bundlePath := filepath.Join(t.TempDir(), "duplicate-bundle.zip")
	writeBundleWithInputs(t, bundlePath, map[string][]byte{
		"a.yaml": content,
		"b.yaml": content,
	})
	dataDir := t.TempDir()
	inputDir := filepath.Join(dataDir, "inputs")
	require.NoError(t, os.MkdirAll(inputDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "old.yaml"), []byte("old-input"), 0o644))

	_, err := subgen.ExtractBundleInputs(bundlePath, dataDir, true)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate node id")
	require.FileExists(t, filepath.Join(inputDir, "old.yaml"))
	require.NoFileExists(t, filepath.Join(inputDir, "a.yaml"))
	require.NoFileExists(t, filepath.Join(inputDir, "b.yaml"))
}

// TestBundleHashMismatchDoesNotWrite 验证 hash mismatch 时不会写入任何 input。
func TestBundleHashMismatchDoesNotWrite(t *testing.T) {
	input := loadManualInput(t)
	content := []byte(subgen.InputToYAML(input))
	bundlePath := filepath.Join(t.TempDir(), "bad-bundle.zip")
	writeCorruptBundle(t, bundlePath, "manual.yaml", content, "tampered")
	dataDir := t.TempDir()

	_, err := subgen.ExtractBundleInputs(bundlePath, dataDir, false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "input hash mismatch")
	require.NoFileExists(t, filepath.Join(dataDir, "inputs", "manual.yaml"))
}

// TestBundleRejectsUnsafeMembers 验证路径穿越、反斜杠和未知成员都会被拒绝。
func TestBundleRejectsUnsafeMembers(t *testing.T) {
	tests := []string{"inputs/../evil.yaml", `inputs\evil.yaml`, "runtime/manifest.json"}
	for _, member := range tests {
		t.Run(member, func(t *testing.T) {
			bundlePath := filepath.Join(t.TempDir(), "bad.zip")
			writeZip(t, bundlePath, map[string][]byte{
				"manifest.json": []byte(`{"bundle_schema":"proxystack.sub-bundle","bundle_version":1,"source":"x","generated_at":"` + fixedGeneratedAt + `","inputs_sha256":{},"template_version":"builtin-v1","access":{"type":"none"}}`),
				member:          []byte("x"),
			})

			_, err := subgen.ExtractBundleInputs(bundlePath, t.TempDir(), false)

			require.Error(t, err)
		})
	}
}

// TestBundleRejectsNativeBackupSchema 验证 bundle 和 native backup manifest schema 不可混用。
func TestBundleRejectsNativeBackupSchema(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "native.zip")
	writeZip(t, bundlePath, map[string][]byte{
		"manifest.json": []byte(`{"backup_schema":"proxystack.native-backup","backup_version":1,"created_at":"` + fixedGeneratedAt + `","files_sha256":{"config/config.yaml":"` + strings.Repeat("0", 64) + `"}}`),
	})

	_, err := subgen.ExtractBundleInputs(bundlePath, t.TempDir(), false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "bundle manifest schema is invalid")
}

// TestNativeBackupRejectsBundleSchema 验证 native backup 读取会拒绝订阅 bundle schema。
func TestNativeBackupRejectsBundleSchema(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "sub-bundle.zip")
	_, err := subgen.WriteBundle(bundlePath, "manual", fixedGeneratedAt, []subgen.BundleInputFile{{Name: "manual.yaml", Content: []byte(subgen.InputToYAML(loadManualInput(t)))}})
	require.NoError(t, err)

	_, _, err = backupgen.ReadNativeBackup(bundlePath)

	require.Error(t, err)
	require.Contains(t, err.Error(), "native backup manifest schema is invalid")
}

// TestNativeBackupRestoreReplacesAgentFilesOnly 验证 native restore 只替换 config.yaml 和 stacks。
func TestNativeBackupRestoreReplacesAgentFilesOnly(t *testing.T) {
	sourceDir := prepareNativeBackupSource(t)
	bundlePath := filepath.Join(t.TempDir(), "native.zip")
	_, err := backupgen.WriteNativeBackup(bundlePath, filepath.Join(sourceDir, "config.yaml"), filepath.Join(sourceDir, "stacks"), fixedGeneratedAt)
	require.NoError(t, err)
	targetDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "stacks"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config.yaml"), []byte("old-config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "stacks", "old.yaml"), []byte("old-stack"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "sub", "config.yaml"), []byte("keep-sub"), 0o644))

	_, err = backupgen.RestoreNativeBackup(bundlePath, targetDir)

	require.NoError(t, err)
	configData, err := os.ReadFile(filepath.Join(targetDir, "config.yaml"))
	require.NoError(t, err)
	require.NotContains(t, string(configData), "base_dir:")
	require.FileExists(t, filepath.Join(targetDir, "stacks", "edge.yaml"))
	require.NoFileExists(t, filepath.Join(targetDir, "stacks", "old.yaml"))
	require.FileExists(t, filepath.Join(targetDir, "sub", "config.yaml"))
}

func writeCorruptBundle(t *testing.T, path string, inputName string, content []byte, replacement string) {
	t.Helper()
	manifest := subgen.BundleManifest{
		BundleSchema:    subgen.BundleSchema,
		BundleVersion:   subgen.BundleVersion,
		Source:          "manual",
		GeneratedAt:     fixedGeneratedAt,
		InputsSHA256:    map[string]string{inputName: strings.Repeat("0", 64)},
		TemplateVersion: "builtin-v1",
		Access:          subgen.Access{Type: "none"},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	writeZip(t, path, map[string][]byte{
		"manifest.json":       manifestData,
		"inputs/" + inputName: []byte(replacement + string(content)),
	})
}

func writeBundleWithInputs(t *testing.T, path string, inputs map[string][]byte) {
	t.Helper()
	hashes := map[string]string{}
	members := map[string][]byte{}
	for name, content := range inputs {
		hashes[name] = sha256Hex(content)
		members["inputs/"+name] = content
	}
	manifest := subgen.BundleManifest{
		BundleSchema:    subgen.BundleSchema,
		BundleVersion:   subgen.BundleVersion,
		Source:          "manual",
		GeneratedAt:     fixedGeneratedAt,
		InputsSHA256:    hashes,
		TemplateVersion: "builtin-v1",
		Access:          subgen.Access{Type: "none"},
	}
	manifestData, err := json.Marshal(manifest)
	require.NoError(t, err)
	members["manifest.json"] = manifestData
	writeZip(t, path, members)
}

func writeZip(t *testing.T, path string, members map[string][]byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, content := range members {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func prepareNativeBackupSource(t *testing.T) string {
	t.Helper()
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(validNativeConfigYAML(baseDir)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "edge.yaml"), []byte(validNativeStackYAML("edge")), 0o644))
	return baseDir
}

func validNativeConfigYAML(baseDir string) string {
	return `version: 1
paths:
  stacks: stacks
port_ranges:
  xrelay_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
`
}

func validNativeStackYAML(name string) string {
	return `name: ` + name + `
enabled: true
role: edge
xrelay:
  enabled: true
  api:
    enabled: false
  stats:
    enabled: false
  policy:
    enabled: false
  outbound:
    type: direct
  inbounds:
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
    secret: demo-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
  upstreams: []
  groups:
    - name: AllProxy
      type: select
      proxies: [DIRECT]
  rules:
    profile: default
`
}
