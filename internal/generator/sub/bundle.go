package sub

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
)

// BundleManifest 是订阅发布包 manifest。
type BundleManifest struct {
	BundleSchema    string            `json:"bundle_schema"`
	BundleVersion   int               `json:"bundle_version"`
	Source          string            `json:"source"`
	GeneratedAt     string            `json:"generated_at"`
	InputsSHA256    map[string]string `json:"inputs_sha256"`
	TemplateVersion string            `json:"template_version"`
	Access          Access            `json:"access"`
}

// Validate 校验 bundle manifest schema、版本和 hash 格式。
func (m BundleManifest) Validate() error {
	if m.BundleSchema != BundleSchema {
		return fmt.Errorf("unsupported subscription bundle schema: %s", m.BundleSchema)
	}
	if m.BundleVersion != BundleVersion {
		return fmt.Errorf("unsupported subscription bundle version: %d", m.BundleVersion)
	}
	if m.Source == "" {
		return fmt.Errorf("bundle source is required")
	}
	if m.GeneratedAt == "" {
		return fmt.Errorf("bundle generated_at is required")
	}
	for name, digest := range m.InputsSHA256 {
		if err := ValidateBundleInputName(name); err != nil {
			return err
		}
		if !isSHA256Hex(digest) {
			return fmt.Errorf("invalid input sha256 for %s", name)
		}
	}
	if err := m.Access.Validate(); err != nil {
		return err
	}
	return nil
}

// BundleInputFile 是待写入 bundle 的 input 文件。
type BundleInputFile struct {
	Name    string
	Content []byte
}

// BundleImportResult 描述导入结果，避免调用方再扫描 zip。
type BundleImportResult struct {
	Manifest       BundleManifest
	WrittenInputs  []string
	ReplacedInputs []string
	RemovedInputs  []string
	ReplaceAll     bool
}

// WriteBundle 写入订阅发布包。
func WriteBundle(outputPath string, source string, generatedAt string, inputFiles []BundleInputFile) (BundleManifest, error) {
	if err := ensureUniqueBundleInputNames(inputFiles); err != nil {
		return BundleManifest{}, err
	}
	manifest := BundleManifest{
		BundleSchema:    BundleSchema,
		BundleVersion:   BundleVersion,
		Source:          source,
		GeneratedAt:     generatedAt,
		InputsSHA256:    map[string]string{},
		TemplateVersion: "builtin-v1",
		Access:          Access{Type: "none"},
	}
	for _, inputFile := range inputFiles {
		if err := ValidateBundleInputName(inputFile.Name); err != nil {
			return BundleManifest{}, err
		}
		if _, err := LoadInputContent(inputFile.Name, inputFile.Content); err != nil {
			return BundleManifest{}, err
		}
		manifest.InputsSHA256[inputFile.Name] = sha256Bytes(inputFile.Content)
	}
	if err := manifest.Validate(); err != nil {
		return BundleManifest{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return BundleManifest{}, err
	}
	tempPath := outputPath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return BundleManifest{}, err
	}
	writeErr := func() error {
		zipWriter := zip.NewWriter(file)
		manifestData, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := writeZipMember(zipWriter, "manifest.json", append(manifestData, '\n')); err != nil {
			return err
		}
		sort.Slice(inputFiles, func(i int, j int) bool { return inputFiles[i].Name < inputFiles[j].Name })
		for _, inputFile := range inputFiles {
			if err := writeZipMember(zipWriter, "inputs/"+inputFile.Name, inputFile.Content); err != nil {
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
		return BundleManifest{}, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return BundleManifest{}, closeErr
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		_ = os.Remove(tempPath)
		return BundleManifest{}, err
	}
	return manifest, nil
}

// ExtractBundleInputs 校验发布包并原子写入 dataDir/inputs。
func ExtractBundleInputs(bundlePath string, dataDir string, replaceAll bool) (BundleImportResult, error) {
	manifest, inputMembers, err := readAndValidateBundle(bundlePath)
	if err != nil {
		return BundleImportResult{}, err
	}
	inputDir := filepath.Join(dataDir, "inputs")
	if err := os.MkdirAll(inputDir, 0o750); err != nil {
		return BundleImportResult{}, err
	}
	existingNames := map[string]bool{}
	existingPaths, err := ScanInputFiles(inputDir)
	if err != nil {
		return BundleImportResult{}, err
	}
	for _, path := range existingPaths {
		existingNames[filepath.Base(path)] = true
	}
	loadedInputs := make([]InputFile, 0, len(inputMembers))
	for name, content := range inputMembers {
		input, err := LoadInputContent(name, content)
		if err != nil {
			return BundleImportResult{}, err
		}
		loadedInputs = append(loadedInputs, InputFile{Name: name, Input: input})
	}
	sort.Slice(loadedInputs, func(i int, j int) bool { return loadedInputs[i].Name < loadedInputs[j].Name })
	if replaceAll {
		if _, err := MergeInputs(loadedInputs, Access{Type: "none"}, manifest.GeneratedAt); err != nil {
			return BundleImportResult{}, err
		}
	} else {
		mergedInputs := make([]InputFile, 0, len(existingPaths)+len(loadedInputs))
		for _, path := range existingPaths {
			name := filepath.Base(path)
			if _, incoming := inputMembers[name]; incoming {
				continue
			}
			input, err := LoadInputFile(path)
			if err != nil {
				return BundleImportResult{}, err
			}
			mergedInputs = append(mergedInputs, InputFile{Name: name, Input: input})
		}
		mergedInputs = append(mergedInputs, loadedInputs...)
		if _, err := MergeInputs(mergedInputs, Access{Type: "none"}, manifest.GeneratedAt); err != nil {
			return BundleImportResult{}, err
		}
	}
	replaced := make([]string, 0)
	removed := make([]string, 0)
	if replaceAll {
		for _, path := range existingPaths {
			name := filepath.Base(path)
			if _, incoming := inputMembers[name]; !incoming {
				removed = append(removed, name)
			}
		}
	}
	names := make([]string, 0, len(inputMembers))
	for name := range inputMembers {
		names = append(names, name)
		if existingNames[name] {
			replaced = append(replaced, name)
		}
	}
	sort.Strings(names)
	sort.Strings(replaced)
	sort.Strings(removed)
	targetInputs := map[string][]byte{}
	if !replaceAll {
		for _, path := range existingPaths {
			name := filepath.Base(path)
			if _, incoming := inputMembers[name]; incoming {
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return BundleImportResult{}, err
			}
			targetInputs[name] = content
		}
	}
	for name, content := range inputMembers {
		targetInputs[name] = content
	}
	if err := replaceInputDirectory(inputDir, targetInputs); err != nil {
		return BundleImportResult{}, err
	}
	return BundleImportResult{
		Manifest:       manifest,
		WrittenInputs:  names,
		ReplacedInputs: replaced,
		RemovedInputs:  removed,
		ReplaceAll:     replaceAll,
	}, nil
}

func readAndValidateBundle(bundlePath string) (BundleManifest, map[string][]byte, error) {
	reader, err := zip.OpenReader(bundlePath)
	if err != nil {
		return BundleManifest{}, nil, GeneratorError{Message: "invalid subscription bundle zip: " + bundlePath}
	}
	defer reader.Close()
	members := map[string]*zip.File{}
	for _, file := range reader.File {
		if err := validateBundleMember(file); err != nil {
			return BundleManifest{}, nil, err
		}
		if !file.FileInfo().IsDir() {
			members[file.Name] = file
		}
	}
	manifestFile, ok := members["manifest.json"]
	if !ok {
		return BundleManifest{}, nil, GeneratorError{Message: "bundle manifest is missing"}
	}
	manifestData, err := readZipFile(manifestFile)
	if err != nil {
		return BundleManifest{}, nil, err
	}
	var manifest BundleManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return BundleManifest{}, nil, GeneratorError{Message: "bundle manifest schema is invalid"}
	}
	if err := manifest.Validate(); err != nil {
		return BundleManifest{}, nil, GeneratorError{Message: "bundle manifest schema is invalid: " + err.Error()}
	}
	actualInputNames := map[string]bool{}
	for memberName := range members {
		if strings.HasPrefix(memberName, "inputs/") {
			actualInputNames[filepath.Base(memberName)] = true
		}
	}
	expectedInputNames := map[string]bool{}
	for name := range manifest.InputsSHA256 {
		expectedInputNames[name] = true
	}
	if !equalStringSets(actualInputNames, expectedInputNames) {
		return BundleManifest{}, nil, GeneratorError{Message: "bundle inputs do not match manifest"}
	}
	inputMembers := map[string][]byte{}
	for name, expectedHash := range manifest.InputsSHA256 {
		file := members["inputs/"+name]
		if file == nil {
			return BundleManifest{}, nil, GeneratorError{Message: "bundle input is missing: " + name}
		}
		content, err := readZipFile(file)
		if err != nil {
			return BundleManifest{}, nil, err
		}
		if sha256Bytes(content) != expectedHash {
			return BundleManifest{}, nil, GeneratorError{Message: "input hash mismatch: " + name}
		}
		inputMembers[name] = content
	}
	return manifest, inputMembers, nil
}

// ValidateBundleInputName 校验 bundle input 文件名不能包含路径片段。
func ValidateBundleInputName(name string) error {
	if filepath.Base(name) != name || strings.Contains(name, "\\") || strings.Contains(name, "\x00") || strings.Contains(name, "/") {
		return GeneratorError{Message: "unsafe bundle input file: " + name}
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return GeneratorError{Message: "unsafe bundle input file: " + name}
		}
	}
	if !supportedInputExtensions[filepath.Ext(name)] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "-") {
		return GeneratorError{Message: "unsafe bundle input file: " + name}
	}
	return nil
}

func validateBundleMember(file *zip.File) error {
	name := file.Name
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "\x00") {
		return GeneratorError{Message: "unsafe bundle path: " + name}
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == ".." {
			return GeneratorError{Message: "unsafe bundle path: " + name}
		}
	}
	if name == "manifest.json" {
		return nil
	}
	if file.FileInfo().IsDir() && name == "inputs/" {
		return nil
	}
	if strings.HasPrefix(name, "inputs/") && len(parts) == 2 && parts[1] != "" {
		return ValidateBundleInputName(parts[1])
	}
	return GeneratorError{Message: "unexpected bundle path: " + name}
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

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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

func replaceInputDirectory(inputDir string, inputs map[string][]byte) error {
	parentDir := filepath.Dir(inputDir)
	if err := os.MkdirAll(parentDir, 0o750); err != nil {
		return err
	}
	stagingDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(inputDir)+".import-*")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	names := sortedByteMapKeys(inputs)
	for _, name := range names {
		if err := writeFileAtomically(filepath.Join(stagingDir, name), inputs[name], 0o640); err != nil {
			return err
		}
	}
	if err := swapDirectory(inputDir, stagingDir); err != nil {
		return err
	}
	committed = true
	return nil
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

func sortedByteMapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ensureUniqueBundleInputNames(inputFiles []BundleInputFile) error {
	seen := map[string]bool{}
	for _, inputFile := range inputFiles {
		if seen[inputFile.Name] {
			return GeneratorError{Message: "duplicate bundle input file: " + inputFile.Name}
		}
		seen[inputFile.Name] = true
	}
	return nil
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
