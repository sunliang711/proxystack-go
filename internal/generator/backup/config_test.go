package backup

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/stretchr/testify/require"
)

// TestRestoreNativeBackupStripsLegacyBaseDir 验证旧备份里的 base_dir 会在恢复时被剥离。
func TestRestoreNativeBackupStripsLegacyBaseDir(t *testing.T) {
	backupPath := filepath.Join(t.TempDir(), "proxystack-backup.zip")
	legacyConfig := []byte(`version: 1
base_dir: /legacy/proxystack
paths:
  stacks: stacks
port_ranges:
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
`)
	writeNativeBackupForTest(t, backupPath, legacyConfig)
	targetDir := t.TempDir()

	_, err := RestoreNativeBackup(backupPath, targetDir)

	require.NoError(t, err)
	targetConfigPath := filepath.Join(targetDir, "config.yaml")
	data, err := os.ReadFile(targetConfigPath)
	require.NoError(t, err)
	require.NotContains(t, string(data), "base_dir:")
	loadedConfig, err := config.LoadConfig(targetConfigPath)
	require.NoError(t, err)
	require.Equal(t, targetDir, loadedConfig.BaseDir)
}

// writeNativeBackupForTest 写入包含指定 config 的最小原生备份。
func writeNativeBackupForTest(t *testing.T, backupPath string, configData []byte) {
	t.Helper()
	file, err := os.Create(backupPath)
	require.NoError(t, err)
	defer file.Close()
	zipWriter := zip.NewWriter(file)
	manifest := Manifest{
		BackupSchema:  BackupSchema,
		BackupVersion: BackupVersion,
		CreatedAt:     "2026-06-17T00:00:00+08:00",
		FilesSHA256:   map[string]string{configMember: sha256Bytes(configData)},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, writeZipMember(zipWriter, manifestMember, append(manifestData, '\n')))
	require.NoError(t, writeZipMember(zipWriter, configMember, configData))
	require.NoError(t, zipWriter.Close())
}

// TestWriteNativeBackupIgnoresPlantedTempSymlink 验证导出不会跟随预埋在可预测临时路径上的软链接。
//
// 输出目录（默认 publish/）对 proxystack 组可写，固定的 "<output>.tmp" 又是可预测路径，
// 一旦 O_CREATE|O_TRUNC 跟随软链接，组成员就能让 root 执行的导出把字节写穿到任意文件。
func TestWriteNativeBackupIgnoresPlantedTempSymlink(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	stacksDir := filepath.Join(baseDir, "stacks")
	require.NoError(t, os.MkdirAll(stacksDir, 0o750))
	require.NoError(t, os.WriteFile(configPath, minimalBackupConfig(), 0o640))

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "proxystack-backup.zip")
	victimPath := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victimPath, []byte("do not touch"), 0o600))
	require.NoError(t, os.Symlink(victimPath, outputPath+".tmp"))

	_, err := WriteNativeBackup(outputPath, configPath, stacksDir, "2026-08-27T12:00:00+08:00")

	require.NoError(t, err)
	victim, readErr := os.ReadFile(victimPath)
	require.NoError(t, readErr)
	require.Equal(t, "do not touch", string(victim), "预埋的软链接目标不应被写入")
	require.FileExists(t, outputPath)
}

// minimalBackupConfig 返回可通过校验的最小 agent 配置。
func minimalBackupConfig() []byte {
	return []byte(`version: 1
paths:
  stacks: stacks
port_ranges:
  xray_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
`)
}
