package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"sort"
	"strings"

	configloader "github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"github.com/eagle/proxystack-go/internal/systemd"
)

const (
	DefaultLaunchdDir = "/Library/LaunchDaemons"
	LaunchdSubLabel   = "com.proxystack.sub"
)

// LaunchdManager 封装 macOS launchd plist 和 launchctl 操作。
type LaunchdManager struct {
	Runner systemd.Runner
	Dir    string
}

// InstallUnits 写入 launchd plist 文件。
func (m LaunchdManager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	plists, err := RenderLaunchdPlists(config, target)
	if err != nil {
		return nil, err
	}
	written := make([]string, 0, len(plists))
	for name, content := range plists {
		path := filepath.Join(m.launchdDir(), name)
		if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	if err := m.removeStaleLaunchdPlists(config, target, plists); err != nil {
		return nil, err
	}
	sort.Strings(written)
	return written, nil
}

// UninstallUnits 删除 launchd plist 文件。
func (m LaunchdManager) UninstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	names, err := m.selectLaunchdPlists(config, target)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(names))
	for _, name := range names {
		if err := m.bootoutIfLoaded(context.Background(), strings.TrimSuffix(name, ".plist")); err != nil {
			return nil, err
		}
		path := filepath.Join(m.launchdDir(), name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		removed = append(removed, path)
	}
	sort.Strings(removed)
	return removed, nil
}

// Start 使用 launchctl bootstrap/kickstart 幂等启动服务。
func (m LaunchdManager) Start(ctx context.Context, services []string) error {
	for _, label := range services {
		active, err := m.IsActive(ctx, label)
		if err != nil {
			return err
		}
		if active {
			continue
		}
		loaded, err := m.isLoaded(ctx, label)
		if err != nil {
			return err
		}
		if !loaded {
			if err := m.launchctl(ctx, "bootstrap", "system", m.plistPath(label)); err != nil {
				return err
			}
		}
		if err := m.launchctl(ctx, "kickstart", launchdDomain(label)); err != nil {
			return err
		}
	}
	return nil
}

// Stop 使用 launchctl bootout 幂等停止并卸载服务。
func (m LaunchdManager) Stop(ctx context.Context, services []string) error {
	for _, label := range services {
		loaded, err := m.isLoaded(ctx, label)
		if err != nil {
			return err
		}
		if !loaded {
			continue
		}
		if err := m.launchctl(ctx, "bootout", launchdDomain(label)); err != nil {
			return err
		}
	}
	return nil
}

// Restart 使用 launchctl kickstart 重启服务，未加载时先 bootstrap。
func (m LaunchdManager) Restart(ctx context.Context, services []string) error {
	for _, label := range services {
		loaded, err := m.isLoaded(ctx, label)
		if err != nil {
			return err
		}
		if !loaded {
			if err := m.launchctl(ctx, "bootstrap", "system", m.plistPath(label)); err != nil {
				return err
			}
		}
		if err := m.launchctl(ctx, "kickstart", "-k", launchdDomain(label)); err != nil {
			return err
		}
	}
	return nil
}

// Enable 使用 launchctl enable 设置 launchd 持久启用状态。
func (m LaunchdManager) Enable(ctx context.Context, services []string) error {
	for _, label := range services {
		if err := m.launchctl(ctx, "enable", launchdDomain(label)); err != nil {
			return err
		}
	}
	return nil
}

// Disable 使用 launchctl disable 设置 launchd 持久禁用状态。
func (m LaunchdManager) Disable(ctx context.Context, services []string) error {
	for _, label := range services {
		if err := m.launchctl(ctx, "disable", launchdDomain(label)); err != nil {
			return err
		}
	}
	return nil
}

// IsActive 使用 launchctl print 判断服务是否实际运行。
func (m LaunchdManager) IsActive(ctx context.Context, service string) (bool, error) {
	result, err := m.run(ctx, "launchctl", "print", launchdDomain(service))
	if err != nil {
		return false, err
	}
	if result.ExitCode != 0 {
		if launchdPrintIsMissing(result) {
			return false, nil
		}
		return false, systemd.CommandError{Name: "launchctl", Args: []string{"print", launchdDomain(service)}, Result: result}
	}
	return launchdPrintIsRunning(result.Stdout), nil
}

// Status 调用 launchctl print 输出服务状态。
func (m LaunchdManager) Status(ctx context.Context, services []string) (Result, error) {
	return m.runForServices(ctx, services, func(label string) (Result, error) {
		args := []string{"print", launchdDomain(label)}
		result, err := m.run(ctx, "launchctl", args...)
		if err != nil {
			return result, err
		}
		if result.ExitCode != 0 {
			return result, systemd.CommandError{Name: "launchctl", Args: args, Result: result}
		}
		return result, nil
	})
}

// Logs 调用 macOS log 工具查询相关进程日志。
func (m LaunchdManager) Logs(ctx context.Context, services []string, follow bool) (Result, error) {
	predicate := launchdLogPredicate(services)
	args := []string{"show", "--last", "1h", "--style", "compact", "--predicate", predicate}
	if follow {
		args = []string{"stream", "--style", "compact", "--predicate", predicate}
	}
	result, err := m.run(ctx, "log", args...)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, systemd.CommandError{Name: "log", Args: args, Result: result}
	}
	return result, nil
}

// ServiceForNode 返回服务节点对应的 launchd label。
func (m LaunchdManager) ServiceForNode(node graph.ServiceNode) string {
	if node.Component == "xrelay" {
		return LaunchdXrayLabel(node.Stack)
	}
	return LaunchdMihomoLabel(node.Stack)
}

// ServicesForNodes 返回服务节点对应的 launchd label 列表。
func (m LaunchdManager) ServicesForNodes(nodes []graph.ServiceNode) []string {
	services := make([]string, 0, len(nodes))
	for _, node := range nodes {
		services = append(services, m.ServiceForNode(node))
	}
	return services
}

// SubService 返回订阅服务对应的 launchd label。
func (m LaunchdManager) SubService() string {
	return LaunchdSubLabel
}

// RenderLaunchdPlists 根据配置和 target 渲染 launchd plist 文件内容。
func RenderLaunchdPlists(config domain.GlobalConfig, target string) (map[string]string, error) {
	selected := map[string]string{}
	if target == "sub" && config.ConfigPath == "" {
		selected[LaunchdSubLabel+".plist"] = renderLaunchdPlist(LaunchdSubLabel, []string{"/usr/local/bin/pssub", "--base-dir", launchdBaseDir(config), "serve"}, launchdBaseDir(config))
		return selected, nil
	}
	nodes, err := launchdTargetNodes(config, target)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		addLaunchdNodePlist(selected, config, node)
	}
	return selected, nil
}

// launchdTargetNodes 解析 stack target 到 launchd plist 需要覆盖的组件集合。
func launchdTargetNodes(config domain.GlobalConfig, target string) ([]graph.ServiceNode, error) {
	stackSet, err := configloader.LoadStacks(config, false)
	if err != nil {
		return nil, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return nil, err
	}
	scope, err := graph.ResolveTargetScope(referenceGraph, target)
	if err != nil {
		return nil, err
	}
	return scope.Nodes, nil
}

// LaunchdXrayLabel 返回指定 stack 的 xray launchd label。
func LaunchdXrayLabel(stack string) string {
	return "com.proxystack.xray." + stack
}

// LaunchdMihomoLabel 返回指定 stack 的 mihomo launchd label。
func LaunchdMihomoLabel(stack string) string {
	return "com.proxystack.mihomo." + stack
}

// addLaunchdNodePlist 按单个 stack 组件追加 xray/mihomo plist。
func addLaunchdNodePlist(selected map[string]string, config domain.GlobalConfig, node graph.ServiceNode) {
	switch node.Component {
	case "xrelay":
		label := LaunchdXrayLabel(node.Stack)
		selected[label+".plist"] = renderLaunchdPlist(label, []string{
			filepath.Join(launchdBinDir(config), "xray"),
			"run",
			"-config",
			filepath.Join(launchdGeneratedDir(config), "xray", node.Stack+".json"),
		}, launchdBaseDir(config))
	case "clash":
		label := LaunchdMihomoLabel(node.Stack)
		selected[label+".plist"] = renderLaunchdPlist(label, []string{
			filepath.Join(launchdBinDir(config), "mihomo"),
			"-d",
			filepath.Join(launchdRuntimeDir(config), "mihomo", node.Stack),
			"-f",
			filepath.Join(launchdGeneratedDir(config), "mihomo", node.Stack+".yaml"),
		}, launchdBaseDir(config))
	}
}

// renderLaunchdPlist 生成 launchd 可加载的 plist XML。
func renderLaunchdPlist(label string, args []string, workingDirectory string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	builder.WriteString(xmlText(label))
	builder.WriteString(`</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, arg := range args {
		builder.WriteString("		<string>")
		builder.WriteString(xmlText(arg))
		builder.WriteString("</string>\n")
	}
	builder.WriteString(`	</array>
	<key>WorkingDirectory</key>
	<string>`)
	builder.WriteString(xmlText(workingDirectory))
	builder.WriteString(`</string>
	<key>RunAtLoad</key>
	<false/>
</dict>
</plist>
`)
	return builder.String()
}

// selectLaunchdPlists 返回 uninstall 需要删除的 plist 文件名。
func (m LaunchdManager) selectLaunchdPlists(config domain.GlobalConfig, target string) ([]string, error) {
	if target == "sub" && config.ConfigPath == "" {
		return []string{LaunchdSubLabel + ".plist"}, nil
	}
	patterns, err := managedLaunchdPatterns(config, target)
	if err != nil {
		return nil, err
	}
	return m.expandLaunchdPlistPatterns(patterns), nil
}

// expandLaunchdPlistPatterns 展开 plist glob，并保留非 glob 的固定文件名。
func (m LaunchdManager) expandLaunchdPlistPatterns(patterns []string) []string {
	names := make([]string, 0, len(patterns))
	seen := map[string]bool{}
	for _, pattern := range patterns {
		if !strings.ContainsAny(pattern, "*?[") {
			if !seen[pattern] {
				names = append(names, pattern)
				seen[pattern] = true
			}
			continue
		}
		matches, err := filepath.Glob(filepath.Join(m.launchdDir(), pattern))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, match := range matches {
			name := filepath.Base(match)
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	return names
}

// removeStaleLaunchdPlists 删除当前 target 下不再由配置生成的旧 plist。
func (m LaunchdManager) removeStaleLaunchdPlists(config domain.GlobalConfig, target string, expected map[string]string) error {
	patterns, err := managedLaunchdPatterns(config, target)
	if err != nil {
		return err
	}
	expectedNames := make(map[string]bool, len(expected))
	for name := range expected {
		expectedNames[name] = true
	}
	for _, name := range m.expandLaunchdPlistPatterns(patterns) {
		if expectedNames[name] {
			continue
		}
		if err := m.bootoutIfLoaded(context.Background(), strings.TrimSuffix(name, ".plist")); err != nil {
			return err
		}
		path := filepath.Join(m.launchdDir(), name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// managedLaunchdPatterns 返回 target 对应的 proxystack plist 管理范围。
func managedLaunchdPatterns(config domain.GlobalConfig, target string) ([]string, error) {
	if target == "sub" && config.ConfigPath == "" {
		return []string{LaunchdSubLabel + ".plist"}, nil
	}
	if target == "" {
		return []string{"com.proxystack.xray.*.plist", "com.proxystack.mihomo.*.plist"}, nil
	}
	nodes, err := launchdTargetNodes(config, target)
	if err != nil {
		return nil, err
	}
	patterns := make([]string, 0, len(nodes))
	for _, node := range nodes {
		switch node.Component {
		case "xrelay":
			patterns = append(patterns, LaunchdXrayLabel(node.Stack)+".plist")
		case "clash":
			patterns = append(patterns, LaunchdMihomoLabel(node.Stack)+".plist")
		}
	}
	return patterns, nil
}

// runForServices 对多个 launchd label 逐一执行命令并合并输出。
func (m LaunchdManager) runForServices(ctx context.Context, services []string, run func(string) (Result, error)) (Result, error) {
	var merged Result
	for _, label := range services {
		result, err := run(label)
		merged.Stdout += result.Stdout
		merged.Stderr += result.Stderr
		merged.ExitCode = result.ExitCode
		if err != nil {
			return merged, err
		}
	}
	return merged, nil
}

// isLoaded 使用 launchctl print 判断服务是否已经 bootstrap。
func (m LaunchdManager) isLoaded(ctx context.Context, label string) (bool, error) {
	result, err := m.run(ctx, "launchctl", "print", launchdDomain(label))
	if err != nil {
		return false, err
	}
	if result.ExitCode == 0 {
		return true, nil
	}
	if launchdPrintIsMissing(result) {
		return false, nil
	}
	return false, systemd.CommandError{Name: "launchctl", Args: []string{"print", launchdDomain(label)}, Result: result}
}

// bootoutIfLoaded 卸载已加载的旧 launchd job，未加载时忽略。
func (m LaunchdManager) bootoutIfLoaded(ctx context.Context, label string) error {
	loaded, err := m.isLoaded(ctx, label)
	if err != nil {
		return err
	}
	if !loaded {
		return nil
	}
	return m.launchctl(ctx, "bootout", launchdDomain(label))
}

// launchctl 执行 launchctl 并把非零退出转换为命令错误。
func (m LaunchdManager) launchctl(ctx context.Context, args ...string) error {
	result, err := m.run(ctx, "launchctl", args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return systemd.CommandError{Name: "launchctl", Args: args, Result: result}
	}
	return nil
}

// run 执行外部命令，测试可通过 Runner 注入 fake 实现。
func (m LaunchdManager) run(ctx context.Context, name string, args ...string) (Result, error) {
	runner := m.Runner
	if runner == nil {
		runner = systemd.CommandRunner{}
	}
	return runner.Run(ctx, name, args...)
}

// plistPath 返回 label 对应的 plist 文件路径。
func (m LaunchdManager) plistPath(label string) string {
	return filepath.Join(m.launchdDir(), label+".plist")
}

// launchdDir 返回 plist 安装目录。
func (m LaunchdManager) launchdDir() string {
	if m.Dir != "" {
		return m.Dir
	}
	return DefaultLaunchdDir
}

// launchdDomain 返回 launchctl 接受的 system domain label。
func launchdDomain(label string) string {
	return "system/" + label
}

// launchdPrintIsRunning 从 launchctl print 输出中判断 job 是否运行。
func launchdPrintIsRunning(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "state = running" {
			return true
		}
		if strings.HasPrefix(trimmed, "pid = ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "pid = ")) != "0" {
			return true
		}
	}
	return false
}

// launchdPrintIsMissing 判断 launchctl print 的非零结果是否只是服务未加载。
func launchdPrintIsMissing(result Result) bool {
	output := strings.ToLower(result.Stdout + "\n" + result.Stderr)
	return strings.Contains(output, "could not find service") ||
		strings.Contains(output, "service is not loaded") ||
		strings.Contains(output, "no such process")
}

// launchdLogPredicate 为 macOS log 工具构造进程过滤表达式。
func launchdLogPredicate(services []string) string {
	processes := map[string]bool{}
	for _, label := range services {
		switch {
		case label == LaunchdSubLabel:
			processes["pssub"] = true
		case strings.HasPrefix(label, "com.proxystack.xray."):
			processes["xray"] = true
		case strings.HasPrefix(label, "com.proxystack.mihomo."):
			processes["mihomo"] = true
		}
	}
	if len(processes) == 0 {
		return `process == "proxystack"`
	}
	names := make([]string, 0, len(processes))
	for name := range processes {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, `process == "`+name+`"`)
	}
	return strings.Join(parts, " OR ")
}

// launchdBaseDir 返回配置对应的运行根目录。
func launchdBaseDir(config domain.GlobalConfig) string {
	return firstLaunchdPath(config.BaseDir, "/opt/proxystack")
}

// launchdBinDir 返回托管二进制目录。
func launchdBinDir(config domain.GlobalConfig) string {
	return resolveLaunchdPath(config, config.Paths.Bin, filepath.Join(launchdBaseDir(config), "bin"))
}

// launchdRuntimeDir 返回 runtime 目录。
func launchdRuntimeDir(config domain.GlobalConfig) string {
	return resolveLaunchdPath(config, config.Paths.Runtime, filepath.Join(launchdBaseDir(config), "runtime"))
}

// launchdGeneratedDir 返回生成配置目录。
func launchdGeneratedDir(config domain.GlobalConfig) string {
	return resolveLaunchdPath(config, config.Paths.Generated, filepath.Join(launchdBaseDir(config), "runtime", "generated"))
}

// resolveLaunchdPath 按配置路径解析 launchd plist 中使用的绝对路径。
func resolveLaunchdPath(config domain.GlobalConfig, pathValue string, fallback string) string {
	if config.BaseDir == "" || pathValue == "" {
		return fallback
	}
	return config.ResolvePath(pathValue)
}

// firstLaunchdPath 返回第一个非空路径。
func firstLaunchdPath(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// xmlText 转义 plist XML 文本节点。
func xmlText(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

// writeFileAtomic 以临时文件和 rename 原子写入 plist。
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
