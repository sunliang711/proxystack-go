package systemd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	configloader "github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
)

const (
	XrayUnitTemplate    = "proxystack-xray@.service"
	ClashUnitTemplate   = "proxystack-clash@.service"
	SubUnit             = "proxystack-sub.service"
	DefaultUnitDir      = "/etc/systemd/system"
	DefaultServiceUser  = "proxystack"
	DefaultServiceGroup = "proxystack"
)

// Result 保存一次 systemctl/journalctl 调用的输出。
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// MetadataRule 描述一条标准权限修复规则。
type MetadataRule struct {
	Path string
	Mode os.FileMode
}

// Runner 抽象外部命令执行，测试可注入 fake runner。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// CommandRunner 使用 os/exec 执行真实外部命令。
type CommandRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Run 使用参数数组执行命令并捕获 stdout/stderr。
func (runner CommandRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if shouldStreamCommandOutput(name, args) {
		// journalctl --follow 是长驻命令，必须直接透传输出，否则日志会一直留在内存 buffer 中。
		command.Stdout = firstWriter(runner.Stdout, os.Stdout)
		command.Stderr = firstWriter(runner.Stderr, os.Stderr)
	} else {
		command.Stdout = &stdout
		command.Stderr = &stderr
	}
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		result.ExitCode = -1
		return result, err
	}
	return result, nil
}

// shouldStreamCommandOutput 判断外部命令是否需要边执行边输出。
func shouldStreamCommandOutput(name string, args []string) bool {
	switch filepath.Base(name) {
	case "journalctl":
		for _, arg := range args {
			if arg == "-f" || arg == "--follow" {
				return true
			}
		}
		return false
	case "log":
		return len(args) > 0 && args[0] == "stream"
	default:
		return false
	}
}

// firstWriter 返回首个非空 writer，用于保持测试可注入和真实终端输出。
func firstWriter(writers ...io.Writer) io.Writer {
	for _, writer := range writers {
		if writer != nil {
			return writer
		}
	}
	return io.Discard
}

// Manager 封装 systemctl、journalctl 和 unit 文件管理。
type Manager struct {
	Runner  Runner
	UnitDir string
}

// NewManager 创建使用真实命令的 systemd manager。
func NewManager() Manager {
	return Manager{Runner: CommandRunner{}, UnitDir: DefaultUnitDir}
}

// UnitForNode 返回依赖图节点对应的 systemd unit 名。
func UnitForNode(node graph.ServiceNode) string {
	return node.ServiceName()
}

// UnitsForNodes 把服务节点转换为稳定 unit 列表。
func UnitsForNodes(nodes []graph.ServiceNode) []string {
	units := make([]string, 0, len(nodes))
	for _, node := range nodes {
		units = append(units, UnitForNode(node))
	}
	return units
}

// Start 调用 systemctl start。
func (m Manager) Start(ctx context.Context, units []string) error {
	return m.systemctl(ctx, "start", units...)
}

// Stop 调用 systemctl stop。
func (m Manager) Stop(ctx context.Context, units []string) error {
	return m.systemctl(ctx, "stop", units...)
}

// Restart 调用 systemctl restart。
func (m Manager) Restart(ctx context.Context, units []string) error {
	return m.systemctl(ctx, "restart", units...)
}

// Enable 调用 systemctl enable。
func (m Manager) Enable(ctx context.Context, units []string) error {
	return m.systemctl(ctx, "enable", units...)
}

// Disable 调用 systemctl disable。
func (m Manager) Disable(ctx context.Context, units []string) error {
	return m.systemctl(ctx, "disable", units...)
}

// IsActive 使用 systemctl is-active --quiet 判断 unit 是否处于 active。
func (m Manager) IsActive(ctx context.Context, unit string) (bool, error) {
	result, err := m.run(ctx, "systemctl", "is-active", "--quiet", unit)
	if err != nil {
		return false, err
	}
	if result.ExitCode == 0 {
		return true, nil
	}
	if result.ExitCode == 3 || result.ExitCode == 4 {
		return false, nil
	}
	return false, CommandError{Name: "systemctl", Args: []string{"is-active", "--quiet", unit}, Result: result}
}

// Status 调用 systemctl status，并把 inactive 的退出码 3 视为可展示状态。
func (m Manager) Status(ctx context.Context, units []string) (Result, error) {
	result, err := m.run(ctx, "systemctl", append([]string{"status"}, units...)...)
	if err != nil {
		return result, err
	}
	if result.ExitCode == 0 || result.ExitCode == 3 {
		return result, nil
	}
	return result, CommandError{Name: "systemctl", Args: append([]string{"status"}, units...), Result: result}
}

// Logs 对多个 unit 使用一次 journalctl 调用。
func (m Manager) Logs(ctx context.Context, units []string, follow bool) (Result, error) {
	args := make([]string, 0, len(units)*2+4)
	for _, unit := range units {
		args = append(args, "-u", unit)
	}
	args = append(args, "--no-pager", "-n", "100")
	if follow {
		args = append(args, "-f")
	}
	result, err := m.run(ctx, "journalctl", args...)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, CommandError{Name: "journalctl", Args: args, Result: result}
	}
	return result, nil
}

// InstallUnits 渲染并写入指定 target 的 unit 文件。
func (m Manager) InstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	units := RenderUnits(config)
	selected, err := selectUnitFiles(config, units, target)
	if err != nil {
		return nil, err
	}
	written := make([]string, 0, len(selected))
	for name, content := range selected {
		path := filepath.Join(m.unitDir(), name)
		if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	sort.Strings(written)
	if len(written) == 0 {
		return written, nil
	}
	if err := m.systemctl(context.Background(), "daemon-reload"); err != nil {
		return written, fmt.Errorf("systemd daemon-reload failed: %w", err)
	}
	return written, nil
}

// UninstallUnits 删除指定 target 的 unit 文件。
func (m Manager) UninstallUnits(config domain.GlobalConfig, target string) ([]string, error) {
	selected, err := selectUnitFilesForUninstall(config, RenderUnits(config), target)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(selected))
	for name := range selected {
		path := filepath.Join(m.unitDir(), name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		removed = append(removed, path)
	}
	sort.Strings(removed)
	return removed, nil
}

// RenderUnits 根据全局配置渲染三个 systemd unit 模板。
func RenderUnits(config domain.GlobalConfig) map[string]string {
	baseDir := firstNonEmpty(config.BaseDir, "/opt/proxystack")
	binDir := resolvePath(config, config.Paths.Bin, filepath.Join(baseDir, "bin"))
	runtimeDir := resolvePath(config, config.Paths.Runtime, filepath.Join(baseDir, "runtime"))
	generatedDir := resolvePath(config, config.Paths.Generated, filepath.Join(baseDir, "runtime", "generated"))
	subDir := resolvePath(config, config.Paths.Sub, filepath.Join(baseDir, "sub"))
	return map[string]string{
		XrayUnitTemplate: `[Unit]
Description=Proxystack Xray %i
After=network-online.target
Wants=network-online.target

[Service]
User=proxystack
Group=proxystack
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=` + runtimeDir + `
ExecStart=` + filepath.Join(binDir, "xray") + ` run -config ` + filepath.Join(generatedDir, "xray", "%i.json") + `
Restart=on-failure

[Install]
WantedBy=multi-user.target
`,
		ClashUnitTemplate: `[Unit]
Description=Proxystack mihomo %i
After=network-online.target
Wants=network-online.target

[Service]
User=proxystack
Group=proxystack
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=` + runtimeDir + `
ExecStart=` + filepath.Join(binDir, "mihomo") + ` -d ` + filepath.Join(runtimeDir, "mihomo", "%i") + ` -f ` + filepath.Join(generatedDir, "mihomo", "%i.yaml") + `
Restart=on-failure

[Install]
WantedBy=multi-user.target
`,
		SubUnit: `[Unit]
Description=Proxystack subscription server
After=network-online.target
Wants=network-online.target

[Service]
User=proxystack
Group=proxystack
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=` + subDir + `
ExecStart=/usr/local/bin/pssub --base-dir ` + baseDir + ` serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
`,
	}
}

// MetadataFixer 抽象 chmod/chown，测试可注入 fake 实现。
type MetadataFixer struct {
	Chmod func(path string, mode os.FileMode) error
	Chown func(path string, uid int, gid int) error
}

// Fix 修复单个路径的权限和可选 owner。
func (f MetadataFixer) Fix(path string, mode os.FileMode, uid int, gid int) error {
	chmod := f.Chmod
	if chmod == nil {
		chmod = os.Chmod
	}
	if err := chmod(path, mode); err != nil {
		return err
	}
	if uid < 0 || gid < 0 {
		return nil
	}
	chown := f.Chown
	if chown == nil {
		chown = os.Chown
	}
	return chown(path, uid, gid)
}

// FixPermissions 按 proxystack 安全规格修复目录或文件权限。
func FixPermissions(path string, mode os.FileMode) error {
	return (MetadataFixer{}).Fix(path, mode, -1, -1)
}

// StandardMetadataRules 返回 proxystack 标准目录和文件权限规则。
func StandardMetadataRules(config domain.GlobalConfig) []MetadataRule {
	rules := []MetadataRule{
		{Path: config.BaseDir, Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Bin), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Geo), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Stacks), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Runtime), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Generated), Mode: 0o750},
		{Path: filepath.Join(config.ResolvePath(config.Paths.Generated), "xray"), Mode: 0o750},
		{Path: filepath.Join(config.ResolvePath(config.Paths.Generated), "mihomo"), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Publish), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Downloads), Mode: 0o750},
		{Path: config.ResolvePath(config.Paths.Sub), Mode: 0o750},
	}
	if config.ConfigPath != "" {
		rules = append(rules, MetadataRule{Path: config.ConfigPath, Mode: 0o640})
	}
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Stacks), "*.yaml"), 0o640)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Bin), "*"), 0o750)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Geo), "*"), 0o640)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Generated), "xray", "*.json"), 0o640)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Generated), "mihomo", "*.yaml"), 0o640)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Runtime), "manifest.json"), 0o640)
	rules = appendGlobRules(rules, filepath.Join(config.ResolvePath(config.Paths.Runtime), "disabled.json"), 0o640)
	return rules
}

// RepairStandardMetadata 修复已存在标准路径的 mode 和 owner。
func RepairStandardMetadata(config domain.GlobalConfig, uid int, gid int, fixer MetadataFixer) error {
	for _, rule := range StandardMetadataRules(config) {
		if _, err := os.Stat(rule.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := fixer.Fix(rule.Path, rule.Mode, uid, gid); err != nil {
			return err
		}
	}
	return RepairSubMetadata(config, uid, gid, fixer)
}

// SubMetadataRules 返回订阅运行目录下所有已存在目录和文件的权限规则。
func SubMetadataRules(config domain.GlobalConfig) ([]MetadataRule, error) {
	subDir := config.ResolvePath(config.Paths.Sub)
	if _, err := os.Stat(subDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	rules := make([]MetadataRule, 0)
	err := filepath.WalkDir(subDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		mode := os.FileMode(0o640)
		if entry.IsDir() {
			mode = 0o750
		}
		rules = append(rules, MetadataRule{Path: path, Mode: mode})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// RepairSubMetadata 递归修复订阅运行目录下已存在路径的 mode 和 owner。
func RepairSubMetadata(config domain.GlobalConfig, uid int, gid int, fixer MetadataFixer) error {
	rules, err := SubMetadataRules(config)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if err := fixer.Fix(rule.Path, rule.Mode, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// ParseOwnerIDs 把 uid/gid 文本解析为 chown 可用的数字 ID。
func ParseOwnerIDs(uidText string, gidText string) (int, int, error) {
	uid, err := strconv.Atoi(uidText)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(gidText)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// CommandError 表示 systemctl/journalctl 非零退出。
type CommandError struct {
	Name   string
	Args   []string
	Result Result
}

// Error 输出命令、退出码和 stdout/stderr 摘要。
func (e CommandError) Error() string {
	parts := []string{fmt.Sprintf("%s failed with exit code %d", e.Name, e.Result.ExitCode)}
	if strings.TrimSpace(e.Result.Stdout) != "" {
		parts = append(parts, "stdout: "+strings.TrimSpace(e.Result.Stdout))
	}
	if strings.TrimSpace(e.Result.Stderr) != "" {
		parts = append(parts, "stderr: "+strings.TrimSpace(e.Result.Stderr))
	}
	return strings.Join(parts, "; ")
}

func (m Manager) systemctl(ctx context.Context, action string, units ...string) error {
	result, err := m.run(ctx, "systemctl", append([]string{action}, units...)...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return CommandError{Name: "systemctl", Args: append([]string{action}, units...), Result: result}
	}
	return nil
}

func (m Manager) run(ctx context.Context, name string, args ...string) (Result, error) {
	runner := m.Runner
	if runner == nil {
		runner = CommandRunner{}
	}
	return runner.Run(ctx, name, args...)
}

func (m Manager) unitDir() string {
	if m.UnitDir != "" {
		return m.UnitDir
	}
	return DefaultUnitDir
}

func selectUnitFiles(config domain.GlobalConfig, units map[string]string, target string) (map[string]string, error) {
	selected := map[string]string{}
	if target == "sub" && config.ConfigPath == "" {
		return selectLegacyUnitFiles(units, target)
	}
	if target == "" {
		selected[XrayUnitTemplate] = units[XrayUnitTemplate]
		selected[ClashUnitTemplate] = units[ClashUnitTemplate]
		return selected, nil
	}
	nodes, err := unitTargetNodes(config, target)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		switch node.Component {
		case "xray":
			selected[XrayUnitTemplate] = units[XrayUnitTemplate]
		case "clash":
			selected[ClashUnitTemplate] = units[ClashUnitTemplate]
		}
	}
	return selected, nil
}

// selectUnitFilesForUninstall 选择卸载范围；systemd 模板被多个 stack 共享，非空 target 只删除无人再用的模板。
func selectUnitFilesForUninstall(config domain.GlobalConfig, units map[string]string, target string) (map[string]string, error) {
	if target == "" || (target == "sub" && config.ConfigPath == "") {
		return selectUnitFiles(config, units, target)
	}
	targetNodes, allNodes, err := unitTargetNodesWithAll(config, target)
	if err != nil {
		return nil, err
	}
	targetSet := serviceNodeSet(targetNodes)
	selected := map[string]string{}
	for _, node := range targetNodes {
		if componentUsedOutsideTarget(allNodes, targetSet, node.Component) {
			continue
		}
		switch node.Component {
		case "xray":
			selected[XrayUnitTemplate] = units[XrayUnitTemplate]
		case "clash":
			selected[ClashUnitTemplate] = units[ClashUnitTemplate]
		}
	}
	return selected, nil
}

// unitTargetNodes 解析 stack target 到 systemd 模板需要覆盖的组件集合。
func unitTargetNodes(config domain.GlobalConfig, target string) ([]graph.ServiceNode, error) {
	targetNodes, _, err := unitTargetNodesWithAll(config, target)
	return targetNodes, err
}

// unitTargetNodesWithAll 同时返回指定 target 和所有 enabled stack 的服务节点。
func unitTargetNodesWithAll(config domain.GlobalConfig, target string) ([]graph.ServiceNode, []graph.ServiceNode, error) {
	stackSet, err := configloader.LoadStacks(config, false)
	if err != nil {
		return nil, nil, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return nil, nil, err
	}
	scope, err := graph.ResolveTargetScope(referenceGraph, target)
	if err != nil {
		return nil, nil, err
	}
	allScope, err := graph.ResolveTargetScope(referenceGraph, "")
	if err != nil {
		return nil, nil, err
	}
	return scope.Nodes, allScope.Nodes, nil
}

// serviceNodeSet 把服务节点列表转为集合，方便比较 target 外引用。
func serviceNodeSet(nodes []graph.ServiceNode) map[graph.ServiceNode]bool {
	set := make(map[graph.ServiceNode]bool, len(nodes))
	for _, node := range nodes {
		set[node] = true
	}
	return set
}

// componentUsedOutsideTarget 判断同类 systemd 模板是否仍被 target 外的 enabled stack 使用。
func componentUsedOutsideTarget(nodes []graph.ServiceNode, targetSet map[graph.ServiceNode]bool, component string) bool {
	for _, node := range nodes {
		if node.Component == component && !targetSet[node] {
			return true
		}
	}
	return false
}

// selectLegacyUnitFiles 保留 pssub 对订阅服务 unit 的显式安装入口。
func selectLegacyUnitFiles(units map[string]string, target string) (map[string]string, error) {
	selected := map[string]string{}
	if target != "sub" {
		return nil, fmt.Errorf("unsupported unit target: %s", target)
	}
	selected[SubUnit] = units[SubUnit]
	return selected, nil
}

func resolvePath(config domain.GlobalConfig, pathValue string, fallback string) string {
	if config.BaseDir == "" || pathValue == "" {
		return fallback
	}
	return config.ResolvePath(pathValue)
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

func appendGlobRules(rules []MetadataRule, pattern string, mode os.FileMode) []MetadataRule {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return rules
	}
	sort.Strings(matches)
	for _, match := range matches {
		rules = append(rules, MetadataRule{Path: match, Mode: mode})
	}
	return rules
}
