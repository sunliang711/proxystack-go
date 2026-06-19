package service

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"github.com/eagle/proxystack-go/internal/systemd"
)

const (
	ManagerAuto    = "auto"
	ManagerSystemd = "systemd"
	ManagerLaunchd = "launchd"
)

// Result 保存一次服务管理命令调用的输出。
type Result = systemd.Result

// Manager 统一封装 systemd 和 launchd 的服务管理能力。
type Manager interface {
	InstallUnits(config domain.GlobalConfig, target string) ([]string, error)
	UninstallUnits(config domain.GlobalConfig, target string) ([]string, error)
	Start(ctx context.Context, services []string) error
	Stop(ctx context.Context, services []string) error
	Restart(ctx context.Context, services []string) error
	Enable(ctx context.Context, services []string) error
	Disable(ctx context.Context, services []string) error
	IsActive(ctx context.Context, service string) (bool, error)
	Status(ctx context.Context, services []string) (Result, error)
	Logs(ctx context.Context, services []string, follow bool) (Result, error)
	ServiceForNode(node graph.ServiceNode) string
	ServicesForNodes(nodes []graph.ServiceNode) []string
	SubService() string
}

type managerOptions struct {
	goos           string
	runner         systemd.Runner
	systemdUnitDir string
	launchdDir     string
}

// ManagerOption 调整服务管理器创建时的测试注入参数。
type ManagerOption func(*managerOptions)

// WithGOOS 覆盖平台识别，主要用于单元测试 auto 分支。
func WithGOOS(goos string) ManagerOption {
	return func(options *managerOptions) {
		options.goos = goos
	}
}

// WithRunner 注入外部命令 runner，主要用于测试 systemctl/launchctl 调用形态。
func WithRunner(runner systemd.Runner) ManagerOption {
	return func(options *managerOptions) {
		options.runner = runner
	}
}

// WithSystemdUnitDir 覆盖 systemd unit 目录，主要用于测试安装输出。
func WithSystemdUnitDir(unitDir string) ManagerOption {
	return func(options *managerOptions) {
		options.systemdUnitDir = unitDir
	}
}

// WithLaunchdDir 覆盖 launchd plist 目录，主要用于测试安装输出。
func WithLaunchdDir(dir string) ManagerOption {
	return func(options *managerOptions) {
		options.launchdDir = dir
	}
}

// NewManager 根据 kind 和当前平台创建服务管理器。
func NewManager(kind string, opts ...ManagerOption) (Manager, error) {
	options := managerOptions{goos: runtime.GOOS}
	for _, opt := range opts {
		opt(&options)
	}
	resolved, err := ResolveManagerKind(kind, options.goos)
	if err != nil {
		return nil, err
	}
	switch resolved {
	case ManagerSystemd:
		return SystemdManager{inner: systemd.Manager{Runner: options.runner, UnitDir: options.systemdUnitDir}}, nil
	case ManagerLaunchd:
		return LaunchdManager{Runner: options.runner, Dir: options.launchdDir}, nil
	default:
		return nil, fmt.Errorf("unsupported service manager: %s", resolved)
	}
}

// ResolveManagerKind 把 auto 解析为具体平台后端。
func ResolveManagerKind(kind string, goos string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		normalized = ManagerAuto
	}
	switch normalized {
	case ManagerAuto:
		switch goos {
		case "linux":
			return ManagerSystemd, nil
		case "darwin":
			return ManagerLaunchd, nil
		default:
			return "", fmt.Errorf("service manager auto is not supported on %s", goos)
		}
	case ManagerSystemd, ManagerLaunchd:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported service manager: %s", kind)
	}
}

// SystemdManager 适配原有 systemd.Manager 到统一服务管理接口。
type SystemdManager struct {
	inner systemd.Manager
}

// InstallUnits 写入 systemd unit 文件。
func (m SystemdManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	return m.inner.InstallUnits(config, target)
}

// UninstallUnits 删除 systemd unit 文件。
func (m SystemdManager) UninstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	return m.inner.UninstallUnits(config, target)
}

// Start 调用 systemctl start。
func (m SystemdManager) Start(ctx context.Context, services []string) error {
	return m.inner.Start(ctx, services)
}

// Stop 调用 systemctl stop。
func (m SystemdManager) Stop(ctx context.Context, services []string) error {
	return m.inner.Stop(ctx, services)
}

// Restart 调用 systemctl restart。
func (m SystemdManager) Restart(ctx context.Context, services []string) error {
	return m.inner.Restart(ctx, services)
}

// Enable 调用 systemctl enable。
func (m SystemdManager) Enable(ctx context.Context, services []string) error {
	return m.inner.Enable(ctx, services)
}

// Disable 调用 systemctl disable。
func (m SystemdManager) Disable(ctx context.Context, services []string) error {
	return m.inner.Disable(ctx, services)
}

// IsActive 使用 systemctl 判断服务是否 active。
func (m SystemdManager) IsActive(ctx context.Context, service string) (bool, error) {
	return m.inner.IsActive(ctx, service)
}

// Status 调用 systemctl status。
func (m SystemdManager) Status(ctx context.Context, services []string) (Result, error) {
	return m.inner.Status(ctx, services)
}

// Logs 调用 journalctl 查询日志。
func (m SystemdManager) Logs(ctx context.Context, services []string, follow bool) (Result, error) {
	return m.inner.Logs(ctx, services, follow)
}

// ServiceForNode 返回服务节点对应的 systemd unit 名。
func (m SystemdManager) ServiceForNode(node graph.ServiceNode) string {
	return systemd.UnitForNode(node)
}

// ServicesForNodes 返回服务节点对应的 systemd unit 列表。
func (m SystemdManager) ServicesForNodes(nodes []graph.ServiceNode) []string {
	return systemd.UnitsForNodes(nodes)
}

// SubService 返回订阅服务对应的 systemd unit 名。
func (m SystemdManager) SubService() string {
	return systemd.SubUnit
}
