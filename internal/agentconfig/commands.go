package agentconfig

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/domain/validation"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
)

const (
	defaultBaseDir = "/opt/proxystack"
)

// ErrConfigAlreadyExists 表示默认 init 不允许覆盖既有全局配置。
var ErrConfigAlreadyExists = errors.New("config already exists")

// InitOptions 保存 init 命令创建默认目录和配置所需的输入。
type InitOptions struct {
	BaseDir      string
	ExternalHost string
	Force        bool
}

// AddOptions 保存 add 命令创建 stack 模板所需的输入。
type AddOptions struct {
	ConfigPath        string
	Name              string
	Template          string
	FromFile          string
	Members           []string
	AllocatePorts     bool
	KeepTemplatePorts bool
}

// CloneOptions 保存 clone 命令复制 stack 所需的输入。
type CloneOptions struct {
	ConfigPath    string
	Source        string
	Target        string
	AllocatePorts bool
}

// MemberOptions 保存 member add/remove/list 命令的输入。
type MemberOptions struct {
	ConfigPath string
	Stack      string
	Member     string
}

// RemoveOptions 保存 remove 命令删除或归档 stack 的输入。
type RemoveOptions struct {
	ConfigPath string
	Name       string
	Purge      bool
}

// StackSummary 是 list 命令输出用的轻量 stack 描述。
type StackSummary struct {
	Name           string
	Enabled        bool
	Role           string
	XrelayEndpoint string
	ClashEndpoint  string
}

// InitProject 创建默认 agent 配置、sub 配置和标准目录。
func InitProject(options InitOptions) error {
	baseDir, err := normalizeBaseDir(options.BaseDir)
	if err != nil {
		return err
	}
	configPath := filepath.Join(baseDir, "config.yaml")
	if _, err := os.Stat(configPath); err == nil && !options.Force {
		return fmt.Errorf("%w: %s", ErrConfigAlreadyExists, configPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := EnsureProjectLayout(options); err != nil {
		return err
	}
	if err := writeFileAtomic(configPath, []byte(defaultAgentConfig(options.ExternalHost)), 0o640); err != nil {
		return err
	}
	return nil
}

// EnsureProjectLayout 幂等创建标准目录，并在缺失时补齐 sub 默认配置。
func EnsureProjectLayout(options InitOptions) error {
	baseDir, err := normalizeBaseDir(options.BaseDir)
	if err != nil {
		return err
	}
	configPath := filepath.Join(baseDir, "config.yaml")
	dirs := []string{
		baseDir,
		filepath.Join(baseDir, "bin"),
		filepath.Join(baseDir, "geo"),
		filepath.Join(baseDir, "stacks"),
		filepath.Join(baseDir, "runtime"),
		filepath.Join(baseDir, "runtime", "generated"),
		filepath.Join(baseDir, "publish"),
		filepath.Join(baseDir, "downloads"),
		filepath.Join(baseDir, "sub"),
		filepath.Dir(configPath),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	subConfigPath := filepath.Join(baseDir, "sub", "config.yaml")
	if _, err := os.Stat(subConfigPath); os.IsNotExist(err) || options.Force {
		return writeFileAtomic(subConfigPath, []byte(config.DefaultSubServerConfigYAML()), 0o640)
	} else if err != nil {
		return err
	}
	return nil
}

// normalizeBaseDir 解析 base dir，未指定时使用默认安装目录。
func normalizeBaseDir(baseDir string) (string, error) {
	baseDir = firstNonEmpty(baseDir, defaultBaseDir)
	if !filepath.IsAbs(baseDir) {
		abs, err := filepath.Abs(baseDir)
		if err != nil {
			return "", err
		}
		baseDir = abs
	}
	return baseDir, nil
}

// AddStack 基于内置模板或外部文件创建新的 stack 文件。
func AddStack(options AddOptions) error {
	cfg, stackSet, err := loadConfigAndStacks(options.ConfigPath)
	if err != nil {
		return err
	}
	if _, ok := stackSet.ByName()[options.Name]; ok {
		return fmt.Errorf("stack already exists: %s", options.Name)
	}
	document, err := buildStackDocument(options)
	if err != nil {
		return err
	}
	if options.AllocatePorts && !options.KeepTemplatePorts {
		if err := allocateStackDocumentPorts(document, stackSet); err != nil {
			return err
		}
	}
	return writeNewStackDocument(cfg, stackSet, options.Name, document)
}

// CloneStack 复制 source stack 为 target，并可只改目标 stack 端口。
func CloneStack(options CloneOptions) error {
	cfg, stackSet, document, _, err := loadConfigAndStackDocument(options.ConfigPath, options.Source)
	if err != nil {
		return err
	}
	if _, ok := stackSet.ByName()[options.Target]; ok {
		return fmt.Errorf("target stack already exists: %s", options.Target)
	}
	setMappingScalar(document.root, "name", options.Target)
	rewriteSelfRefsInNode(document.root, options.Source, options.Target)
	if options.AllocatePorts {
		if err := allocateStackDocumentPorts(document, stackSet); err != nil {
			return err
		}
	}
	return writeNewStackDocument(cfg, stackSet, options.Target, document)
}

// ListStacks 返回稳定排序后的 stack 摘要。
func ListStacks(configPath string) ([]StackSummary, error) {
	_, stackSet, err := loadConfigAndStacks(configPath)
	if err != nil {
		return nil, err
	}
	summaries := make([]StackSummary, 0, len(stackSet.Stacks))
	for _, stack := range stackSet.Stacks {
		summaries = append(summaries, StackSummary{
			Name:           stack.Name,
			Enabled:        stack.Enabled,
			Role:           stack.Role,
			XrelayEndpoint: firstXrelayEndpoint(stack),
			ClashEndpoint:  firstClashEndpoint(stack),
		})
	}
	sort.Slice(summaries, func(i int, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries, nil
}

// ListMembers 返回 auto stack 当前 xrelay-socks5 成员列表。
func ListMembers(options MemberOptions) ([]string, error) {
	_, _, document, _, err := loadAutoStackDocument(options.ConfigPath, options.Stack)
	if err != nil {
		return nil, err
	}
	return membersFromDocument(document.root), nil
}

// AddMember 把 member stack 的 relay inbound 加入 auto stack。
func AddMember(options MemberOptions) error {
	cfg, stackSet, document, stack, err := loadAutoStackDocument(options.ConfigPath, options.Stack)
	if err != nil {
		return err
	}
	if err := ensureRelayMember(stackSet, options.Member); err != nil {
		return err
	}
	if err := addMemberToDocument(document.root, options.Member); err != nil {
		return err
	}
	return writeExistingStackDocument(cfg, stackSet, stack.Name, stack.SourcePath, document)
}

// RemoveMember 从 auto stack 中移除 member upstream 和 group 引用。
func RemoveMember(options MemberOptions) error {
	cfg, stackSet, document, stack, err := loadAutoStackDocument(options.ConfigPath, options.Stack)
	if err != nil {
		return err
	}
	if err := removeMemberFromDocument(document.root, options.Member); err != nil {
		return err
	}
	return writeExistingStackDocument(cfg, stackSet, stack.Name, stack.SourcePath, document)
}

// RemoveStack 归档 stack 文件；purge 时同时清理 manifest 中对应生成文件。
func RemoveStack(options RemoveOptions) error {
	cfg, stackSet, err := loadConfigAndStacks(options.ConfigPath)
	if err != nil {
		return err
	}
	stack, ok := stackSet.ByName()[options.Name]
	if !ok {
		return fmt.Errorf("stack does not exist: %s", options.Name)
	}
	archivePath := stack.SourcePath + ".removed"
	if err := os.Rename(stack.SourcePath, archivePath); err != nil {
		return err
	}
	if options.Purge {
		return purgeManifestFiles(cfg, options.Name)
	}
	return nil
}

func loadConfigAndStacks(configPath string) (domain.GlobalConfig, domain.StackSet, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, err
	}
	return cfg, stackSet, nil
}

func usedPorts(stackSet domain.StackSet) map[int]bool {
	ports := map[int]bool{}
	for _, binding := range validation.CollectPortBindings(stackSet) {
		ports[binding.Port] = true
	}
	return ports
}

func replaceStack(stacks []domain.Stack, stack domain.Stack) []domain.Stack {
	replaced := make([]domain.Stack, 0, len(stacks))
	for _, current := range stacks {
		if current.Name == stack.Name {
			replaced = append(replaced, stack)
			continue
		}
		replaced = append(replaced, current)
	}
	return replaced
}

func ensureRelayMember(stackSet domain.StackSet, member string) error {
	stack, ok := stackSet.ByName()[member]
	if !ok {
		return fmt.Errorf("member stack does not exist: %s", member)
	}
	for _, inbound := range stack.Xrelay.Inbounds {
		if inbound.Name == "relay" && inbound.Protocol == "socks5" {
			return nil
		}
	}
	return fmt.Errorf("xrelay socks5 inbound ref does not exist: %s.relay", member)
}

func purgeManifestFiles(cfg domain.GlobalConfig, stackName string) error {
	manifestPath := agentruntime.ManifestPath(cfg)
	manifest, err := agentruntime.ReadManifest(manifestPath)
	if err != nil || manifest == nil {
		return err
	}
	files := make([]agentruntime.ManifestFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if strings.Contains(file.Service, "@"+stackName+".service") {
			if err := os.Remove(file.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		files = append(files, file)
	}
	manifest.Files = files
	data, err := json.MarshalIndent(*manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(manifestPath, append(data, '\n'), 0o640)
}

func firstXrelayEndpoint(stack domain.Stack) string {
	if len(stack.Xrelay.Inbounds) == 0 {
		return "-"
	}
	inbound := stack.Xrelay.Inbounds[0]
	return fmt.Sprintf("%s:%d", inbound.Listen, inbound.Port)
}

func firstClashEndpoint(stack domain.Stack) string {
	if len(stack.Clash.Listeners.Socks) == 0 {
		return "-"
	}
	listener := stack.Clash.Listeners.Socks[0]
	return fmt.Sprintf("%s:%d", listener.Listen, listener.Port)
}

func randomUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func defaultAgentConfig(externalHost string) string {
	return fmt.Sprintf(`version: 1
paths:
  bin: bin
  geo: geo
  stacks: stacks
  runtime: runtime
  generated: runtime/generated
  publish: publish
  downloads: downloads
  sub: sub
external_host: %s
subscription:
  source: local
port_ranges:
  xrelay_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
defaults:
  clash:
    mode: Rule
    rule_profile: default
  xrelay:
    loglevel: warning
    api:
      enabled: true
      tag: api
      listen: 127.0.0.1:10085
      services: [StatsService]
    stats:
      enabled: true
    policy:
      enabled: true
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
`, externalHost)
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
