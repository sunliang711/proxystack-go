package domain

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultBaseDir           = "/opt/proxystack"
	defaultXrayLogLevel      = "warning"
	defaultClashLogLevel     = "info"
	defaultClashMode         = "Rule"
	defaultRuleProfile       = "default"
	defaultXrelayAPITag      = "api"
	defaultXrelayAPIListen   = "127.0.0.1:10085"
	defaultInstallVersion    = "latest"
	defaultInstallSource     = "auto"
	defaultStackRole         = "edge"
	defaultClashFinal        = "AllProxy"
	defaultListenHost        = "0.0.0.0"
	defaultClashListenerHost = "127.0.0.1"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	regionPattern     = regexp.MustCompile(`^[A-Z]{2}$`)
	uuidPattern       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

var shadowsocks2022KeyLengths = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

// ConfigPaths 保存全局配置中的路径字段，所有相对路径均按运行时 base dir 解析。
type ConfigPaths struct {
	Bin       string `json:"bin" yaml:"bin" mapstructure:"bin"`
	Geo       string `json:"geo" yaml:"geo" mapstructure:"geo"`
	Stacks    string `json:"stacks" yaml:"stacks" mapstructure:"stacks"`
	Runtime   string `json:"runtime" yaml:"runtime" mapstructure:"runtime"`
	Generated string `json:"generated" yaml:"generated" mapstructure:"generated"`
	Publish   string `json:"publish" yaml:"publish" mapstructure:"publish"`
	Downloads string `json:"downloads" yaml:"downloads" mapstructure:"downloads"`
	Sub       string `json:"sub" yaml:"sub" mapstructure:"sub"`
}

// DefaultConfigPaths 返回项目约定的默认目录结构。
func DefaultConfigPaths() ConfigPaths {
	return ConfigPaths{
		Bin:       "bin",
		Geo:       "geo",
		Stacks:    "stacks",
		Runtime:   "runtime",
		Generated: "runtime/generated",
		Publish:   "publish",
		Downloads: "downloads",
		Sub:       "sub",
	}
}

// SubscriptionConfig 保存订阅来源配置。
type SubscriptionConfig struct {
	Source string `json:"source" yaml:"source" mapstructure:"source"`
}

// PortRange 表示一个闭区间端口池，支持 YAML 中的 start-end 字符串写法。
type PortRange struct {
	Start int `json:"start" yaml:"start" mapstructure:"start"`
	End   int `json:"end" yaml:"end" mapstructure:"end"`
}

// UnmarshalYAML 支持端口范围从字符串或结构化对象两种格式加载。
func (p *PortRange) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		parts := strings.SplitN(value.Value, "-", 2)
		if len(parts) != 2 {
			return fmt.Errorf("port range must use start-end format")
		}
		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return fmt.Errorf("port range start must be an integer: %w", err)
		}
		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("port range end must be an integer: %w", err)
		}
		p.Start = start
		p.End = end
		return p.Validate()
	}
	type raw PortRange
	if err := value.Decode((*raw)(p)); err != nil {
		return err
	}
	return p.Validate()
}

// Validate 校验端口范围边界和顺序。
func (p PortRange) Validate() error {
	if err := ValidatePort(p.Start, "port range start"); err != nil {
		return err
	}
	if err := ValidatePort(p.End, "port range end"); err != nil {
		return err
	}
	if p.Start > p.End {
		return fmt.Errorf("port range start must be less than or equal to end")
	}
	return nil
}

// Allocate 从端口池中稳定分配指定数量的未使用端口。
func (p PortRange) Allocate(usedPorts map[int]bool, count int) ([]int, error) {
	allocated := make([]int, 0, count)
	unavailable := make(map[int]bool, len(usedPorts)+count)
	for port, used := range usedPorts {
		unavailable[port] = used
	}
	for port := p.Start; port <= p.End; port++ {
		if unavailable[port] {
			continue
		}
		allocated = append(allocated, port)
		unavailable[port] = true
		if len(allocated) == count {
			return allocated, nil
		}
	}
	return nil, fmt.Errorf("not enough available ports in range")
}

// PortRanges 保存所有自动分配端口池。
type PortRanges struct {
	XrelayInbound  PortRange `json:"xrelay_inbound" yaml:"xrelay_inbound" mapstructure:"xrelay_inbound"`
	ClashSocks     PortRange `json:"clash_socks" yaml:"clash_socks" mapstructure:"clash_socks"`
	ClashHTTP      PortRange `json:"clash_http" yaml:"clash_http" mapstructure:"clash_http"`
	XrayAPIRange   PortRange `json:"xray_api_range" yaml:"xray_api_range" mapstructure:"xray_api_range"`
	ClashControler PortRange `json:"clash_controller" yaml:"clash_controller" mapstructure:"clash_controller"`
}

// Validate 校验所有端口池均为合法范围。
func (p PortRanges) Validate() error {
	ranges := map[string]PortRange{
		"xrelay_inbound":   p.XrelayInbound,
		"clash_socks":      p.ClashSocks,
		"clash_http":       p.ClashHTTP,
		"xray_api_range":   p.XrayAPIRange,
		"clash_controller": p.ClashControler,
	}
	for name, portRange := range ranges {
		if err := portRange.Validate(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// DefaultClashConfig 保存 mihomo 默认配置。
type DefaultClashConfig struct {
	Mode        string `json:"mode" yaml:"mode" mapstructure:"mode"`
	LogLevel    string `json:"loglevel" yaml:"loglevel" mapstructure:"loglevel"`
	RuleProfile string `json:"rule_profile" yaml:"rule_profile" mapstructure:"rule_profile"`
}

// UnmarshalYAML 在加载 mihomo 默认配置时补齐兼容默认值。
func (d *DefaultClashConfig) UnmarshalYAML(value *yaml.Node) error {
	*d = DefaultClashConfig{Mode: defaultClashMode, LogLevel: defaultClashLogLevel, RuleProfile: defaultRuleProfile}
	type raw DefaultClashConfig
	return value.Decode((*raw)(d))
}

// XrelayAPIConfig 保存 Xray API 配置，并记录 YAML 中显式写入的字段。
type XrelayAPIConfig struct {
	Enabled  bool     `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Tag      string   `json:"tag" yaml:"tag" mapstructure:"tag"`
	Listen   string   `json:"listen" yaml:"listen" mapstructure:"listen"`
	Services []string `json:"services" yaml:"services" mapstructure:"services"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// DefaultXrelayAPIConfig 返回 Xray API 默认配置。
func DefaultXrelayAPIConfig() XrelayAPIConfig {
	return XrelayAPIConfig{
		Enabled:  true,
		Tag:      defaultXrelayAPITag,
		Listen:   defaultXrelayAPIListen,
		Services: []string{"StatsService"},
	}
}

// UnmarshalYAML 在加载 API 配置时补齐默认值并保留显式字段集合。
func (x *XrelayAPIConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = DefaultXrelayAPIConfig()
	x.fields = yamlFields(value)
	type raw XrelayAPIConfig
	return value.Decode((*raw)(x))
}

// XrelayStatsConfig 保存 Xray stats 开关。
type XrelayStatsConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled" mapstructure:"enabled"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 在加载 stats 配置时补齐默认启用值。
func (x *XrelayStatsConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = XrelayStatsConfig{Enabled: true, fields: yamlFields(value)}
	type raw XrelayStatsConfig
	return value.Decode((*raw)(x))
}

// XrelayPolicySystemConfig 保存 Xray system policy 统计开关。
type XrelayPolicySystemConfig struct {
	StatsInboundUplink    *bool `json:"statsInboundUplink" yaml:"statsInboundUplink" mapstructure:"statsInboundUplink"`
	StatsInboundDownlink  *bool `json:"statsInboundDownlink" yaml:"statsInboundDownlink" mapstructure:"statsInboundDownlink"`
	StatsOutboundUplink   *bool `json:"statsOutboundUplink" yaml:"statsOutboundUplink" mapstructure:"statsOutboundUplink"`
	StatsOutboundDownlink *bool `json:"statsOutboundDownlink" yaml:"statsOutboundDownlink" mapstructure:"statsOutboundDownlink"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 记录 system policy 中显式写入的统计字段。
func (x *XrelayPolicySystemConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = XrelayPolicySystemConfig{fields: yamlFields(value)}
	type raw XrelayPolicySystemConfig
	return value.Decode((*raw)(x))
}

// XrelayPolicyLevelConfig 保存 Xray level policy 用户统计开关。
type XrelayPolicyLevelConfig struct {
	StatsUserUplink   *bool `json:"statsUserUplink" yaml:"statsUserUplink" mapstructure:"statsUserUplink"`
	StatsUserDownlink *bool `json:"statsUserDownlink" yaml:"statsUserDownlink" mapstructure:"statsUserDownlink"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 记录 level policy 中显式写入的统计字段。
func (x *XrelayPolicyLevelConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = XrelayPolicyLevelConfig{fields: yamlFields(value)}
	type raw XrelayPolicyLevelConfig
	return value.Decode((*raw)(x))
}

// XrelayPolicyConfig 保存 Xray policy 配置。
type XrelayPolicyConfig struct {
	Enabled bool                               `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Levels  map[string]XrelayPolicyLevelConfig `json:"levels" yaml:"levels" mapstructure:"levels"`
	System  XrelayPolicySystemConfig           `json:"system" yaml:"system" mapstructure:"system"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// DefaultXrelayPolicyConfig 返回默认 Xray policy 配置。
func DefaultXrelayPolicyConfig() XrelayPolicyConfig {
	yes := true
	return XrelayPolicyConfig{
		Enabled: true,
		Levels: map[string]XrelayPolicyLevelConfig{
			"0": {
				StatsUserUplink:   &yes,
				StatsUserDownlink: &yes,
			},
		},
		System: XrelayPolicySystemConfig{},
	}
}

// UnmarshalYAML 在加载 policy 配置时补齐默认 levels 并记录显式字段。
func (x *XrelayPolicyConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = DefaultXrelayPolicyConfig()
	x.fields = yamlFields(value)
	type raw XrelayPolicyConfig
	return value.Decode((*raw)(x))
}

// DefaultXrelayConfig 保存 xrelay 默认值。
type DefaultXrelayConfig struct {
	LogLevel string             `json:"loglevel" yaml:"loglevel" mapstructure:"loglevel"`
	API      XrelayAPIConfig    `json:"api" yaml:"api" mapstructure:"api"`
	Stats    XrelayStatsConfig  `json:"stats" yaml:"stats" mapstructure:"stats"`
	Policy   XrelayPolicyConfig `json:"policy" yaml:"policy" mapstructure:"policy"`
}

// UnmarshalYAML 在加载 xrelay 默认配置时补齐默认值。
func (d *DefaultXrelayConfig) UnmarshalYAML(value *yaml.Node) error {
	*d = DefaultXrelayConfig{
		LogLevel: defaultXrayLogLevel,
		API:      DefaultXrelayAPIConfig(),
		Stats:    XrelayStatsConfig{Enabled: true},
		Policy:   DefaultXrelayPolicyConfig(),
	}
	type raw DefaultXrelayConfig
	return value.Decode((*raw)(d))
}

// DefaultsConfig 保存全局默认值配置。
type DefaultsConfig struct {
	Clash  DefaultClashConfig  `json:"clash" yaml:"clash" mapstructure:"clash"`
	Xrelay DefaultXrelayConfig `json:"xrelay" yaml:"xrelay" mapstructure:"xrelay"`
}

// UnmarshalYAML 在加载全局默认值时补齐旧配置兼容默认。
func (d *DefaultsConfig) UnmarshalYAML(value *yaml.Node) error {
	*d = DefaultDefaultsConfig()
	type raw DefaultsConfig
	return value.Decode((*raw)(d))
}

// DefaultDefaultsConfig 返回完整默认值配置。
func DefaultDefaultsConfig() DefaultsConfig {
	return DefaultsConfig{
		Clash: DefaultClashConfig{
			Mode:        defaultClashMode,
			LogLevel:    defaultClashLogLevel,
			RuleProfile: defaultRuleProfile,
		},
		Xrelay: DefaultXrelayConfig{
			LogLevel: defaultXrayLogLevel,
			API:      DefaultXrelayAPIConfig(),
			Stats:    XrelayStatsConfig{Enabled: true},
			Policy:   DefaultXrelayPolicyConfig(),
		},
	}
}

// SecurityConfig 保存安全校验开关。
type SecurityConfig struct {
	RequireAuthForPublicSocksHTTP bool `json:"require_auth_for_public_socks_http" yaml:"require_auth_for_public_socks_http" mapstructure:"require_auth_for_public_socks_http"`
	AllowNoAuthPublic             bool `json:"allow_noauth_public" yaml:"allow_noauth_public" mapstructure:"allow_noauth_public"`
}

// UnmarshalYAML 在加载安全配置时补齐默认拒绝公开 noauth。
func (s *SecurityConfig) UnmarshalYAML(value *yaml.Node) error {
	*s = SecurityConfig{RequireAuthForPublicSocksHTTP: true}
	type raw SecurityConfig
	return value.Decode((*raw)(s))
}

// InstallToolConfig 保存单个托管文件的安装来源配置。
type InstallToolConfig struct {
	Version       string `json:"version" yaml:"version" mapstructure:"version"`
	Source        string `json:"source" yaml:"source" mapstructure:"source"`
	SHA256        string `json:"sha256" yaml:"sha256" mapstructure:"sha256"`
	ArchiveMember string `json:"archive_member" yaml:"archive_member" mapstructure:"archive_member"`
}

// UnmarshalYAML 在加载安装配置时补齐默认版本和来源。
func (i *InstallToolConfig) UnmarshalYAML(value *yaml.Node) error {
	*i = InstallToolConfig{Version: defaultInstallVersion, Source: defaultInstallSource}
	type raw InstallToolConfig
	return value.Decode((*raw)(i))
}

// InstallConfig 保存所有托管目标的安装配置。
type InstallConfig struct {
	Mihomo InstallToolConfig `json:"mihomo" yaml:"mihomo" mapstructure:"mihomo"`
	Xray   InstallToolConfig `json:"xray" yaml:"xray" mapstructure:"xray"`
	Geo    InstallToolConfig `json:"geo" yaml:"geo" mapstructure:"geo"`
}

// UnmarshalYAML 在加载 install 配置时补齐每个目标的默认值。
func (i *InstallConfig) UnmarshalYAML(value *yaml.Node) error {
	defaultTool := InstallToolConfig{Version: defaultInstallVersion, Source: defaultInstallSource}
	*i = InstallConfig{Mihomo: defaultTool, Xray: defaultTool, Geo: defaultTool}
	type raw InstallConfig
	return value.Decode((*raw)(i))
}

// GlobalConfig 对应 agent 的 config.yaml。
type GlobalConfig struct {
	Version      int                `json:"version" yaml:"version" mapstructure:"version"`
	BaseDir      string             `json:"-" yaml:"-" mapstructure:"-"`
	Paths        ConfigPaths        `json:"paths" yaml:"paths" mapstructure:"paths"`
	ExternalHost string             `json:"external_host" yaml:"external_host" mapstructure:"external_host"`
	Subscription SubscriptionConfig `json:"subscription" yaml:"subscription" mapstructure:"subscription"`
	PortRanges   PortRanges         `json:"port_ranges" yaml:"port_ranges" mapstructure:"port_ranges"`
	Defaults     DefaultsConfig     `json:"defaults" yaml:"defaults" mapstructure:"defaults"`
	Security     SecurityConfig     `json:"security" yaml:"security" mapstructure:"security"`
	Install      InstallConfig      `json:"install" yaml:"install" mapstructure:"install"`
	ConfigPath   string             `json:"-" yaml:"-"`
}

// UnmarshalYAML 在加载全局配置时补齐默认路径、默认值和安全配置。
func (g *GlobalConfig) UnmarshalYAML(value *yaml.Node) error {
	*g = GlobalConfig{
		Version:      1,
		Paths:        DefaultConfigPaths(),
		Subscription: SubscriptionConfig{Source: "local"},
		Defaults:     DefaultDefaultsConfig(),
		Security:     SecurityConfig{RequireAuthForPublicSocksHTTP: true},
		Install: InstallConfig{
			Mihomo: InstallToolConfig{Version: defaultInstallVersion, Source: defaultInstallSource},
			Xray:   InstallToolConfig{Version: defaultInstallVersion, Source: defaultInstallSource},
			Geo:    InstallToolConfig{Version: defaultInstallVersion, Source: defaultInstallSource},
		},
	}
	type raw GlobalConfig
	return value.Decode((*raw)(g))
}

// ResolvePath 将配置路径解析为绝对路径。
func (g GlobalConfig) ResolvePath(pathValue string) string {
	if filepath.IsAbs(pathValue) {
		return filepath.Clean(pathValue)
	}
	return filepath.Clean(filepath.Join(g.BaseDir, pathValue))
}

// StacksDir 返回 stack 配置目录。
func (g GlobalConfig) StacksDir() string {
	return g.ResolvePath(g.Paths.Stacks)
}

// Validate 校验全局配置字段和默认值枚举。
func (g *GlobalConfig) Validate() error {
	if g.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if g.BaseDir == "" {
		g.BaseDir = defaultBaseDir
	}
	if !filepath.IsAbs(g.BaseDir) {
		abs, err := filepath.Abs(g.BaseDir)
		if err != nil {
			return fmt.Errorf("base dir could not be resolved: %w", err)
		}
		g.BaseDir = abs
	}
	if g.Subscription.Source == "" {
		g.Subscription.Source = "local"
	}
	if g.Subscription.Source != "local" {
		return fmt.Errorf("subscription.source must be local")
	}
	if err := g.PortRanges.Validate(); err != nil {
		return err
	}
	if filepath.Clean(g.ResolvePath(g.Paths.Sub)) == filepath.Clean(g.ResolvePath(g.Paths.Runtime)) {
		return fmt.Errorf("paths.sub must not equal paths.runtime")
	}
	if filepath.Clean(g.ResolvePath(g.Paths.Sub)) == filepath.Clean(g.ResolvePath(g.Paths.Stacks)) {
		return fmt.Errorf("paths.sub must not equal paths.stacks")
	}
	if err := validateDefaultClash(g.Defaults.Clash); err != nil {
		return err
	}
	if err := validateDefaultXrelay(g.Defaults.Xrelay); err != nil {
		return err
	}
	return nil
}

// InboundAuth 保存 socks/http inbound 鉴权配置。
type InboundAuth struct {
	Type     string `json:"type" yaml:"type" mapstructure:"type"`
	Username string `json:"username" yaml:"username" mapstructure:"username"`
	Password string `json:"password" yaml:"password" mapstructure:"password"`
}

// Validate 校验 password 模式必须同时配置用户名和密码。
func (a InboundAuth) Validate() error {
	if a.Type != "noauth" && a.Type != "password" {
		return fmt.Errorf("auth.type must be noauth or password")
	}
	if a.Type == "password" && (a.Username == "" || a.Password == "") {
		return fmt.Errorf("username and password are required for password auth")
	}
	return nil
}

// InboundUser 保存 vmess/shadowsocks 多用户凭据。
type InboundUser struct {
	User            string `json:"user" yaml:"user" mapstructure:"user"`
	UUID            string `json:"uuid" yaml:"uuid" mapstructure:"uuid"`
	Password        string `json:"password" yaml:"password" mapstructure:"password"`
	Method          string `json:"method" yaml:"method" mapstructure:"method"`
	Cipher          string `json:"cipher" yaml:"cipher" mapstructure:"cipher"`
	Email           string `json:"email" yaml:"email" mapstructure:"email"`
	Remark          string `json:"remark" yaml:"remark" mapstructure:"remark"`
	DisplayTemplate string `json:"display_template" yaml:"display_template" mapstructure:"display_template"`
	Tag             string `json:"tag" yaml:"tag" mapstructure:"tag"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 记录 users[] 中显式字段，用于拒绝不支持的 region 字段。
func (u *InboundUser) UnmarshalYAML(value *yaml.Node) error {
	u.fields = yamlFields(value)
	type raw InboundUser
	if err := value.Decode((*raw)(u)); err != nil {
		return err
	}
	if u.fields["region"] {
		return fmt.Errorf("users[].region is not supported; configure inbound.region instead")
	}
	return nil
}

// EmailOrUser 返回 Xray 用户统计 email，未配置时使用业务 user。
func (u InboundUser) EmailOrUser() string {
	if u.Email != "" {
		return u.Email
	}
	return u.User
}

// WebSocketOptions 保存 vmess websocket 传输参数。
type WebSocketOptions struct {
	Path    string            `json:"path,omitempty" yaml:"path" mapstructure:"path"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers" mapstructure:"headers"`
}

// GRPCOptions 保存 vmess grpc 传输参数。
type GRPCOptions struct {
	ServiceName string `json:"service_name,omitempty" yaml:"service_name" mapstructure:"service_name"`
}

// UnmarshalYAML 兼容常见 gRPC service name 字段写法。
func (o *GRPCOptions) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("grpc_opts must be a mapping")
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		key := value.Content[index].Value
		if key != "service_name" && key != "grpc_service_name" && key != "serviceName" {
			continue
		}
		o.ServiceName = value.Content[index+1].Value
	}
	return nil
}

// Inbound 保存 xrelay inbound 配置。
type Inbound struct {
	Name            string            `json:"name" yaml:"name" mapstructure:"name"`
	Protocol        string            `json:"protocol" yaml:"protocol" mapstructure:"protocol"`
	Listen          string            `json:"listen" yaml:"listen" mapstructure:"listen"`
	Port            int               `json:"port" yaml:"port" mapstructure:"port"`
	UDP             bool              `json:"udp" yaml:"udp" mapstructure:"udp"`
	Auth            *InboundAuth      `json:"auth" yaml:"auth" mapstructure:"auth"`
	User            string            `json:"user" yaml:"user" mapstructure:"user"`
	Server          string            `json:"server" yaml:"server" mapstructure:"server"`
	Remark          string            `json:"remark" yaml:"remark" mapstructure:"remark"`
	DisplayTemplate string            `json:"display_template" yaml:"display_template" mapstructure:"display_template"`
	Region          string            `json:"region" yaml:"region" mapstructure:"region"`
	Tag             string            `json:"tag" yaml:"tag" mapstructure:"tag"`
	Sub             bool              `json:"sub" yaml:"sub" mapstructure:"sub"`
	UUID            string            `json:"uuid" yaml:"uuid" mapstructure:"uuid"`
	Users           []InboundUser     `json:"users" yaml:"users" mapstructure:"users"`
	Network         string            `json:"network" yaml:"network" mapstructure:"network"`
	Password        string            `json:"password" yaml:"password" mapstructure:"password"`
	Method          string            `json:"method" yaml:"method" mapstructure:"method"`
	Cipher          string            `json:"cipher" yaml:"cipher" mapstructure:"cipher"`
	WSOpts          *WebSocketOptions `json:"ws_opts" yaml:"ws_opts" mapstructure:"ws_opts"`
	GRPCOpts        *GRPCOptions      `json:"grpc_opts" yaml:"grpc_opts" mapstructure:"grpc_opts"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 在加载 inbound 时补齐默认监听地址并记录显式字段。
func (i *Inbound) UnmarshalYAML(value *yaml.Node) error {
	*i = Inbound{Listen: defaultListenHost, fields: yamlFields(value)}
	type raw Inbound
	return value.Decode((*raw)(i))
}

// TagOrDefault 返回显式 tag 或协议端口名称组成的默认 tag。
func (i Inbound) TagOrDefault() string {
	if i.Tag != "" {
		return i.Tag
	}
	return fmt.Sprintf("%s:%d:%s", i.Protocol, i.Port, i.Name)
}

// UDPConfigured 判断 inbound 是否显式配置了 udp 字段。
func (i Inbound) UDPConfigured() bool {
	if i.fields != nil {
		return i.fields["udp"]
	}
	return i.UDP
}

// Validate 校验 inbound 的协议字段、凭据和订阅暴露约束。
func (i Inbound) Validate() error {
	if err := ValidateIdentifier(i.Name, "inbound name"); err != nil {
		return err
	}
	if !validInboundProtocol(i.Protocol) {
		return fmt.Errorf("unsupported inbound protocol: %s", i.Protocol)
	}
	if err := ValidatePort(i.Port, "inbound port"); err != nil {
		return err
	}
	if i.fields != nil && !i.fields["sub"] {
		return fmt.Errorf("sub is required for inbound")
	}
	if i.Region != "" && !regionPattern.MatchString(i.Region) {
		return fmt.Errorf("region must use two uppercase letters")
	}
	if i.UDPConfigured() && !SupportsInboundUDPProtocol(i.Protocol) {
		return fmt.Errorf("udp is not supported for %s inbound", i.Protocol)
	}
	if i.Auth != nil {
		if err := i.Auth.Validate(); err != nil {
			return err
		}
	}
	if len(i.Users) > 0 && i.Protocol != "vmess" && i.Protocol != "shadowsocks" {
		return fmt.Errorf("users is only supported for vmess or shadowsocks inbound")
	}
	switch i.Protocol {
	case "vmess":
		return i.validateVmess()
	case "shadowsocks":
		return i.validateShadowsocks()
	case "socks5", "http":
		if i.Sub && (i.Auth == nil || i.Auth.Type != "password") {
			return fmt.Errorf("password auth is required when socks/http inbound is published")
		}
	}
	return nil
}

// validateVmess 校验 vmess 多用户字段。
func (i Inbound) validateVmess() error {
	if i.UUID != "" {
		return fmt.Errorf("uuid is not supported for vmess inbound; use users instead")
	}
	if i.User != "" || i.Remark != "" {
		return fmt.Errorf("user and remark must be configured under vmess users")
	}
	if len(i.Users) == 0 {
		return fmt.Errorf("users is required for vmess inbound")
	}
	if i.Network == "" {
		return fmt.Errorf("network is required for vmess inbound")
	}
	if err := ensureUnique(userValues(i.Users, func(user InboundUser) string { return user.User }), "duplicate vmess user"); err != nil {
		return err
	}
	uuids := make([]string, 0, len(i.Users))
	emails := make([]string, 0, len(i.Users))
	tags := make([]string, 0, len(i.Users))
	baseTag := i.TagOrDefault()
	for _, user := range i.Users {
		if err := ValidateIdentifier(user.User, "vmess user"); err != nil {
			return err
		}
		if user.UUID == "" {
			return fmt.Errorf("uuid is required for vmess user")
		}
		if !uuidPattern.MatchString(user.UUID) {
			return fmt.Errorf("uuid must be a valid UUID")
		}
		uuids = append(uuids, strings.ToLower(user.UUID))
		emails = append(emails, user.EmailOrUser())
		if user.Tag != "" {
			tags = append(tags, user.Tag)
		} else {
			tags = append(tags, baseTag+":"+user.User)
		}
	}
	if err := ensureUnique(uuids, "duplicate vmess uuid"); err != nil {
		return err
	}
	if err := ensureUnique(emails, "duplicate vmess user email"); err != nil {
		return err
	}
	return ensureUnique(tags, "duplicate vmess user tag")
}

// validateShadowsocks 校验 shadowsocks 单用户或多用户字段。
func (i Inbound) validateShadowsocks() error {
	if i.Password == "" {
		return fmt.Errorf("password is required for shadowsocks inbound")
	}
	method := i.MethodOrCipher()
	if method == "" {
		return fmt.Errorf("method or cipher is required for shadowsocks inbound")
	}
	if err := i.validateShadowsocks2022Passwords(method); err != nil {
		return err
	}
	if len(i.Users) == 0 {
		return nil
	}
	if i.User != "" || i.Remark != "" {
		return fmt.Errorf("user and remark must be configured under shadowsocks users")
	}
	if err := ensureUnique(userValues(i.Users, func(user InboundUser) string { return user.User }), "duplicate shadowsocks user"); err != nil {
		return err
	}
	emails := make([]string, 0, len(i.Users))
	tags := make([]string, 0, len(i.Users))
	baseTag := i.TagOrDefault()
	for _, user := range i.Users {
		if err := ValidateIdentifier(user.User, "shadowsocks user"); err != nil {
			return err
		}
		if user.Password == "" {
			return fmt.Errorf("password is required for shadowsocks user")
		}
		if IsShadowsocks2022Method(method) && (user.Method != "" || user.Cipher != "") {
			return fmt.Errorf("shadowsocks 2022 users must not set method or cipher")
		}
		emails = append(emails, user.EmailOrUser())
		if user.Tag != "" {
			tags = append(tags, user.Tag)
		} else {
			tags = append(tags, baseTag+":"+user.User)
		}
	}
	if err := ensureUnique(emails, "duplicate shadowsocks user email"); err != nil {
		return err
	}
	return ensureUnique(tags, "duplicate shadowsocks user tag")
}

// MethodOrCipher 返回 shadowsocks method 字段，兼容旧配置中的 cipher。
func (i Inbound) MethodOrCipher() string {
	if i.Method != "" {
		return i.Method
	}
	return i.Cipher
}

// validateShadowsocks2022Passwords 校验 SS2022 server/user PSK。
func (i Inbound) validateShadowsocks2022Passwords(method string) error {
	if !IsShadowsocks2022Method(method) {
		return nil
	}
	if err := ValidateShadowsocks2022PSK(i.Password, method, "shadowsocks 2022 inbound password"); err != nil {
		return err
	}
	for _, user := range i.Users {
		if user.Password == "" {
			continue
		}
		if err := ValidateShadowsocks2022PSK(user.Password, method, "shadowsocks 2022 user password for "+user.User); err != nil {
			return err
		}
	}
	return nil
}

// XrelayOutbound 保存 xrelay egress 配置。
type XrelayOutbound struct {
	Type          string `json:"type" yaml:"type" mapstructure:"type"`
	Ref           string `json:"ref" yaml:"ref" mapstructure:"ref"`
	Server        string `json:"server" yaml:"server" mapstructure:"server"`
	Port          int    `json:"port" yaml:"port" mapstructure:"port"`
	Username      string `json:"username" yaml:"username" mapstructure:"username"`
	Password      string `json:"password" yaml:"password" mapstructure:"password"`
	PrivateDirect bool   `json:"private_direct" yaml:"private_direct" mapstructure:"private_direct"`
}

// Validate 校验 outbound 类型和对应目标字段。
func (x XrelayOutbound) Validate() error {
	if x.PrivateDirect && x.Type != "socks5" && x.Type != "http" {
		return fmt.Errorf("private_direct is only supported for socks5/http outbound")
	}
	switch x.Type {
	case "clash":
		return ValidateRef(x.Ref, 3, "clash outbound ref is required")
	case "socks5", "http":
		if x.Server == "" || x.Port == 0 {
			return fmt.Errorf("server and port are required for socks5/http outbound")
		}
		return ValidatePort(x.Port, "outbound port")
	case "direct":
		return nil
	default:
		return fmt.Errorf("unsupported outbound type: %s", x.Type)
	}
}

// XrelayConfig 保存单个 stack 的 xrelay 配置。
type XrelayConfig struct {
	Enabled  bool                `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	LogLevel string              `json:"loglevel" yaml:"loglevel" mapstructure:"loglevel"`
	Outbound XrelayOutbound      `json:"outbound" yaml:"outbound" mapstructure:"outbound"`
	Inbounds []Inbound           `json:"inbounds" yaml:"inbounds" mapstructure:"inbounds"`
	API      *XrelayAPIConfig    `json:"api" yaml:"api" mapstructure:"api"`
	Stats    *XrelayStatsConfig  `json:"stats" yaml:"stats" mapstructure:"stats"`
	Policy   *XrelayPolicyConfig `json:"policy" yaml:"policy" mapstructure:"policy"`
}

// UnmarshalYAML 在加载 xrelay 时补齐 enabled 默认值。
func (x *XrelayConfig) UnmarshalYAML(value *yaml.Node) error {
	*x = XrelayConfig{Enabled: true}
	type raw XrelayConfig
	return value.Decode((*raw)(x))
}

// Validate 校验 xrelay 配置和 inbound 唯一性。
func (x XrelayConfig) Validate() error {
	if x.LogLevel != "" && !validXrayLogLevel(x.LogLevel) {
		return fmt.Errorf("xrelay loglevel is invalid: %s", x.LogLevel)
	}
	if x.API != nil {
		if err := validateAPIConfig(*x.API); err != nil {
			return err
		}
	}
	if err := x.Outbound.Validate(); err != nil {
		return err
	}
	if len(x.Inbounds) == 0 {
		return fmt.Errorf("inbounds is required for xrelay")
	}
	names := make([]string, 0, len(x.Inbounds))
	for _, inbound := range x.Inbounds {
		if err := inbound.Validate(); err != nil {
			return err
		}
		names = append(names, inbound.Name)
	}
	return ensureUnique(names, "duplicate inbound name")
}

// ClashController 保存 mihomo controller 配置。
type ClashController struct {
	Listen string `json:"listen" yaml:"listen" mapstructure:"listen"`
	Secret string `json:"secret" yaml:"secret" mapstructure:"secret"`
}

// Validate 校验 controller 监听地址和密钥。
func (c ClashController) Validate() error {
	if _, _, err := ParseListen(c.Listen); err != nil {
		return err
	}
	if c.Secret == "" {
		return fmt.Errorf("controller secret is required")
	}
	return nil
}

// ClashListenerUser 保存 mihomo 高级 listener 认证用户。
type ClashListenerUser struct {
	Username string `json:"username" yaml:"username" mapstructure:"username"`
	Password string `json:"password" yaml:"password" mapstructure:"password"`
}

// Validate 校验 listener 用户名和密码。
func (c ClashListenerUser) Validate() error {
	if err := ValidateIdentifier(c.Username, "listener username"); err != nil {
		return err
	}
	if c.Password == "" {
		return fmt.Errorf("listener password is required")
	}
	return nil
}

// SocksListener 保存 mihomo socks listener 配置。
type SocksListener struct {
	Name   string              `json:"name" yaml:"name" mapstructure:"name"`
	Listen string              `json:"listen" yaml:"listen" mapstructure:"listen"`
	Port   int                 `json:"port" yaml:"port" mapstructure:"port"`
	Users  []ClashListenerUser `json:"users" yaml:"users" mapstructure:"users"`
}

// UnmarshalYAML 在加载 socks listener 时补齐 loopback 默认监听地址。
func (s *SocksListener) UnmarshalYAML(value *yaml.Node) error {
	*s = SocksListener{Listen: defaultClashListenerHost}
	type raw SocksListener
	return value.Decode((*raw)(s))
}

// Validate 校验 socks listener 名称、端口和认证用户。
func (s SocksListener) Validate() error {
	if err := ValidateIdentifier(s.Name, "listener name"); err != nil {
		return err
	}
	if err := ValidatePort(s.Port, "listener port"); err != nil {
		return err
	}
	for _, user := range s.Users {
		if err := user.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// HTTPListener 保存 mihomo HTTP listener 配置。
type HTTPListener struct {
	Name   string              `json:"name" yaml:"name" mapstructure:"name"`
	Listen string              `json:"listen" yaml:"listen" mapstructure:"listen"`
	Port   int                 `json:"port" yaml:"port" mapstructure:"port"`
	Users  []ClashListenerUser `json:"users" yaml:"users" mapstructure:"users"`
}

// UnmarshalYAML 在加载 HTTP listener 时补齐 loopback 默认监听地址。
func (h *HTTPListener) UnmarshalYAML(value *yaml.Node) error {
	*h = HTTPListener{Listen: defaultClashListenerHost}
	type raw HTTPListener
	return value.Decode((*raw)(h))
}

// Validate 校验 HTTP listener 名称、端口和认证用户。
func (h HTTPListener) Validate() error {
	if err := ValidateIdentifier(h.Name, "listener name"); err != nil {
		return err
	}
	if err := ValidatePort(h.Port, "listener port"); err != nil {
		return err
	}
	for _, user := range h.Users {
		if err := user.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ClashListeners 保存 mihomo listener 集合，P0 只支持 socks 和 HTTP。
type ClashListeners struct {
	Socks []SocksListener `json:"socks" yaml:"socks" mapstructure:"socks"`
	HTTP  []HTTPListener  `json:"http" yaml:"http" mapstructure:"http"`
	Mixed any             `json:"mixed" yaml:"mixed,omitempty" mapstructure:"mixed"`

	fields map[string]bool `json:"-" yaml:"-"`
}

// UnmarshalYAML 记录 mixed 字段是否出现，以便显式拒绝。
func (c *ClashListeners) UnmarshalYAML(value *yaml.Node) error {
	c.fields = yamlFields(value)
	type raw ClashListeners
	return value.Decode((*raw)(c))
}

// Validate 校验 P0 listener 限制和内部唯一性。
func (c ClashListeners) Validate() error {
	if c.fields["mixed"] {
		return fmt.Errorf("listeners.mixed is not supported in P0")
	}
	if len(c.Socks) > 1 {
		return fmt.Errorf("only one socks listener is supported in P0")
	}
	if len(c.HTTP) > 1 {
		return fmt.Errorf("only one http listener is supported in P0")
	}
	socksNames := make([]string, 0, len(c.Socks))
	for _, listener := range c.Socks {
		if err := listener.Validate(); err != nil {
			return err
		}
		socksNames = append(socksNames, listener.Name)
	}
	httpNames := make([]string, 0, len(c.HTTP))
	for _, listener := range c.HTTP {
		if err := listener.Validate(); err != nil {
			return err
		}
		httpNames = append(httpNames, listener.Name)
	}
	if err := ensureUnique(socksNames, "duplicate socks listener name"); err != nil {
		return err
	}
	return ensureUnique(httpNames, "duplicate http listener name")
}

// ClashUpstream 保存 mihomo upstream/proxy 配置。
type ClashUpstream struct {
	Name   string         `json:"name" yaml:"name" mapstructure:"name"`
	Type   string         `json:"type" yaml:"type" mapstructure:"type"`
	Ref    string         `json:"ref" yaml:"ref" mapstructure:"ref"`
	Config map[string]any `json:"config" yaml:"config" mapstructure:"config"`

	configNode *yaml.Node `json:"-" yaml:"-"`
}

// UnmarshalYAML 保留 raw config 的原始字段顺序，避免生成 mihomo YAML 时 map 迭代导致 diff 抖动。
func (c *ClashUpstream) UnmarshalYAML(value *yaml.Node) error {
	type raw ClashUpstream
	if value.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(value.Content); index += 2 {
			if value.Content[index].Value == "config" {
				c.configNode = cloneYAMLNode(value.Content[index+1])
				break
			}
		}
	}
	return value.Decode((*raw)(c))
}

// ConfigNode 返回 raw upstream config 的有序 YAML 节点副本。
func (c ClashUpstream) ConfigNode() *yaml.Node {
	return cloneYAMLNode(c.configNode)
}

// Validate 校验 upstream 类型和必填字段。
func (c ClashUpstream) Validate() error {
	if err := ValidateIdentifier(c.Name, "upstream name"); err != nil {
		return err
	}
	switch c.Type {
	case "xrelay-socks5":
		return ValidateRef(c.Ref, 2, "xrelay-socks5 ref is required")
	case "raw":
		return ValidateRawProxyConfig(c.Config)
	default:
		return fmt.Errorf("unsupported upstream type: %s", c.Type)
	}
}

// ClashGroup 保存 mihomo proxy group 配置。
type ClashGroup struct {
	Name     string   `json:"name" yaml:"name" mapstructure:"name"`
	Type     string   `json:"type" yaml:"type" mapstructure:"type"`
	Proxies  []string `json:"proxies" yaml:"proxies" mapstructure:"proxies"`
	URL      string   `json:"url" yaml:"url" mapstructure:"url"`
	Interval int      `json:"interval" yaml:"interval" mapstructure:"interval"`
	Strategy string   `json:"strategy" yaml:"strategy" mapstructure:"strategy"`
}

// Validate 校验 group 类型、引用列表和健康检查字段。
func (c *ClashGroup) Validate() error {
	if err := ValidateIdentifier(c.Name, "group name"); err != nil {
		return err
	}
	if !validGroupType(c.Type) {
		return fmt.Errorf("unsupported group type: %s", c.Type)
	}
	if len(c.Proxies) == 0 {
		return fmt.Errorf("group proxies is required")
	}
	if c.Type == "url-test" || c.Type == "load-balance" || c.Type == "fallback" {
		if c.URL == "" || c.Interval == 0 {
			return fmt.Errorf("url and interval are required for url-test/load-balance/fallback group")
		}
	}
	if c.Type == "load-balance" && c.Strategy == "" {
		c.Strategy = "consistent-hashing"
	}
	return nil
}

// ClashRules 保存 mihomo 规则配置。
type ClashRules struct {
	Profile string   `json:"profile" yaml:"profile" mapstructure:"profile"`
	Final   string   `json:"final" yaml:"final" mapstructure:"final"`
	Extra   []string `json:"extra" yaml:"extra" mapstructure:"extra"`
}

// UnmarshalYAML 在加载 rules 时补齐默认规则 profile 和 final。
func (c *ClashRules) UnmarshalYAML(value *yaml.Node) error {
	*c = ClashRules{Profile: defaultRuleProfile, Final: defaultClashFinal}
	type raw ClashRules
	return value.Decode((*raw)(c))
}

// ClashConfig 保存单个 stack 的 mihomo 配置。
type ClashConfig struct {
	Enabled    bool            `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Mode       string          `json:"mode" yaml:"mode" mapstructure:"mode"`
	LogLevel   string          `json:"loglevel" yaml:"loglevel" mapstructure:"loglevel"`
	Controller ClashController `json:"controller" yaml:"controller" mapstructure:"controller"`
	Listeners  ClashListeners  `json:"listeners" yaml:"listeners" mapstructure:"listeners"`
	Upstreams  []ClashUpstream `json:"upstreams" yaml:"upstreams" mapstructure:"upstreams"`
	Groups     []ClashGroup    `json:"groups" yaml:"groups" mapstructure:"groups"`
	Rules      ClashRules      `json:"rules" yaml:"rules" mapstructure:"rules"`
}

// UnmarshalYAML 在加载 clash 时补齐 enabled、mode 和 rules 默认值。
func (c *ClashConfig) UnmarshalYAML(value *yaml.Node) error {
	*c = ClashConfig{Enabled: true, Mode: defaultClashMode, Rules: ClashRules{Profile: defaultRuleProfile, Final: defaultClashFinal}}
	type raw ClashConfig
	return value.Decode((*raw)(c))
}

// Validate 校验 mihomo 配置内部引用和字段约束。
func (c *ClashConfig) Validate() error {
	if !validClashMode(c.Mode) {
		return fmt.Errorf("clash mode is invalid: %s", c.Mode)
	}
	if c.LogLevel != "" && !validClashLogLevel(c.LogLevel) {
		return fmt.Errorf("clash loglevel is invalid: %s", c.LogLevel)
	}
	if err := c.Controller.Validate(); err != nil {
		return err
	}
	if err := c.Listeners.Validate(); err != nil {
		return err
	}
	upstreamNames := make([]string, 0, len(c.Upstreams))
	for _, upstream := range c.Upstreams {
		if err := upstream.Validate(); err != nil {
			return err
		}
		upstreamNames = append(upstreamNames, upstream.Name)
	}
	groupNames := make([]string, 0, len(c.Groups))
	for index := range c.Groups {
		if err := c.Groups[index].Validate(); err != nil {
			return err
		}
		groupNames = append(groupNames, c.Groups[index].Name)
	}
	if err := ensureUnique(upstreamNames, "duplicate upstream name"); err != nil {
		return err
	}
	if err := ensureUnique(groupNames, "duplicate group name"); err != nil {
		return err
	}
	if c.Rules.Profile == "" {
		c.Rules.Profile = defaultRuleProfile
	}
	if c.Rules.Final == "" {
		c.Rules.Final = defaultClashFinal
	}
	if c.Rules.Profile != defaultRuleProfile {
		return fmt.Errorf("rules.profile only supports default")
	}
	knownTargets := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, name := range upstreamNames {
		knownTargets[name] = true
	}
	for _, name := range groupNames {
		knownTargets[name] = true
	}
	for _, group := range c.Groups {
		for _, target := range group.Proxies {
			if !knownTargets[target] {
				return fmt.Errorf("group proxy target does not exist: %s", target)
			}
		}
	}
	if err := ValidateRuleTarget(c.Rules.Final, knownTargets, "rules.final target does not exist"); err != nil {
		return err
	}
	for _, rule := range c.Rules.Extra {
		target, err := ExtractRuleTarget(rule)
		if err != nil {
			return err
		}
		if err := ValidateRuleTarget(target, knownTargets, "rules.extra target does not exist"); err != nil {
			return err
		}
	}
	return nil
}

// Stack 保存单个 stacks/<name>.yaml 配置。
type Stack struct {
	Name       string       `json:"name" yaml:"name" mapstructure:"name"`
	Enabled    bool         `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Role       string       `json:"role" yaml:"role" mapstructure:"role"`
	Labels     []string     `json:"labels" yaml:"labels" mapstructure:"labels"`
	Xrelay     XrelayConfig `json:"xrelay" yaml:"xrelay" mapstructure:"xrelay"`
	Clash      ClashConfig  `json:"clash" yaml:"clash" mapstructure:"clash"`
	SourcePath string       `json:"-" yaml:"-"`
}

// UnmarshalYAML 在加载 stack 时补齐 enabled 和 role 默认值。
func (s *Stack) UnmarshalYAML(value *yaml.Node) error {
	*s = Stack{Enabled: true, Role: defaultStackRole}
	type raw Stack
	return value.Decode((*raw)(s))
}

// Validate 校验 stack 文件内部配置。
func (s *Stack) Validate() error {
	if err := ValidateIdentifier(s.Name, "stack name"); err != nil {
		return err
	}
	if s.Role == "" {
		s.Role = defaultStackRole
	}
	if s.Role != "edge" && s.Role != "auto" {
		return fmt.Errorf("stack role must be edge or auto")
	}
	for _, label := range s.Labels {
		if err := ValidateIdentifier(label, "label"); err != nil {
			return err
		}
	}
	if err := s.Xrelay.Validate(); err != nil {
		return err
	}
	return s.Clash.Validate()
}

// StackSet 保存全局配置和所有 stack 的合并输入。
type StackSet struct {
	Config GlobalConfig `json:"config" yaml:"config"`
	Stacks []Stack      `json:"stacks" yaml:"stacks"`
}

// StackNames 返回所有 stack 名称，保持加载顺序。
func (s StackSet) StackNames() []string {
	names := make([]string, 0, len(s.Stacks))
	for _, stack := range s.Stacks {
		names = append(names, stack.Name)
	}
	return names
}

// ByName 返回按 stack 名称索引的映射。
func (s StackSet) ByName() map[string]*Stack {
	stacks := make(map[string]*Stack, len(s.Stacks))
	for index := range s.Stacks {
		stacks[s.Stacks[index].Name] = &s.Stacks[index]
	}
	return stacks
}

// ResolveXrelayLogLevel 读取 stack 级 Xray 日志级别覆盖，未配置时使用全局默认。
func ResolveXrelayLogLevel(defaults DefaultXrelayConfig, xrelay XrelayConfig) string {
	if xrelay.LogLevel != "" {
		return xrelay.LogLevel
	}
	if defaults.LogLevel != "" {
		return defaults.LogLevel
	}
	return defaultXrayLogLevel
}

// ResolveClashLogLevel 读取 stack 级 mihomo 日志级别覆盖，未配置时使用全局默认。
func ResolveClashLogLevel(defaults DefaultClashConfig, clash ClashConfig) string {
	if clash.LogLevel != "" {
		return clash.LogLevel
	}
	if defaults.LogLevel != "" {
		return defaults.LogLevel
	}
	return defaultClashLogLevel
}

// ResolveXrelayAPIConfig 合并全局 defaults.xrelay.api 和 stack 级覆盖。
func ResolveXrelayAPIConfig(defaults DefaultXrelayConfig, xrelay XrelayConfig) XrelayAPIConfig {
	if xrelay.API == nil {
		return defaults.API
	}
	override := *xrelay.API
	if override.fields == nil {
		return override
	}
	merged := defaults.API
	if override.fields["enabled"] {
		merged.Enabled = override.Enabled
	}
	if override.fields["tag"] {
		merged.Tag = override.Tag
	}
	if override.fields["listen"] {
		merged.Listen = override.Listen
	}
	if override.fields["services"] {
		merged.Services = append([]string(nil), override.Services...)
	}
	return merged
}

// ResolveXrelayStatsConfig 合并全局 defaults.xrelay.stats 和 stack 级覆盖。
func ResolveXrelayStatsConfig(defaults DefaultXrelayConfig, xrelay XrelayConfig) XrelayStatsConfig {
	if xrelay.Stats == nil {
		return defaults.Stats
	}
	override := *xrelay.Stats
	if override.fields == nil {
		return override
	}
	merged := defaults.Stats
	if override.fields["enabled"] {
		merged.Enabled = override.Enabled
	}
	return merged
}

// ResolveXrelayPolicyConfig 合并全局 defaults.xrelay.policy 和 stack 级覆盖。
func ResolveXrelayPolicyConfig(defaults DefaultXrelayConfig, xrelay XrelayConfig) XrelayPolicyConfig {
	if xrelay.Policy == nil {
		return defaults.Policy
	}
	override := *xrelay.Policy
	if override.fields == nil {
		return override
	}
	merged := defaults.Policy
	if override.fields["enabled"] {
		merged.Enabled = override.Enabled
	}
	if override.fields["levels"] {
		merged.Levels = mergePolicyLevels(merged.Levels, override.Levels)
	}
	if override.fields["system"] {
		merged.System = mergePolicySystem(merged.System, override.System)
	}
	return merged
}

// IsShadowsocks2022Method 判断 shadowsocks method 是否属于 SS2022 方法。
func IsShadowsocks2022Method(method string) bool {
	_, ok := shadowsocks2022KeyLengths[method]
	return ok
}

// ValidateShadowsocks2022PSK 校验 SS2022 PSK 是 base64 且解码长度符合 method。
func ValidateShadowsocks2022PSK(value string, method string, label string) error {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return fmt.Errorf("%s must be a base64-encoded PSK for %s", label, method)
	}
	expectedLength := shadowsocks2022KeyLengths[method]
	if len(decoded) != expectedLength {
		return fmt.Errorf("%s must decode to %d bytes for %s", label, expectedLength, method)
	}
	return nil
}

// ValidateIdentifier 校验配置标识符只包含安全的文件名/ref 片段字符。
func ValidateIdentifier(value string, label string) error {
	if !identifierPattern.MatchString(value) || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s contains invalid characters", label)
	}
	return nil
}

// ValidatePort 校验端口处于 TCP/UDP 合法范围。
func ValidatePort(port int, label string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", label)
	}
	return nil
}

// ValidateRef 校验 ref 存在且段数符合基础格式要求。
func ValidateRef(value string, segmentCount int, message string) error {
	if value == "" {
		return errors.New(message)
	}
	parts := strings.Split(value, ".")
	if len(parts) != segmentCount {
		return fmt.Errorf("ref must contain %d dot-separated segments", segmentCount)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("ref must not contain empty segments")
		}
	}
	return nil
}

// ParseListen 解析 host:port 形式的监听地址。
func ParseListen(value string) (string, int, error) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		if strings.Count(value, ":") == 1 {
			parts := strings.SplitN(value, ":", 2)
			host = parts[0]
			portText = parts[1]
		} else {
			return "", 0, fmt.Errorf("listen must use host:port format")
		}
	}
	if host == "" {
		return "", 0, fmt.Errorf("listen host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, fmt.Errorf("listen port must be an integer")
	}
	if err := ValidatePort(port, "listen port"); err != nil {
		return "", 0, err
	}
	return host, port, nil
}

// IsLoopbackHost 判断 host 是否为本机回环地址。
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NormalizeInternalEndpointAddress 把本机 wildcard listener 地址归一为可连接的 loopback 地址。
func NormalizeInternalEndpointAddress(address string) string {
	switch address {
	case "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	default:
		return address
	}
}

// ValidateRawProxyConfig 校验 raw upstream 中常见 mihomo 节点的必填字段。
func ValidateRawProxyConfig(config map[string]any) error {
	if len(config) == 0 {
		return fmt.Errorf("raw upstream config is required")
	}
	proxyType, _ := config["type"].(string)
	if proxyType == "" {
		return fmt.Errorf("raw upstream config.type is required")
	}
	if server, _ := config["server"].(string); server == "" {
		return fmt.Errorf("raw upstream config.server is required")
	}
	port, ok := config["port"].(int)
	if !ok {
		return fmt.Errorf("raw upstream config.port must be between 1 and 65535")
	}
	if err := ValidatePort(port, "raw upstream config.port"); err != nil {
		return err
	}
	switch proxyType {
	case "vmess":
		uuid, _ := config["uuid"].(string)
		if uuid == "" {
			return fmt.Errorf("raw vmess upstream uuid is required")
		}
		if !uuidPattern.MatchString(uuid) {
			return fmt.Errorf("uuid must be a valid UUID")
		}
		network, _ := config["network"].(string)
		if network == "" {
			return fmt.Errorf("raw vmess upstream network is required")
		}
	case "shadowsocks":
		cipher, _ := config["cipher"].(string)
		password, _ := config["password"].(string)
		if cipher == "" {
			return fmt.Errorf("raw shadowsocks upstream cipher is required")
		}
		if password == "" {
			return fmt.Errorf("raw shadowsocks upstream password is required")
		}
	}
	return nil
}

// ExtractRuleTarget 从 mihomo 规则文本中提取目标策略或组名。
func ExtractRuleTarget(rule string) (string, error) {
	parts := make([]string, 0)
	for _, part := range strings.Split(rule, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) < 3 {
		return "", fmt.Errorf("rules.extra entry is invalid: %s", rule)
	}
	if parts[len(parts)-1] == "no-resolve" && len(parts) >= 4 {
		return parts[len(parts)-2], nil
	}
	return parts[len(parts)-1], nil
}

// ValidateRuleTarget 校验规则目标指向已存在的节点、组或内置策略。
func ValidateRuleTarget(target string, knownTargets map[string]bool, message string) error {
	if !knownTargets[target] {
		return fmt.Errorf("%s: %s", message, target)
	}
	return nil
}

func validateDefaultClash(config DefaultClashConfig) error {
	if !validClashMode(config.Mode) {
		return fmt.Errorf("defaults.clash.mode is invalid: %s", config.Mode)
	}
	if !validClashLogLevel(config.LogLevel) {
		return fmt.Errorf("defaults.clash.loglevel is invalid: %s", config.LogLevel)
	}
	if config.RuleProfile != defaultRuleProfile {
		return fmt.Errorf("defaults.clash.rule_profile only supports default")
	}
	return nil
}

func validateDefaultXrelay(config DefaultXrelayConfig) error {
	if !validXrayLogLevel(config.LogLevel) {
		return fmt.Errorf("defaults.xrelay.loglevel is invalid: %s", config.LogLevel)
	}
	if err := validateAPIConfig(config.API); err != nil {
		return err
	}
	return nil
}

func validateAPIConfig(config XrelayAPIConfig) error {
	if err := ValidateIdentifier(config.Tag, "xray api tag"); err != nil {
		return err
	}
	host, _, err := ParseListen(config.Listen)
	if err != nil {
		return err
	}
	if !IsLoopbackHost(host) {
		return fmt.Errorf("xray api listen must use loopback host")
	}
	if len(config.Services) == 0 {
		return fmt.Errorf("xray api services is required")
	}
	for _, service := range config.Services {
		if service == "" {
			return fmt.Errorf("xray api service name is required")
		}
	}
	return nil
}

func mergePolicyLevels(defaults map[string]XrelayPolicyLevelConfig, overrides map[string]XrelayPolicyLevelConfig) map[string]XrelayPolicyLevelConfig {
	merged := make(map[string]XrelayPolicyLevelConfig, len(defaults)+len(overrides))
	for level, config := range defaults {
		merged[level] = config
	}
	for level, override := range overrides {
		current := merged[level]
		if override.fields == nil {
			merged[level] = override
			continue
		}
		if override.fields["statsUserUplink"] {
			current.StatsUserUplink = override.StatsUserUplink
		}
		if override.fields["statsUserDownlink"] {
			current.StatsUserDownlink = override.StatsUserDownlink
		}
		merged[level] = current
	}
	return merged
}

func mergePolicySystem(defaults XrelayPolicySystemConfig, override XrelayPolicySystemConfig) XrelayPolicySystemConfig {
	if override.fields == nil {
		return override
	}
	merged := defaults
	if override.fields["statsInboundUplink"] {
		merged.StatsInboundUplink = override.StatsInboundUplink
	}
	if override.fields["statsInboundDownlink"] {
		merged.StatsInboundDownlink = override.StatsInboundDownlink
	}
	if override.fields["statsOutboundUplink"] {
		merged.StatsOutboundUplink = override.StatsOutboundUplink
	}
	if override.fields["statsOutboundDownlink"] {
		merged.StatsOutboundDownlink = override.StatsOutboundDownlink
	}
	return merged
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

func cloneYAMLNode(value *yaml.Node) *yaml.Node {
	if value == nil {
		return nil
	}
	cloned := *value
	if len(value.Content) > 0 {
		cloned.Content = make([]*yaml.Node, 0, len(value.Content))
		for _, child := range value.Content {
			cloned.Content = append(cloned.Content, cloneYAMLNode(child))
		}
	}
	return &cloned
}

func ensureUnique(values []string, message string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return fmt.Errorf("%s: %s", message, value)
		}
		seen[value] = true
	}
	return nil
}

func userValues(users []InboundUser, pick func(InboundUser) string) []string {
	values := make([]string, 0, len(users))
	for _, user := range users {
		values = append(values, pick(user))
	}
	return values
}

func validInboundProtocol(protocol string) bool {
	switch protocol {
	case "vmess", "shadowsocks", "socks5", "http":
		return true
	default:
		return false
	}
}

// SupportsInboundUDPProtocol 判断 inbound 协议是否支持导出 UDP 订阅字段。
func SupportsInboundUDPProtocol(protocol string) bool {
	switch protocol {
	case "vmess", "shadowsocks", "socks5":
		return true
	default:
		return false
	}
}

func validXrayLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warning", "error", "none":
		return true
	default:
		return false
	}
}

func validClashLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warning", "error", "silent":
		return true
	default:
		return false
	}
}

func validClashMode(mode string) bool {
	switch mode {
	case "Rule", "Global", "Direct":
		return true
	default:
		return false
	}
}

func validGroupType(groupType string) bool {
	switch groupType {
	case "select", "url-test", "load-balance", "fallback":
		return true
	default:
		return false
	}
}
