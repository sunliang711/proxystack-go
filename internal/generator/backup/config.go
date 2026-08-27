package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	BackupSchema  = "proxystack.native-backup"
	BackupVersion = 1

	manifestMember = "manifest.json"
	configMember   = "config/config.yaml"
)

// Error 表示原生配置备份包生成或校验失败。
type Error struct {
	Message string
}

// Error 返回错误原因。
func (e Error) Error() string {
	return e.Message
}

// Manifest 是原生 agent 配置备份包 manifest。
type Manifest struct {
	BackupSchema  string            `json:"backup_schema"`
	BackupVersion int               `json:"backup_version"`
	CreatedAt     string            `json:"created_at"`
	FilesSHA256   map[string]string `json:"files_sha256"`
}

// Validate 校验 native backup manifest。
func (m Manifest) Validate() error {
	if m.BackupSchema != BackupSchema {
		return fmt.Errorf("unsupported native backup schema: %s", m.BackupSchema)
	}
	if m.BackupVersion != BackupVersion {
		return fmt.Errorf("unsupported native backup version: %d", m.BackupVersion)
	}
	if m.CreatedAt == "" {
		return fmt.Errorf("created_at is required")
	}
	if _, ok := m.FilesSHA256[configMember]; !ok {
		return fmt.Errorf("native backup config is missing")
	}
	for name, digest := range m.FilesSHA256 {
		if err := ValidateFileMemberName(name); err != nil {
			return err
		}
		if !isSHA256Hex(digest) {
			return fmt.Errorf("invalid file sha256 for %s", name)
		}
	}
	return nil
}

// WriteNativeBackup 写出只包含 config.yaml 和 stacks/*.yaml 的原生备份。
func WriteNativeBackup(outputPath string, configPath string, stacksDir string, createdAt string) (Manifest, error) {
	files, err := collectBackupFiles(configPath, stacksDir)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		BackupSchema:  BackupSchema,
		BackupVersion: BackupVersion,
		CreatedAt:     createdAt,
		FilesSHA256:   map[string]string{},
	}
	for name, content := range files {
		manifest.FilesSHA256[name] = sha256Bytes(content)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return Manifest{}, err
	}
	// 用随机名 + O_EXCL 建临时文件：固定的 "<outputPath>.tmp" 是可预测路径，
	// 而 O_CREATE|O_TRUNC 会跟随软链接——在可写的输出目录里预埋一个链接，
	// 就能让这次写入（可能是 root 执行的）把字节写穿到别的文件上。
	file, err := os.CreateTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".tmp-*")
	if err != nil {
		return Manifest{}, err
	}
	tempPath := file.Name()
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return Manifest{}, err
	}
	writeErr := func() error {
		zipWriter := zip.NewWriter(file)
		manifestData, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := writeZipMember(zipWriter, manifestMember, append(manifestData, '\n')); err != nil {
			return err
		}
		names := sortedMapKeys(files)
		for _, name := range names {
			if err := writeZipMember(zipWriter, name, files[name]); err != nil {
				return err
			}
		}
		if err := zipWriter.Close(); err != nil {
			return err
		}
		return file.Sync()
	}()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return Manifest{}, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return Manifest{}, closeErr
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		_ = os.Remove(tempPath)
		return Manifest{}, err
	}
	return manifest, nil
}

// ReadNativeBackup 读取并完整校验原生备份包。
func ReadNativeBackup(path string) (Manifest, map[string][]byte, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, nil, Error{Message: "invalid native backup zip: " + path}
	}
	defer reader.Close()
	members := map[string]*zip.File{}
	for _, file := range reader.File {
		if err := validateBackupMemberSafety(file); err != nil {
			return Manifest{}, nil, err
		}
		if !file.FileInfo().IsDir() {
			members[file.Name] = file
		}
	}
	manifestFile := members[manifestMember]
	if manifestFile == nil {
		return Manifest{}, nil, Error{Message: "native backup manifest is missing"}
	}
	manifestData, err := readZipFile(manifestFile)
	if err != nil {
		return Manifest{}, nil, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, nil, Error{Message: "native backup manifest schema is invalid"}
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, Error{Message: "native backup manifest schema is invalid: " + err.Error()}
	}
	for _, file := range reader.File {
		if err := validateBackupMember(file); err != nil {
			return Manifest{}, nil, err
		}
	}
	actual := map[string]bool{}
	for name := range members {
		if name != manifestMember {
			actual[name] = true
		}
	}
	expected := map[string]bool{}
	for name := range manifest.FilesSHA256 {
		expected[name] = true
	}
	if !equalStringSets(actual, expected) {
		return Manifest{}, nil, Error{Message: "native backup files do not match manifest"}
	}
	files := map[string][]byte{}
	for name, expectedHash := range manifest.FilesSHA256 {
		content, err := readZipFile(members[name])
		if err != nil {
			return Manifest{}, nil, err
		}
		if sha256Bytes(content) != expectedHash {
			return Manifest{}, nil, Error{Message: "file hash mismatch: " + name}
		}
		files[name] = content
	}
	return manifest, files, nil
}

// RestoreNativeBackup 完整校验备份内容后，把 config 和 stacks 原子恢复到目标 baseDir。
func RestoreNativeBackup(path string, baseDir string) (Manifest, error) {
	if baseDir == "" {
		return Manifest{}, Error{Message: "restore base dir is required"}
	}
	manifest, files, err := ReadNativeBackup(path)
	if err != nil {
		return Manifest{}, err
	}
	tempDir, err := os.MkdirTemp("", "proxystack-backup-validate-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tempDir)
	validationConfig, err := stripConfigBaseDir(files[configMember])
	if err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "stacks"), 0o750); err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "config.yaml"), validationConfig, 0o640); err != nil {
		return Manifest{}, err
	}
	stackMembers := sortedStackMembers(files)
	for _, member := range stackMembers {
		if err := os.WriteFile(filepath.Join(tempDir, "stacks", filepath.Base(member)), files[member], 0o640); err != nil {
			return Manifest{}, err
		}
	}
	loadedConfig, err := config.LoadConfig(filepath.Join(tempDir, "config.yaml"))
	if err != nil {
		return Manifest{}, Error{Message: "invalid native backup config: " + err.Error()}
	}
	if _, err := config.LoadStacks(loadedConfig, false); err != nil {
		return Manifest{}, Error{Message: "invalid native backup stacks: " + err.Error()}
	}
	targetConfig, err := stripConfigBaseDir(files[configMember])
	if err != nil {
		return Manifest{}, err
	}
	if err := restoreBackupFiles(baseDir, targetConfig, stackMembers, files); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ValidateFileMemberName 校验备份文件路径属于允许集合。
func ValidateFileMemberName(name string) error {
	if name == configMember {
		return nil
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return Error{Message: "unsafe native backup path: " + name}
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == ".." {
			return Error{Message: "unsafe native backup path: " + name}
		}
	}
	if len(parts) == 2 && parts[0] == "stacks" && filepath.Ext(parts[1]) == ".yaml" && !strings.HasPrefix(parts[1], ".") && !strings.HasPrefix(parts[1], "-") {
		return nil
	}
	return Error{Message: "unexpected native backup path: " + name}
}

func collectBackupFiles(configPath string, stacksDir string) (map[string][]byte, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	configData, err = stripConfigBaseDir(configData)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{configMember: configData}
	stackPaths, err := filepath.Glob(filepath.Join(stacksDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(stackPaths)
	for _, stackPath := range stackPaths {
		name := filepath.Base(stackPath)
		content, err := os.ReadFile(stackPath)
		if err != nil {
			return nil, err
		}
		files["stacks/"+name] = content
	}
	return files, nil
}

// stripConfigBaseDir 从备份配置中移除旧版 base_dir 字段。
func stripConfigBaseDir(content []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, Error{Message: "native backup config contains invalid YAML"}
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, Error{Message: "native backup config must be a mapping"}
	}
	mapping := root.Content[0]
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == "base_dir" {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return marshalYAMLNode(&root)
		}
	}
	return marshalYAMLNode(&root)
}

func marshalYAMLNode(node *yaml.Node) ([]byte, error) {
	var builder strings.Builder
	encoder := yaml.NewEncoder(&builder)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

func sortedStackMembers(files map[string][]byte) []string {
	members := make([]string, 0)
	for name := range files {
		if strings.HasPrefix(name, "stacks/") {
			members = append(members, name)
		}
	}
	sort.Strings(members)
	return members
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	tempPath := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return writeErr
	}
	if syncErr != nil {
		_ = os.Remove(tempPath)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	return os.Rename(tempPath, path)
}

func restoreBackupFiles(baseDir string, targetConfig []byte, stackMembers []string, files map[string][]byte) error {
	if err := os.MkdirAll(baseDir, 0o770); err != nil {
		return err
	}
	stagingRoot, err := os.MkdirTemp(baseDir, ".restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingRoot)
	stagingConfigPath := filepath.Join(stagingRoot, "config.yaml")
	stagingStacksDir := filepath.Join(stagingRoot, "stacks")
	if err := writeFileAtomically(stagingConfigPath, targetConfig, 0o640); err != nil {
		return err
	}
	if err := os.MkdirAll(stagingStacksDir, 0o770); err != nil {
		return err
	}
	for _, member := range stackMembers {
		if err := writeFileAtomically(filepath.Join(stagingStacksDir, filepath.Base(member)), files[member], 0o640); err != nil {
			return err
		}
	}
	configBackupPath := filepath.Join(stagingRoot, "config.yaml.old")
	hadConfig, err := moveAsideIfExists(filepath.Join(baseDir, "config.yaml"), configBackupPath)
	if err != nil {
		return err
	}
	configInstalled := false
	if err := os.Rename(stagingConfigPath, filepath.Join(baseDir, "config.yaml")); err != nil {
		rollbackFile(filepath.Join(baseDir, "config.yaml"), configBackupPath, hadConfig)
		return err
	}
	configInstalled = true
	if err := swapDirectory(filepath.Join(baseDir, "stacks"), stagingStacksDir); err != nil {
		if configInstalled {
			rollbackFile(filepath.Join(baseDir, "config.yaml"), configBackupPath, hadConfig)
		}
		return err
	}
	if hadConfig {
		_ = os.Remove(configBackupPath)
	}
	return nil
}

func moveAsideIfExists(targetPath string, backupPath string) (bool, error) {
	if _, err := os.Stat(targetPath); err == nil {
		return true, os.Rename(targetPath, backupPath)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}

func rollbackFile(targetPath string, backupPath string, hadOriginal bool) {
	_ = os.Remove(targetPath)
	if hadOriginal {
		_ = os.Rename(backupPath, targetPath)
	}
}

func swapDirectory(targetDir string, stagingDir string) error {
	backupDir := stagingDir + ".old"
	targetExists := false
	if _, err := os.Stat(targetDir); err == nil {
		targetExists = true
		if err := os.Rename(targetDir, backupDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stagingDir, targetDir); err != nil {
		if targetExists {
			_ = os.Rename(backupDir, targetDir)
		}
		return err
	}
	if targetExists {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

func validateBackupMember(file *zip.File) error {
	name := file.Name
	if name == manifestMember {
		return nil
	}
	if file.FileInfo().IsDir() && (name == "config/" || name == "stacks/") {
		return nil
	}
	return ValidateFileMemberName(name)
}

func validateBackupMemberSafety(file *zip.File) error {
	name := file.Name
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "\x00") {
		return Error{Message: "unsafe native backup path: " + name}
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return Error{Message: "unsafe native backup path: " + name}
		}
	}
	return nil
}

func writeZipMember(zipWriter *zip.Writer, name string, content []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o640)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(content)
	return err
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func sortedMapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sha256Bytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func equalStringSets(left map[string]bool, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if !right[value] {
			return false
		}
	}
	return true
}
