package validation

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"text/template"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
)

// Issue 表示带字段路径的配置校验问题。
type Issue struct {
	Path    string
	Message string
}

// String 输出面向 CLI 的单行错误。
func (i Issue) String() string {
	return i.Path + ": " + i.Message
}

// ConfigValidationError 保存所有跨 stack 校验问题，便于 CLI 一次性展示。
type ConfigValidationError struct {
	Issues []Issue
}

// Error 输出所有校验问题。
func (e ConfigValidationError) Error() string {
	lines := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		lines = append(lines, issue.String())
	}
	return strings.Join(lines, "\n")
}

// PortBinding 表示本地监听端口和来源字段路径。
type PortBinding struct {
	Host string
	Port int
	Path string
}

// PortChecker 抽象系统端口探测，测试可注入 fake 实现。
type PortChecker interface {
	IsAvailable(host string, port int) bool
}

// TCPPortChecker 通过尝试 bind 判断端口是否可用。
type TCPPortChecker struct{}

// IsAvailable 判断指定 host/port 是否可绑定。
func (TCPPortChecker) IsAvailable(host string, port int) bool {
	family := "tcp4"
	if strings.Contains(host, ":") && host != "0.0.0.0" {
		family = "tcp6"
	}
	listener, err := net.Listen(family, net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// NoopPortChecker 跳过系统端口占用探测。
type NoopPortChecker struct{}

// IsAvailable 始终返回端口可用，供只做配置逻辑测试时使用。
func (NoopPortChecker) IsAvailable(host string, port int) bool {
	return true
}

// FakePortChecker 使用预设端口集合模拟系统端口占用。
type FakePortChecker struct {
	Occupied map[int]bool
}

// IsAvailable 根据 Occupied 判断端口是否可用。
func (f FakePortChecker) IsAvailable(host string, port int) bool {
	return !f.Occupied[port]
}

// Option 调整 stack set 校验行为。
type Option func(*options)

type options struct {
	portChecker PortChecker
}

// WithPortChecker 注入系统端口检查实现。
func WithPortChecker(portChecker PortChecker) Option {
	return func(options *options) {
		options.portChecker = portChecker
	}
}

// ValidateStackSet 执行跨 stack 的名称、端口、安全和引用图校验。
func ValidateStackSet(stackSet domain.StackSet, opts ...Option) error {
	options := options{portChecker: TCPPortChecker{}}
	for _, opt := range opts {
		opt(&options)
	}
	issues := make([]Issue, 0)
	issues = append(issues, ValidateUniqueStackNames(stackSet.Stacks)...)
	issues = append(issues, ValidatePublicInboundAuth(stackSet.Config, stackSet.Stacks)...)
	issues = append(issues, ValidateSubscriptionProxyNames(stackSet.Stacks)...)
	portBindings := CollectPortBindings(stackSet)
	issues = append(issues, ValidateUniquePorts(portBindings)...)
	issues = append(issues, ValidateReferenceGraph(stackSet)...)
	if options.portChecker != nil {
		issues = append(issues, ValidateSystemPortsAvailable(portBindings, options.portChecker)...)
	}
	if len(issues) > 0 {
		return ConfigValidationError{Issues: issues}
	}
	return nil
}

var subscriptionDisplayTemplateFuncs = template.FuncMap{
	"toUpper": strings.ToUpper,
	"toLower": strings.ToLower,
	"trim":    strings.TrimSpace,
	"replace": func(old string, new string, value string) string {
		return strings.ReplaceAll(value, old, new)
	},
}

type subscriptionProxyName struct {
	User   string
	Remark string
	Path   string
}

// ValidateSubscriptionProxyNames 校验同一订阅 user 下最终节点展示名不重复。
func ValidateSubscriptionProxyNames(stacks []domain.Stack) []Issue {
	issues := make([]Issue, 0)
	seen := map[string]subscriptionProxyName{}
	for _, stack := range stacks {
		if !stack.Enabled || !stack.Xray.Enabled {
			continue
		}
		for inboundIndex, inbound := range stack.Xray.Inbounds {
			if !inbound.Sub {
				continue
			}
			names, nameIssues := collectSubscriptionProxyNames(stack.Name, inboundIndex, inbound)
			issues = append(issues, nameIssues...)
			for _, name := range names {
				key := name.User + "\x00" + name.Remark
				if first, ok := seen[key]; ok {
					issues = append(issues, Issue{
						Path:    name.Path,
						Message: fmt.Sprintf("duplicate proxy name for user: user=%s name=%s, first seen at %s, repeated at %s", name.User, name.Remark, first.Path, name.Path),
					})
					continue
				}
				seen[key] = name
			}
		}
	}
	return issues
}

// collectSubscriptionProxyNames 渲染一个 inbound 会贡献的订阅 user/name 对。
func collectSubscriptionProxyNames(stackName string, inboundIndex int, inbound domain.Inbound) ([]subscriptionProxyName, []Issue) {
	path := fmt.Sprintf("stacks.%s.xray.inbounds[%d]", stackName, inboundIndex)
	users := subscriptionInboundUsers(inbound)
	if len(users) > 0 {
		names := make([]subscriptionProxyName, 0, len(users))
		issues := make([]Issue, 0)
		for userIndex, user := range users {
			remark, err := subscriptionRemark(stackName, inbound, user.User, user.ProfileOrDefault(), firstNonEmpty(user.Remark, inbound.Remark), firstNonEmpty(user.DisplayTemplate, inbound.DisplayTemplate))
			userPath := fmt.Sprintf("%s.user_refs[%d]", path, userIndex)
			if !inbound.UsesUserRefs() {
				userPath = fmt.Sprintf("%s.users[%d]", path, userIndex)
			}
			if err != nil {
				issues = append(issues, Issue{Path: userPath + ".display_template", Message: err.Error()})
				continue
			}
			names = append(names, subscriptionProxyName{User: user.User, Remark: remark, Path: userPath})
		}
		return names, issues
	}
	user := inbound.User
	if user == "" {
		user = "default"
	}
	remark, err := subscriptionRemark(stackName, inbound, user, domain.DefaultUserProfile, inbound.Remark, inbound.DisplayTemplate)
	if err != nil {
		return nil, []Issue{{Path: path + ".display_template", Message: err.Error()}}
	}
	return []subscriptionProxyName{{User: user, Remark: remark, Path: path}}, nil
}

// subscriptionInboundUsers 返回展开后或原始 user_refs 代表的订阅用户列表。
func subscriptionInboundUsers(inbound domain.Inbound) []domain.InboundUser {
	if len(inbound.Users) > 0 {
		return inbound.Users
	}
	if len(inbound.UserRefs) == 0 {
		return nil
	}
	users := make([]domain.InboundUser, 0, len(inbound.UserRefs))
	for _, ref := range inbound.UserRefs {
		users = append(users, domain.InboundUser{
			User:            ref.User,
			Profile:         domain.NormalizeUserProfile(ref.Profile),
			Remark:          ref.Remark,
			DisplayTemplate: ref.DisplayTemplate,
			Tag:             ref.Tag,
		})
	}
	return users
}

// subscriptionRemark 复用订阅生成器的命名规则渲染最终节点名。
func subscriptionRemark(stackName string, inbound domain.Inbound, user string, profile string, configuredRemark string, displayTemplate string) (string, error) {
	baseRemark := configuredRemark
	if baseRemark == "" {
		baseRemark = inbound.Name
	}
	if displayTemplate != "" {
		return renderSubscriptionDisplayTemplate(displayTemplate, map[string]any{
			"stack":    stackName,
			"inbound":  inbound.Name,
			"protocol": inbound.Protocol,
			"port":     inbound.Port,
			"user":     user,
			"profile":  domain.NormalizeUserProfile(profile),
			"remark":   baseRemark,
		})
	}
	if configuredRemark != "" {
		return configuredRemark, nil
	}
	return fmt.Sprintf("%s %s", stackName, inbound.Protocol), nil
}

// renderSubscriptionDisplayTemplate 用订阅展示名模板的受限函数集渲染文本。
func renderSubscriptionDisplayTemplate(templateText string, data map[string]any) (string, error) {
	parsedTemplate, err := template.New("display_template").Funcs(subscriptionDisplayTemplateFuncs).Option("missingkey=error").Parse(templateText)
	if err != nil {
		return "", fmt.Errorf("invalid display_template: %s", err.Error())
	}
	var buffer bytes.Buffer
	if err := parsedTemplate.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("invalid display_template: %s", err.Error())
	}
	displayName := strings.TrimSpace(buffer.String())
	if displayName == "" {
		return "", fmt.Errorf("display_template rendered empty")
	}
	return displayName, nil
}

// firstNonEmpty 返回第一个非空字符串，用于订阅字段覆盖优先级。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ValidateUniqueStackNames 校验所有 stack 名称唯一。
func ValidateUniqueStackNames(stacks []domain.Stack) []Issue {
	issues := make([]Issue, 0)
	seen := make(map[string]string, len(stacks))
	for _, stack := range stacks {
		path := stack.Name
		if stack.SourcePath != "" {
			path = stack.SourcePath
		}
		if firstPath, ok := seen[stack.Name]; ok {
			issues = append(issues, Issue{
				Path:    "stacks." + stack.Name + ".name",
				Message: "duplicate stack name, first seen in " + firstPath,
			})
			continue
		}
		seen[stack.Name] = path
	}
	return issues
}

// ValidatePublicInboundAuth 校验公开 socks/http inbound 必须启用密码鉴权。
func ValidatePublicInboundAuth(config domain.GlobalConfig, stacks []domain.Stack) []Issue {
	if !config.Security.RequireAuthForPublicSocksHTTP || config.Security.AllowNoAuthPublic {
		return nil
	}
	issues := make([]Issue, 0)
	for _, stack := range stacks {
		if !stack.Enabled || !stack.Xray.Enabled {
			continue
		}
		for inboundIndex, inbound := range stack.Xray.Inbounds {
			if inbound.Protocol != "socks5" && inbound.Protocol != "http" {
				continue
			}
			if domain.IsLoopbackHost(inbound.Listen) {
				continue
			}
			if inbound.Auth != nil && inbound.Auth.Type == "password" {
				continue
			}
			issues = append(issues, Issue{
				Path:    fmt.Sprintf("stacks.%s.xray.inbounds[%d].auth", stack.Name, inboundIndex),
				Message: "public socks/http inbound requires password auth",
			})
		}
	}
	return issues
}

// CollectPortBindings 收集所有本地监听端口。
func CollectPortBindings(stackSet domain.StackSet) []PortBinding {
	bindings := make([]PortBinding, 0)
	for _, stack := range stackSet.Stacks {
		if !stack.Enabled {
			continue
		}
		if stack.Xray.Enabled {
			for inboundIndex, inbound := range stack.Xray.Inbounds {
				bindings = append(bindings, PortBinding{
					Host: inbound.Listen,
					Port: inbound.Port,
					Path: fmt.Sprintf("stacks.%s.xray.inbounds[%d].port", stack.Name, inboundIndex),
				})
			}
			apiConfig := domain.ResolveXrayAPIConfig(stackSet.Config.Defaults.Xray, stack.Xray)
			if apiConfig.Enabled {
				apiHost, apiPort, err := domain.ParseListen(apiConfig.Listen)
				if err == nil {
					bindings = append(bindings, PortBinding{
						Host: apiHost,
						Port: apiPort,
						Path: fmt.Sprintf("stacks.%s.xray.api.listen", stack.Name),
					})
				}
			}
		}
		if stack.Clash.Enabled {
			for listenerIndex, listener := range stack.Clash.Listeners.Socks {
				bindings = append(bindings, PortBinding{
					Host: listener.Listen,
					Port: listener.Port,
					Path: fmt.Sprintf("stacks.%s.clash.listeners.socks[%d].port", stack.Name, listenerIndex),
				})
			}
			for listenerIndex, listener := range stack.Clash.Listeners.HTTP {
				bindings = append(bindings, PortBinding{
					Host: listener.Listen,
					Port: listener.Port,
					Path: fmt.Sprintf("stacks.%s.clash.listeners.http[%d].port", stack.Name, listenerIndex),
				})
			}
			controllerHost, controllerPort, err := domain.ParseListen(stack.Clash.Controller.Listen)
			if err == nil {
				bindings = append(bindings, PortBinding{
					Host: controllerHost,
					Port: controllerPort,
					Path: fmt.Sprintf("stacks.%s.clash.controller.listen", stack.Name),
				})
			}
		}
	}
	return bindings
}

// ValidateUniquePorts 校验本地监听端口在所有 stack 中全局唯一。
func ValidateUniquePorts(bindings []PortBinding) []Issue {
	issues := make([]Issue, 0)
	seen := make(map[int]PortBinding)
	for _, binding := range bindings {
		first, ok := seen[binding.Port]
		if ok {
			issues = append(issues, Issue{
				Path:    binding.Path,
				Message: fmt.Sprintf("duplicate listen port %d, first seen at %s", binding.Port, first.Path),
			})
			continue
		}
		seen[binding.Port] = binding
	}
	return issues
}

// ValidateSystemPortsAvailable 校验配置声明的监听端口当前未被系统占用。
func ValidateSystemPortsAvailable(bindings []PortBinding, checker PortChecker) []Issue {
	issues := make([]Issue, 0)
	checked := make(map[string]bool)
	for _, binding := range bindings {
		key := fmt.Sprintf("%s:%d", binding.Host, binding.Port)
		if checked[key] {
			continue
		}
		checked[key] = true
		if checker.IsAvailable(binding.Host, binding.Port) {
			continue
		}
		issues = append(issues, Issue{
			Path:    binding.Path,
			Message: fmt.Sprintf("listen port %d is already in use", binding.Port),
		})
	}
	return issues
}

// ValidateReferenceGraph 将引用图问题转换为配置校验问题。
func ValidateReferenceGraph(stackSet domain.StackSet) []Issue {
	result := graph.CompileReferenceGraph(stackSet)
	issues := make([]Issue, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issues = append(issues, Issue{Path: issue.Path, Message: issue.Message})
	}
	return issues
}
