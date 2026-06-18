package config

import (
	"fmt"
	"net/url"

	"github.com/eagle/proxystack-go/internal/domain"
	"gopkg.in/yaml.v3"
)

// SubServerConfig 保存 ps-sub 自身运行配置，使用 strict decode 防止误写字段。
type SubServerConfig struct {
	DataDir       string        `json:"-" yaml:"-" mapstructure:"-"`
	Listen        string        `json:"listen" yaml:"listen" mapstructure:"listen"`
	Log           LogConfig     `json:"log" yaml:"log" mapstructure:"log"`
	Access        AccessConfig  `json:"access" yaml:"access" mapstructure:"access"`
	TemplatesDir  string        `json:"templates_dir" yaml:"templates_dir" mapstructure:"templates_dir"`
	WatchInterval float64       `json:"watch_interval" yaml:"watch_interval" mapstructure:"watch_interval"`
	WatchDebounce float64       `json:"watch_debounce" yaml:"watch_debounce" mapstructure:"watch_debounce"`
	ManagedConfig ManagedConfig `json:"managed_config" yaml:"managed_config" mapstructure:"managed_config"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// DefaultSubServerConfigYAML 返回 ps-sub init 与 ps-agent init 共用的默认订阅服务配置。
func DefaultSubServerConfigYAML() string {
	return `# ps-sub 订阅服务配置。
# 文件路径固定为 <base-dir>/sub/config.yaml，运行数据目录固定为 <base-dir>/sub。
# 修改后可执行 ps-sub --base-dir <base-dir> config check 校验。

# HTTP 监听地址，格式为 host:port。
# Docker 或公网暴露通常使用 0.0.0.0:3003；只允许本机访问时可改为 127.0.0.1:3003。
listen: 0.0.0.0:3003

# 日志输出设置。
# format: json 输出结构化 JSON，适合 systemd/docker 收集；format: console 输出人类可读文本，适合前台排查。
log:
  format: json

# HTTP 访问控制。
# type: none 表示不校验 token；type: token 表示订阅 URL 必须携带 token。
access:
  type: none
  # token 仅在 type: token 时必填，可通过 /sub/<token>/<user> 或 ?token=... 访问。
  # token: change-me

# 自定义模板目录；为空时使用内置模板。
# 查找顺序：templates_dir/sub/<template> -> templates_dir/<template> -> <base-dir>/sub/templates/sub/<template> -> 内置模板。
# templates_dir: /opt/proxystack/sub/templates

# inputs 目录扫描间隔，单位秒；用于轮询兜底检测 sub/inputs 变更。
watch_interval: 2

# 文件变化后的防抖时间，单位秒；避免一次导入触发多次 reload。
watch_debounce: 0.3

# Surge managed config 输出设置。
managed_config:
  # enabled 为 true 时，surge_sub 响应会包含 #!MANAGED-CONFIG 行。
  enabled: true
  # public_base_url 为空时按请求 Host 推导；生产环境建议填写外部访问地址，例如 https://sub.example.com。
  # public_base_url: https://sub.example.com
  # interval 是客户端刷新间隔，单位秒。
  interval: 86400
  # strict 为 true 时，Surge managed config 使用 strict 模式。
  strict: true
`
}

// UnmarshalYAML 记录 sub config 顶层显式字段，避免显式 0 被默认值覆盖。
func (s *SubServerConfig) UnmarshalYAML(value *yaml.Node) error {
	s.fields = yamlFields(value)
	if err := rejectUnknownFields(s.fields, map[string]bool{
		"listen":         true,
		"log":            true,
		"access":         true,
		"templates_dir":  true,
		"watch_interval": true,
		"watch_debounce": true,
		"managed_config": true,
	}); err != nil {
		return err
	}
	type raw SubServerConfig
	return value.Decode((*raw)(s))
}

// ApplyDefaults 补齐 sub config 的运行默认值。
func (s *SubServerConfig) ApplyDefaults() {
	if s.Listen == "" {
		s.Listen = "0.0.0.0:3003"
	}
	if s.Access.Type == "" {
		s.Access.Type = "none"
	}
	s.Log.ApplyDefaults()
	if !s.fields["watch_interval"] && s.WatchInterval == 0 {
		s.WatchInterval = 2.0
	}
	if !s.fields["watch_debounce"] && s.WatchDebounce == 0 {
		s.WatchDebounce = 0.3
	}
	s.ManagedConfig.ApplyDefaults()
	if s.ManagedConfig.Enabled == nil {
		s.ManagedConfig.Enabled = boolPtr(true)
	}
	if s.ManagedConfig.Strict == nil {
		s.ManagedConfig.Strict = boolPtr(true)
	}
}

// Validate 校验 sub config 的运行期安全约束和基础格式。
func (s SubServerConfig) Validate() error {
	if _, _, err := domain.ParseListen(s.Listen); err != nil {
		return err
	}
	if s.WatchInterval <= 0 {
		return fmt.Errorf("watch_interval must be greater than 0")
	}
	if s.WatchDebounce < 0 {
		return fmt.Errorf("watch_debounce must be greater than or equal to 0")
	}
	if err := s.Access.Validate(); err != nil {
		return err
	}
	if err := s.Log.Validate(); err != nil {
		return err
	}
	return s.ManagedConfig.Validate()
}

const (
	// LogFormatJSON 表示输出结构化 JSON 日志。
	LogFormatJSON = "json"
	// LogFormatConsole 表示输出人类可读 console 日志。
	LogFormatConsole = "console"
)

// LogConfig 保存 ps-sub 日志输出配置。
type LogConfig struct {
	Format string `json:"format" yaml:"format" mapstructure:"format"`
}

// UnmarshalYAML 记录并拒绝 log 子对象未知字段。
func (l *LogConfig) UnmarshalYAML(value *yaml.Node) error {
	fields := yamlFields(value)
	if err := rejectUnknownFields(fields, map[string]bool{
		"format": true,
	}); err != nil {
		return err
	}
	type raw LogConfig
	return value.Decode((*raw)(l))
}

// ApplyDefaults 补齐日志默认输出格式。
func (l *LogConfig) ApplyDefaults() {
	if l.Format == "" {
		l.Format = LogFormatJSON
	}
}

// Validate 校验日志输出格式。
func (l LogConfig) Validate() error {
	switch l.Format {
	case LogFormatJSON, LogFormatConsole:
		return nil
	default:
		return fmt.Errorf("log.format must be json or console")
	}
}

// AccessConfig 保存 sub HTTP 访问控制配置。
type AccessConfig struct {
	Type  string `json:"type" yaml:"type" mapstructure:"type"`
	Token string `json:"token" yaml:"token" mapstructure:"token"`
}

// UnmarshalYAML 记录并拒绝 access 子对象未知字段，保持传输模型 strict 语义。
func (a *AccessConfig) UnmarshalYAML(value *yaml.Node) error {
	fields := yamlFields(value)
	if err := rejectUnknownFields(fields, map[string]bool{
		"type":  true,
		"token": true,
	}); err != nil {
		return err
	}
	type raw AccessConfig
	return value.Decode((*raw)(a))
}

// Validate 校验访问控制类型和 token 必填规则。
func (a AccessConfig) Validate() error {
	switch a.Type {
	case "none":
		return nil
	case "token":
		if a.Token == "" {
			return fmt.Errorf("access.token is required when access.type is token")
		}
		return nil
	default:
		return fmt.Errorf("access.type must be none or token")
	}
}

// ManagedConfig 保存 managed subscription 输出配置。
type ManagedConfig struct {
	Enabled       *bool  `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	PublicBaseURL string `json:"public_base_url" yaml:"public_base_url" mapstructure:"public_base_url"`
	Interval      int    `json:"interval" yaml:"interval" mapstructure:"interval"`
	Strict        *bool  `json:"strict" yaml:"strict" mapstructure:"strict"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 记录 managed config 显式字段，避免显式 0 被默认值吞掉。
func (m *ManagedConfig) UnmarshalYAML(value *yaml.Node) error {
	m.fields = yamlFields(value)
	if err := rejectUnknownFields(m.fields, map[string]bool{
		"enabled":         true,
		"public_base_url": true,
		"interval":        true,
		"strict":          true,
	}); err != nil {
		return err
	}
	type raw ManagedConfig
	return value.Decode((*raw)(m))
}

// ApplyDefaults 补齐 managed config 默认值。
func (m *ManagedConfig) ApplyDefaults() {
	if !m.fields["interval"] && m.Interval == 0 {
		m.Interval = 86400
	}
}

// EnabledValue 返回 managed config enabled 的解引用值。
func (m ManagedConfig) EnabledValue() bool {
	return m.Enabled != nil && *m.Enabled
}

// StrictValue 返回 managed config strict 的解引用值。
func (m ManagedConfig) StrictValue() bool {
	return m.Strict != nil && *m.Strict
}

// Validate 校验 managed config 的 URL 和周期配置。
func (m ManagedConfig) Validate() error {
	if m.Interval <= 0 {
		return fmt.Errorf("managed_config.interval must be greater than 0")
	}
	if m.PublicBaseURL == "" {
		return nil
	}
	parsedURL, err := url.Parse(m.PublicBaseURL)
	if err != nil {
		return fmt.Errorf("managed_config.public_base_url is invalid: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("managed_config.public_base_url must use http or https")
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("managed_config.public_base_url host is required")
	}
	if parsedURL.User != nil {
		return fmt.Errorf("managed_config.public_base_url must not include userinfo")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fmt.Errorf("managed_config.public_base_url must not include query or fragment")
	}
	return nil
}

// SubscriptionInput 是 agent 和 sub 之间的订阅输入传输契约。
type SubscriptionInput struct {
	InputSchema  string             `json:"input_schema" yaml:"input_schema"`
	InputVersion int                `json:"input_version" yaml:"input_version"`
	Source       string             `json:"source" yaml:"source"`
	GeneratedAt  string             `json:"generated_at" yaml:"generated_at"`
	Nodes        []SubscriptionNode `json:"nodes" yaml:"nodes"`
}

// SubscriptionNode 保存单个订阅节点传输数据。
type SubscriptionNode struct {
	ID       string           `json:"id" yaml:"id"`
	User     string           `json:"user" yaml:"user"`
	Protocol string           `json:"protocol" yaml:"protocol"`
	Server   string           `json:"server" yaml:"server"`
	Port     int              `json:"port" yaml:"port"`
	Tag      string           `json:"tag" yaml:"tag"`
	Remark   string           `json:"remark" yaml:"remark"`
	Region   string           `json:"region" yaml:"region"`
	Auth     SubscriptionAuth `json:"auth" yaml:"auth"`
	Network  string           `json:"network" yaml:"network"`
	Method   string           `json:"method" yaml:"method"`
	UDP      bool             `json:"udp" yaml:"udp"`
}

// SubscriptionAuth 保存订阅节点认证数据。
type SubscriptionAuth struct {
	Type     string `json:"type" yaml:"type"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	UUID     string `json:"uuid" yaml:"uuid"`
}

func boolPtr(value bool) *bool {
	return &value
}

func yamlFields(value *yaml.Node) map[string]bool {
	fields := make(map[string]bool)
	if value == nil || value.Kind != yaml.MappingNode {
		return fields
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		fields[value.Content[index].Value] = true
	}
	return fields
}

func rejectUnknownFields(fields map[string]bool, allowed map[string]bool) error {
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("field %s not found", field)
		}
	}
	return nil
}
