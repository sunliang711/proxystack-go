package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/domain/validation"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigPath 是 agent 全局配置默认路径。
	DefaultConfigPath = "/opt/proxystack/config.yaml"
)

// LoadYAMLMapping 读取 YAML 文件并确保顶层是 mapping。
func LoadYAMLMapping(path string, label string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %s (%w)", label, path, err)
	}
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("%s contains invalid YAML: %s\n%w", label, path, err)
	}
	if value == nil {
		return map[string]any{}, nil
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping: %s", label, path)
	}
	return mapping, nil
}

// LoadConfig 读取全局配置文件并构造强类型模型。
func LoadConfig(path string) (domain.GlobalConfig, error) {
	return LoadConfigWithBaseDir(path, "")
}

// LoadConfigWithBaseDir 读取全局配置文件，并用指定 baseDir 覆盖默认运行目录。
func LoadConfigWithBaseDir(path string, baseDir string) (domain.GlobalConfig, error) {
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.GlobalConfig{}, fmt.Errorf("Config file could not be read: %s (%w)", path, err)
	}
	var config domain.GlobalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return domain.GlobalConfig{}, fmt.Errorf("Config file contains invalid YAML: %s\n%w", path, err)
	}
	config.BaseDir = firstNonEmpty(baseDir, filepath.Dir(path))
	if err := config.Validate(); err != nil {
		return domain.GlobalConfig{}, err
	}
	config.ConfigPath = path
	return config, nil
}

// LoadStack 读取单个 stack 文件并执行文件内校验。
func LoadStack(path string) (domain.Stack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Stack{}, fmt.Errorf("Stack file could not be read: %s (%w)", path, err)
	}
	var stack domain.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return domain.Stack{}, fmt.Errorf("Stack file contains invalid YAML: %s\n%w", path, err)
	}
	stack.SourcePath = path
	if err := stack.Validate(); err != nil {
		return domain.Stack{}, err
	}
	if filepath.Base(path) != "" && filepath.Ext(path) != "" {
		expectedName := stringsTrimSuffix(filepath.Base(path), filepath.Ext(path))
		if expectedName != stack.Name {
			return domain.Stack{}, validation.ConfigValidationError{
				Issues: []validation.Issue{{
					Path:    fmt.Sprintf("stacks.%s.name", filepath.Base(path)),
					Message: fmt.Sprintf("stack name must match file name: %s", expectedName),
				}},
			}
		}
	}
	return stack, nil
}

// LoadStacks 读取全局配置指定目录下的所有 stack 并执行跨文件校验。
func LoadStacks(config domain.GlobalConfig, checkSystemPorts bool) (domain.StackSet, error) {
	pattern := filepath.Join(config.StacksDir(), "*.yaml")
	stackPaths, err := filepath.Glob(pattern)
	if err != nil {
		return domain.StackSet{}, fmt.Errorf("invalid stacks glob: %w", err)
	}
	sort.Strings(stackPaths)
	stacks := make([]domain.Stack, 0, len(stackPaths))
	for _, stackPath := range stackPaths {
		stack, err := LoadStack(stackPath)
		if err != nil {
			return domain.StackSet{}, err
		}
		stacks = append(stacks, stack)
	}
	stackSet := domain.StackSet{Config: config, Stacks: stacks}
	options := []validation.Option{}
	if !checkSystemPorts {
		options = append(options, validation.WithPortChecker(validation.NoopPortChecker{}))
	}
	if err := validation.ValidateStackSet(stackSet, options...); err != nil {
		return domain.StackSet{}, err
	}
	return stackSet, nil
}

// DecodeStrictYAML 解码传输契约模型，并拒绝未知字段。
func DecodeStrictYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

// LoadSubServerConfig 读取 ps-sub 自身配置，未知字段会 fail fast。
func LoadSubServerConfig(path string) (SubServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SubServerConfig{}, fmt.Errorf("Sub config could not be read: %s (%w)", path, err)
	}
	var config SubServerConfig
	if err := DecodeStrictYAML(data, &config); err != nil {
		return SubServerConfig{}, fmt.Errorf("Sub config contains invalid YAML: %s\n%w", path, err)
	}
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return SubServerConfig{}, err
	}
	return config, nil
}

func stringsTrimSuffix(value string, suffix string) string {
	if suffix == "" || len(value) < len(suffix) {
		return value
	}
	return value[:len(value)-len(suffix)]
}

// firstNonEmpty 返回第一个非空字符串，用于合并显式参数和默认值。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
