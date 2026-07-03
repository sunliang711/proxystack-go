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

// StackCandidate 保存 add/clone 生成但尚未落盘的 stack 内容。
type StackCandidate struct {
	Name string
	Path string
	Data []byte
	Mode os.FileMode
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
	XrayEndpoint string
	ClashEndpoint  string
}

// InitProject 创建默认 agent 配置和标准目录。
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
	defaultUserUUID, err := randomUUID()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(configPath, []byte(defaultAgentConfig(options.ExternalHost, defaultUserUUID)), 0o640); err != nil {
		return err
	}
	return nil
}

// EnsureProjectLayout 幂等创建标准目录。
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
		filepath.Dir(configPath),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
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
	candidate, err := BuildAddStackCandidate(options)
	if err != nil {
		return err
	}
	return writeFileAtomic(candidate.Path, candidate.Data, candidate.Mode)
}

// BuildAddStackCandidate 基于模板生成 add 的候选 stack 内容，供交互编辑前预览和校验。
func BuildAddStackCandidate(options AddOptions) (StackCandidate, error) {
	return buildAddStackCandidate(options, true)
}

// BuildAddStackDraftCandidate 基于模板生成可编辑草稿，允许初始模板等待用户补齐后再校验。
func BuildAddStackDraftCandidate(options AddOptions) (StackCandidate, error) {
	return buildAddStackCandidate(options, false)
}

// buildAddStackCandidate 统一生成 add 候选内容，并按调用场景决定是否立即执行完整校验。
func buildAddStackCandidate(options AddOptions, validate bool) (StackCandidate, error) {
	cfg, stackSet, err := loadConfigAndStacks(options.ConfigPath)
	if err != nil {
		return StackCandidate{}, err
	}
	if _, ok := stackSet.ByName()[options.Name]; ok {
		return StackCandidate{}, fmt.Errorf("stack already exists: %s", options.Name)
	}
	document, err := buildStackDocument(options)
	if err != nil {
		return StackCandidate{}, err
	}
	if options.FromFile == "" {
		if err := applyConfigUserProfileToTemplateRefs(document.root, cfg.Users); err != nil {
			return StackCandidate{}, err
		}
	}
	if options.AllocatePorts && !options.KeepTemplatePorts {
		if err := allocateStackDocumentPorts(document, stackSet); err != nil {
			return StackCandidate{}, err
		}
	}
	if !validate {
		return rawStackCandidateFromDocument(cfg, options.Name, document, 0o640)
	}
	return stackCandidateFromDocument(cfg, stackSet, options.Name, document, 0o640)
}

// CloneStack 复制 source stack 为 target，并可只改目标 stack 端口。
func CloneStack(options CloneOptions) error {
	candidate, err := BuildCloneStackCandidate(options)
	if err != nil {
		return err
	}
	return writeFileAtomic(candidate.Path, candidate.Data, candidate.Mode)
}

// BuildCloneStackCandidate 生成 clone 的候选 stack 内容，供交互编辑前预览和校验。
func BuildCloneStackCandidate(options CloneOptions) (StackCandidate, error) {
	cfg, stackSet, document, _, err := loadConfigAndStackDocument(options.ConfigPath, options.Source)
	if err != nil {
		return StackCandidate{}, err
	}
	if _, ok := stackSet.ByName()[options.Target]; ok {
		return StackCandidate{}, fmt.Errorf("target stack already exists: %s", options.Target)
	}
	setMappingScalar(document.root, "name", options.Target)
	rewriteSelfRefsInNode(document.root, options.Source, options.Target)
	rewriteSubscriptionRemarksInNode(document.root, options.Source, options.Target)
	if options.AllocatePorts {
		if err := allocateStackDocumentPorts(document, stackSet); err != nil {
			return StackCandidate{}, err
		}
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(filepath.Join(cfg.StacksDir(), options.Source+".yaml")); err == nil {
		mode = info.Mode().Perm()
	}
	return stackCandidateFromDocument(cfg, stackSet, options.Target, document, mode)
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
			XrayEndpoint: firstXrayEndpoint(stack),
			ClashEndpoint:  firstClashEndpoint(stack),
		})
	}
	sort.Slice(summaries, func(i int, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries, nil
}

// ListMembers 返回 auto stack 当前 xray-socks5 成员列表。
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
	for _, inbound := range stack.Xray.Inbounds {
		if inbound.Name == "relay" && inbound.Protocol == "socks5" {
			return nil
		}
	}
	return fmt.Errorf("xray socks5 inbound ref does not exist: %s.relay", member)
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

func firstXrayEndpoint(stack domain.Stack) string {
	if len(stack.Xray.Inbounds) == 0 {
		return "-"
	}
	inbound := stack.Xray.Inbounds[0]
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

func defaultAgentConfig(externalHost string, defaultUserUUID string) string {
	return fmt.Sprintf(`# default agent 配置。
# 文件路径固定为 <base-dir>/config.yaml；运行时 base dir 由 psctl --base-dir 指定。
# 修改后建议执行 psctl --base-dir <base-dir> validate 或 check 校验。

# 配置 schema 版本。当前固定为 1。
version: 1

# 路径配置。相对路径均以 <base-dir> 为基准解析。
paths:
  # psctl、pssub、xray、mihomo 等二进制所在目录。
  bin: bin
  # geoip/geosite 等地理数据目录。
  geo: geo
  # stack 配置目录；每个 stack 使用一个 <name>.yaml 文件。
  stacks: stacks
  # runtime 状态目录，保存 manifest、锁文件和运行时辅助数据。
  runtime: runtime
  # 生成后的 xray/mihomo 配置目录。
  generated: runtime/generated
  # 订阅包和备份包输出目录。
  publish: publish
  # install/update 下载缓存目录。
  downloads: downloads

# 对外访问主机名或 IP，用于生成订阅节点 server。
external_host: %s

# 订阅导出设置。
subscription:
  # source 标识导出来源；local 表示本机 agent 生成。
  source: local

# 全局订阅用户档案。stack 的 xray.inbounds[].user_refs 会引用这里的 user/profile。
# VMess/Shadowsocks 会使用这里的 uuid/password；socks5/http 的实际连接账号仍配置在 inbound.auth。
users:
  # user 是订阅入口用户；profile 用来区分同一 user 的不同凭据档案，默认 default。
  - user: user1
    profile: default
    # VMess inbound 引用该用户时使用 uuid。
    uuid: %s
    # Shadowsocks inbound 引用该用户时使用 password；部署前请替换。
    password: change-me-user1-password
    # 订阅节点默认备注；单个 inbound 可通过 user_refs[].remark 覆盖。
    remark: user1
    # 节点展示名模板支持 .stack/.inbound/.protocol/.port/.user/.profile/.remark。
    # display_template: '{{ .stack }} {{ .inbound }} {{ .protocol }} {{ .remark }}'

# 自动分配端口范围。仅 add/clone --allocate-ports 使用；手工配置端口可在范围外。
port_ranges:
  # xray inbound 端口分配范围。
  xray_inbound: 4300-4399
  # mihomo socks listener 端口分配范围。
  clash_socks: 7001-7101
  # mihomo http listener 端口分配范围。
  clash_http: 7201-7301
  # Xray API 端口分配范围。
  xray_api_range: 10001-10999
  # mihomo external-controller 端口分配范围。
  clash_controller: 19000-19999

# stack 默认值；具体 stack 未显式配置时继承这里的设置。
defaults:
  # mihomo 默认配置。
  clash:
    # 代理模式，支持 Rule、Global、Direct。
    mode: Rule
    # 内置规则模板名称。
    rule_profile: default
  # Xray/xray 默认配置。
  xray:
    # Xray 日志级别，支持 debug、info、warning、error、none。
    loglevel: warning
    # Xray API 默认配置，用于 stats 查询等内部能力。
    api:
      # 是否默认启用 Xray API。
      enabled: true
      # API 出站 tag。
      tag: api
      # API 默认监听地址；新增 stack 可按需自动分配端口。
      listen: 127.0.0.1:10085
      # 启用的 API 服务列表。
      services: [StatsService]
    # Xray stats 默认配置。
    stats:
      # 是否默认启用 stats。
      enabled: true
    # Xray policy 默认配置。
    policy:
      # 是否默认生成 policy。
      enabled: true

# 安全策略。默认禁止公开 noauth socks/http。
security:
  # socks/http 入站监听非 loopback 地址时必须配置认证。
  require_auth_for_public_socks_http: true
  # 是否允许显式放行公开 noauth；生产环境不建议开启。
  allow_noauth_public: false

# 核心组件安装来源配置。
install:
  # mihomo 安装配置。
  mihomo:
    # latest 表示使用最新 release。
    version: latest
    # auto 表示由安装器自动选择下载来源。
    source: auto
  # xray-core 安装配置。
  xray:
    # latest 表示使用最新 release。
    version: latest
    # auto 表示由安装器自动选择下载来源。
    source: auto
  # geo 数据安装配置。
  geo:
    # latest 表示使用最新 release。
    version: latest
    # auto 表示由安装器自动选择下载来源。
    source: auto
`, externalHost, defaultUserUUID)
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
