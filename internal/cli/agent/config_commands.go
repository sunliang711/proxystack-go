package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/cli/draftedit"
	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/domain/validation"
	"github.com/eagle/proxystack-go/internal/graph"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newInitCommand 创建默认配置初始化命令。
func newInitCommand() *cobra.Command {
	var externalHost string
	var force bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize proxystack agent configuration",
		RunE: func(command *cobra.Command, args []string) error {
			baseDir, err := agentBaseDir(command)
			if err != nil {
				return err
			}
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if err := ensureServiceAccountForInit(context.Background(), baseDir); err != nil {
				return err
			}
			if err := agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: externalHost, Force: force}); err != nil {
				return err
			}
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return err
			}
			if err := repairServiceMetadata(cfg); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Initialized agent config: %s\n", configPath)
			return nil
		},
	}
	command.Flags().StringVar(&externalHost, "external-host", "", "External subscription host")
	command.Flags().BoolVar(&force, "force", false, "Overwrite existing config")
	return command
}

// newAddCommand 创建 stack 模板写入命令。
func newAddCommand() *cobra.Command {
	var template string
	var fromFile string
	var members string
	var allocatePorts bool
	var keepTemplatePorts bool
	var edit bool
	var noEdit bool
	var editor string
	command := &cobra.Command{
		Use:   "add NAME",
		Short: "Create a new stack from a template",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if keepTemplatePorts {
				allocatePorts = false
			}
			if noEdit && command.Flags().Changed("edit") {
				return fmt.Errorf("--edit and --no-edit cannot be used together")
			}
			options := agentconfig.AddOptions{
				ConfigPath:        configPath,
				Name:              args[0],
				Template:          template,
				FromFile:          fromFile,
				Members:           splitCSV(members),
				AllocatePorts:     allocatePorts,
				KeepTemplatePorts: keepTemplatePorts,
			}
			if noEdit {
				if err := agentconfig.AddStack(options); err != nil {
					return err
				}
			} else {
				if err := editNewStackCandidate(configPath, agentconfig.BuildAddStackCandidate, options, editor); err != nil {
					return err
				}
			}
			if err := repairServiceMetadataForConfigPath(configPath); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Created stack: %s\n", args[0])
			return nil
		},
	}
	command.Flags().StringVar(&template, "template", "pair", "Stack template: pair, auto-url-test, load-balance")
	command.Flags().StringVar(&fromFile, "from-file", "", "Use an existing stack YAML as template")
	command.Flags().StringVar(&members, "members", "", "Comma separated member stacks for auto templates")
	command.Flags().BoolVar(&allocatePorts, "allocate-ports", true, "Allocate fresh ports")
	command.Flags().BoolVar(&keepTemplatePorts, "keep-template-ports", false, "Keep ports from template")
	command.Flags().BoolVar(&edit, "edit", false, "Open editor before writing stack")
	command.Flags().BoolVar(&noEdit, "no-edit", false, "Write stack without opening editor")
	command.Flags().StringVar(&editor, "editor", "", "Editor command")
	return command
}

// newConfigCommand 创建配置校验或编辑命令。
func newConfigCommand() *cobra.Command {
	var editor string
	var checkOnly bool
	command := &cobra.Command{
		Use:   "config [NAME]",
		Short: "Edit or validate agent config files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			targetPath, changed, err := editConfigOrStack(configPath, optionalArg(args), len(args) > 0, editor, checkOnly)
			if err != nil {
				return err
			}
			if changed && len(args) > 0 {
				if err := restartActiveStack(command, configPath, args[0]); err != nil {
					return err
				}
			}
			fmt.Fprintf(command.OutOrStdout(), "Config OK: %s\n", targetPath)
			return nil
		},
	}
	command.Flags().StringVar(&editor, "editor", "", "Editor command")
	command.Flags().BoolVar(&checkOnly, "check-only", false, "Validate without opening editor")
	return command
}

// editConfigOrStack 通过草稿编辑配置，校验通过后再替换真实文件。
func editConfigOrStack(configPath string, name string, hasName bool, editor string, checkOnly bool) (string, bool, error) {
	targetPath, err := configEditTarget(configPath, name, hasName)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(targetPath); err != nil {
		if os.IsNotExist(err) {
			return "", false, fmt.Errorf("file does not exist: %s", targetPath)
		}
		return "", false, err
	}
	if checkOnly {
		return targetPath, false, validateEditTarget(targetPath, configPath, name, hasName)
	}
	original, err := os.ReadFile(targetPath)
	if err != nil {
		return "", false, err
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(targetPath); err == nil {
		mode = info.Mode().Perm()
	}
	result, err := draftedit.EditAndCommit(draftedit.Options{
		TargetPath: targetPath,
		Initial:    original,
		Mode:       mode,
		Editor:     editor,
	}, func(path string, data []byte) error {
		return validateEditTarget(path, configPath, name, hasName)
	}, func(data []byte) (bool, error) {
		return writeTextFileIfChanged(targetPath, data)
	})
	return targetPath, result.Changed, err
}

// editNewStackCandidate 打开新增 stack 草稿，校验通过后再写入真实 stack 文件。
func editNewStackCandidate(configPath string, build func(agentconfig.AddOptions) (agentconfig.StackCandidate, error), options agentconfig.AddOptions, editor string) error {
	candidate, err := build(options)
	if err != nil {
		return err
	}
	_, err = draftedit.EditAndCommit(draftedit.Options{
		TargetPath: candidate.Path,
		Initial:    candidate.Data,
		Mode:       candidate.Mode,
		Editor:     editor,
	}, func(path string, data []byte) error {
		return validateEditTarget(path, configPath, candidate.Name, true)
	}, func(data []byte) (bool, error) {
		if _, err := os.Lstat(candidate.Path); err == nil {
			return false, fmt.Errorf("stack already exists: %s", candidate.Name)
		} else if !os.IsNotExist(err) {
			return false, err
		}
		return true, writeFileAtomic(candidate.Path, data, candidate.Mode)
	})
	return err
}

// editClonedStackCandidate 打开 clone 草稿，校验通过后再写入目标 stack 文件。
func editClonedStackCandidate(configPath string, options agentconfig.CloneOptions, editor string) error {
	candidate, err := agentconfig.BuildCloneStackCandidate(options)
	if err != nil {
		return err
	}
	_, err = draftedit.EditAndCommit(draftedit.Options{
		TargetPath: candidate.Path,
		Initial:    candidate.Data,
		Mode:       candidate.Mode,
		Editor:     editor,
	}, func(path string, data []byte) error {
		return validateEditTarget(path, configPath, candidate.Name, true)
	}, func(data []byte) (bool, error) {
		if _, err := os.Lstat(candidate.Path); err == nil {
			return false, fmt.Errorf("target stack already exists: %s", candidate.Name)
		} else if !os.IsNotExist(err) {
			return false, err
		}
		return true, writeFileAtomic(candidate.Path, data, candidate.Mode)
	})
	return err
}

func configEditTarget(configPath string, name string, hasName bool) (string, error) {
	if !hasName {
		return configPath, nil
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg.StacksDir(), name+".yaml"), nil
}

func validateEditTarget(path string, configPath string, name string, hasName bool) error {
	if !hasName {
		cfg, err := config.LoadConfig(path)
		if err != nil {
			return err
		}
		_, err = config.LoadStacks(cfg, false)
		return err
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}
	stackSet, err := config.LoadStacks(cfg, false)
	if err != nil {
		return err
	}
	targetPath := filepath.Join(cfg.StacksDir(), name+".yaml")
	stack, err := loadEditedStack(path, name, targetPath)
	if err != nil {
		return err
	}
	nextStacks := make([]domain.Stack, 0, len(stackSet.Stacks))
	replaced := false
	for _, current := range stackSet.Stacks {
		if current.Name == name {
			nextStacks = append(nextStacks, stack)
			replaced = true
			continue
		}
		nextStacks = append(nextStacks, current)
	}
	if !replaced {
		nextStacks = append(nextStacks, stack)
	}
	nextStackSet, err := domain.ResolveStackSetUserRefs(domain.StackSet{Config: cfg, Stacks: nextStacks})
	if err != nil {
		return err
	}
	return validation.ValidateStackSet(nextStackSet, validation.WithPortChecker(validation.NoopPortChecker{}))
}

func loadEditedStack(path string, expectedName string, sourcePath string) (domain.Stack, error) {
	if _, err := config.LoadYAMLMapping(path, "Stack file"); err != nil {
		return domain.Stack{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Stack{}, err
	}
	var stack domain.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return domain.Stack{}, fmt.Errorf("Stack file contains invalid YAML: %s\n%w", path, err)
	}
	stack.SourcePath = sourcePath
	if err := stack.Validate(); err != nil {
		return domain.Stack{}, err
	}
	if stack.Name != expectedName {
		return domain.Stack{}, fmt.Errorf("stack name must be %s", expectedName)
	}
	return stack, nil
}

func writeTextFileIfChanged(path string, data []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if bytes.Equal(current, data) {
		return false, nil
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return true, writeFileAtomic(path, data, mode)
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
	return os.Rename(tempPath, path)
}

// restartActiveStack 应用 stack runtime plan，并只重启当前 active 的组件。
func restartActiveStack(command *cobra.Command, configPath string, stackName string) error {
	plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{ConfigPath: configPath, Target: stackName, SkipSystemPorts: true})
	if err != nil {
		return err
	}
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	activeNodes := make([]graph.ServiceNode, 0)
	activeServices := make([]string, 0)
	ctx := context.Background()
	for _, node := range plan.Scope.Nodes {
		serviceName := manager.ServiceForNode(node)
		active, err := manager.IsActive(ctx, serviceName)
		if err != nil {
			return err
		}
		if active {
			activeNodes = append(activeNodes, node)
			activeServices = append(activeServices, serviceName)
		}
	}
	if len(activeNodes) == 0 {
		return nil
	}
	if err := checkRequiredBinariesForNodes(plan, activeNodes); err != nil {
		return err
	}
	if err := agentruntime.ApplyPlan(plan); err != nil {
		return err
	}
	if err := repairServiceMetadataFunc(plan.Config); err != nil {
		return err
	}
	if err := manager.Restart(ctx, activeServices); err != nil {
		return err
	}
	fmt.Fprintf(command.OutOrStdout(), "Restarted active services: %v\n", activeServices)
	return nil
}

// newListCommand 创建 stack 列表命令。
func newListCommand() *cobra.Command {
	var verbose bool
	var checkSystemPorts bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List configured stacks",
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			manager, err := agentServiceManager(command)
			if err != nil {
				return err
			}
			rows, err := listStackRows(configPath, checkSystemPorts, manager)
			if err != nil {
				return err
			}
			for _, line := range formatStackTable(rows, verbose) {
				fmt.Fprintln(command.OutOrStdout(), line)
			}
			return nil
		},
	}
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose endpoints")
	command.Flags().BoolVar(&checkSystemPorts, "check-system-ports", false, "Probe live system ports while loading config")
	return command
}

const listPortScopeNote = "Note: (L)=localhost/loopback, (*)=non-loopback"

type stackListRow struct {
	Name            string
	Enabled         string
	Role            string
	Xrelay          string
	Clash           string
	Generated       string
	Running         string
	XrelayPorts     string
	XrelayAPIPort   string
	ClashSocks      string
	ClashHTTP       string
	ClashController string
}

type stackDisplayRow struct {
	Name      string
	Role      string
	Enabled   string
	Component string
	Running   string
	Generated string
	Endpoints string
}

// listStackRows 汇总 stack 配置、生成文件和运行状态。
func listStackRows(configPath string, checkSystemPorts bool, manager servicemanager.Manager) ([]stackListRow, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	stackSet, err := config.LoadStacks(cfg, checkSystemPorts)
	if err != nil {
		return nil, err
	}
	generatedDir := cfg.ResolvePath(cfg.Paths.Generated)
	rows := make([]stackListRow, 0, len(stackSet.Stacks))
	for _, stack := range stackSet.Stacks {
		rows = append(rows, stackListRow{
			Name:            stack.Name,
			Enabled:         yesNo(stack.Enabled),
			Role:            stack.Role,
			Xrelay:          yesNo(stack.Xrelay.Enabled),
			Clash:           yesNo(stack.Clash.Enabled),
			Generated:       formatComponentList(generatedStackComponents(generatedDir, stack)),
			Running:         formatComponentList(runningStackComponents(stack, manager)),
			XrelayPorts:     formatXrelayInbounds(stack.Xrelay.Inbounds),
			XrelayAPIPort:   formatXrelayAPIPort(cfg, stack),
			ClashSocks:      formatSocksListeners(stack.Clash.Listeners.Socks),
			ClashHTTP:       formatHTTPListeners(stack.Clash.Listeners.HTTP),
			ClashController: formatListenPort(stack.Clash.Controller.Listen),
		})
	}
	return rows, nil
}

func formatStackTable(rows []stackListRow, verbose bool) []string {
	if len(rows) == 0 {
		return []string{"未找到 stack。"}
	}
	groups := make([][]stackDisplayRow, 0, len(rows))
	displayRows := make([]stackDisplayRow, 0, len(rows)*2)
	for _, row := range rows {
		group := formatStackComponentRows(row, verbose)
		groups = append(groups, group)
		displayRows = append(displayRows, group...)
	}
	endpointTitle := "Ports"
	if verbose {
		endpointTitle = "Endpoints"
	}
	columns := []struct {
		key   string
		title string
		value func(stackDisplayRow) string
	}{
		{key: "name", title: "Name", value: func(row stackDisplayRow) string { return row.Name }},
		{key: "role", title: "Role", value: func(row stackDisplayRow) string { return row.Role }},
		{key: "enabled", title: "Enabled", value: func(row stackDisplayRow) string { return row.Enabled }},
		{key: "component", title: "Component", value: func(row stackDisplayRow) string { return row.Component }},
		{key: "running", title: "Running", value: func(row stackDisplayRow) string { return row.Running }},
		{key: "generated", title: "Generated", value: func(row stackDisplayRow) string { return row.Generated }},
		{key: "endpoints", title: endpointTitle, value: func(row stackDisplayRow) string { return row.Endpoints }},
	}
	widths := make(map[string]int, len(columns))
	for _, column := range columns {
		widths[column.key] = len(column.title)
		for _, row := range displayRows {
			if valueLength := len(column.value(row)); valueLength > widths[column.key] {
				widths[column.key] = valueLength
			}
		}
	}
	lines := []string{
		formatDisplayLine(columns, widths, func(columnTitle string, _ func(stackDisplayRow) string) string { return columnTitle }),
		formatSeparatorLine(columns, widths),
	}
	for groupIndex, group := range groups {
		for _, row := range group {
			lines = append(lines, formatDisplayLine(columns, widths, func(_ string, value func(stackDisplayRow) string) string {
				return value(row)
			}))
		}
		if groupIndex < len(groups)-1 {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "", listPortScopeNote)
	return lines
}

func formatDisplayLine(columns []struct {
	key   string
	title string
	value func(stackDisplayRow) string
}, widths map[string]int, value func(string, func(stackDisplayRow) string) string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, padRight(value(column.title, column.value), widths[column.key]))
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func formatSeparatorLine(columns []struct {
	key   string
	title string
	value func(stackDisplayRow) string
}, widths map[string]int) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, strings.Repeat("-", widths[column.key]))
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func formatStackComponentRows(row stackListRow, verbose bool) []stackDisplayRow {
	return []stackDisplayRow{
		{
			Name:      row.Name,
			Role:      row.Role,
			Enabled:   row.Enabled,
			Component: "xrelay",
			Running:   formatStackComponentStatus(row, "xrelay", "running"),
			Generated: formatStackComponentStatus(row, "xrelay", "generated"),
			Endpoints: formatStackXrelayEndpoints(row, verbose),
		},
		{
			Component: "clash",
			Running:   formatStackComponentStatus(row, "clash", "running"),
			Generated: formatStackComponentStatus(row, "clash", "generated"),
			Endpoints: formatStackClashEndpoints(row, verbose),
		},
	}
}

func formatStackComponentStatus(row stackListRow, component string, field string) string {
	if component == "xrelay" && row.Xrelay != "yes" {
		return "disabled"
	}
	if component == "clash" && row.Clash != "yes" {
		return "disabled"
	}
	value := row.Running
	if field == "generated" {
		value = row.Generated
	}
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) == component {
			return "yes"
		}
	}
	return "no"
}

func formatStackXrelayEndpoints(row stackListRow, verbose bool) string {
	inbounds := firstNonEmpty(row.XrelayPorts, "-")
	api := firstNonEmpty(row.XrelayAPIPort, "-")
	if !verbose {
		return inbounds
	}
	if inbounds == "-" && api == "-" {
		return "-"
	}
	return "inbounds: " + inbounds + " | api:" + api
}

func formatStackClashEndpoints(row stackListRow, verbose bool) string {
	socks := firstNonEmpty(row.ClashSocks, "-")
	http := firstNonEmpty(row.ClashHTTP, "-")
	controller := firstNonEmpty(row.ClashController, "-")
	if !verbose {
		if socks == "-" && http == "-" {
			return "-"
		}
		return "socks:" + socks + " | http:" + http
	}
	if socks == "-" && http == "-" && controller == "-" {
		return "-"
	}
	return "socks:" + socks + " | http:" + http + " | controller:" + controller
}

func generatedStackComponents(generatedDir string, stack domain.Stack) []string {
	components := make([]string, 0, 2)
	if stack.Xrelay.Enabled && fileExists(filepath.Join(generatedDir, "xray", stack.Name+".json")) {
		components = append(components, "xrelay")
	}
	if stack.Clash.Enabled && fileExists(filepath.Join(generatedDir, "mihomo", stack.Name+".yaml")) {
		components = append(components, "clash")
	}
	return components
}

// runningStackComponents 返回当前服务管理器认为 active 的组件。
func runningStackComponents(stack domain.Stack, manager servicemanager.Manager) []string {
	components := make([]string, 0, 2)
	ctx := context.Background()
	if stack.Xrelay.Enabled && isServiceActive(ctx, manager, manager.ServiceForNode(graph.ServiceNode{Stack: stack.Name, Component: "xrelay"})) {
		components = append(components, "xrelay")
	}
	if stack.Clash.Enabled && isServiceActive(ctx, manager, manager.ServiceForNode(graph.ServiceNode{Stack: stack.Name, Component: "clash"})) {
		components = append(components, "clash")
	}
	return components
}

// isServiceActive 查询服务 active 状态，失败时按未运行处理。
func isServiceActive(ctx context.Context, manager servicemanager.Manager, serviceName string) bool {
	active, err := manager.IsActive(ctx, serviceName)
	return err == nil && active
}

func formatComponentList(components []string) string {
	if len(components) == 0 {
		return "-"
	}
	return strings.Join(components, ",")
}

func formatXrelayInbounds(inbounds []domain.Inbound) string {
	items := make([]string, 0, len(inbounds))
	for _, inbound := range inbounds {
		items = append(items, inbound.Protocol+":"+formatListenerPort(inbound.Listen, inbound.Port))
	}
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func formatXrelayAPIPort(cfg domain.GlobalConfig, stack domain.Stack) string {
	if !stack.Xrelay.Enabled {
		return "-"
	}
	apiConfig := domain.ResolveXrelayAPIConfig(cfg.Defaults.Xrelay, stack.Xrelay)
	if !apiConfig.Enabled {
		return "-"
	}
	return formatListenPort(apiConfig.Listen)
}

func formatSocksListeners(listeners []domain.SocksListener) string {
	items := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		items = append(items, formatListenerPort(listener.Listen, listener.Port))
	}
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func formatHTTPListeners(listeners []domain.HTTPListener) string {
	items := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		items = append(items, formatListenerPort(listener.Listen, listener.Port))
	}
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func formatListenPort(listen string) string {
	host, port, err := domain.ParseListen(listen)
	if err != nil {
		return "-"
	}
	return formatEndpointPort(host, port)
}

func formatListenerPort(listen string, port int) string {
	return formatEndpointPort(listen, port)
}

func formatEndpointPort(host string, port int) string {
	return strconv.Itoa(port) + listenScopeMarker(host)
}

func listenScopeMarker(host string) string {
	if domain.IsLoopbackHost(host) {
		return "(L)"
	}
	return "(*)"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// newCloneCommand 创建 stack 克隆命令。
func newCloneCommand() *cobra.Command {
	var allocatePorts bool
	var edit bool
	var noEdit bool
	var editor string
	command := &cobra.Command{
		Use:   "clone SOURCE TARGET",
		Short: "Clone one stack into another",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if noEdit && command.Flags().Changed("edit") {
				return fmt.Errorf("--edit and --no-edit cannot be used together")
			}
			options := agentconfig.CloneOptions{ConfigPath: configPath, Source: args[0], Target: args[1], AllocatePorts: allocatePorts}
			if noEdit {
				if err := agentconfig.CloneStack(options); err != nil {
					return err
				}
			} else {
				if err := editClonedStackCandidate(configPath, options, editor); err != nil {
					return err
				}
			}
			fmt.Fprintf(command.OutOrStdout(), "Cloned stack: %s -> %s\n", args[0], args[1])
			return nil
		},
	}
	command.Flags().BoolVar(&allocatePorts, "allocate-ports", true, "Allocate fresh ports for target stack")
	command.Flags().BoolVar(&edit, "edit", false, "Open editor before writing stack")
	command.Flags().BoolVar(&noEdit, "no-edit", false, "Write stack without opening editor")
	command.Flags().StringVar(&editor, "editor", "", "Editor command")
	return command
}

// newMemberCommand 创建 member 子命令集合。
func newMemberCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "member",
		Short: "Manage auto stack members",
	}
	command.AddCommand(newMemberListCommand())
	command.AddCommand(newMemberAddCommand())
	command.AddCommand(newMemberRemoveCommand())
	return command
}

// newMemberListCommand 创建 member list 子命令。
func newMemberListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list STACK",
		Short: "List auto stack members",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			members, err := agentconfig.ListMembers(agentconfig.MemberOptions{ConfigPath: configPath, Stack: args[0]})
			if err != nil {
				return err
			}
			for _, member := range members {
				fmt.Fprintln(command.OutOrStdout(), member)
			}
			return nil
		},
	}
}

// newMemberAddCommand 创建 member add 子命令。
func newMemberAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add STACK MEMBER",
		Short: "Add a member stack",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if err := agentconfig.AddMember(agentconfig.MemberOptions{ConfigPath: configPath, Stack: args[0], Member: args[1]}); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Added member: %s -> %s\n", args[1], args[0])
			return nil
		},
	}
}

// newMemberRemoveCommand 创建 member remove 子命令。
func newMemberRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove STACK MEMBER",
		Short: "Remove a member stack",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if err := agentconfig.RemoveMember(agentconfig.MemberOptions{ConfigPath: configPath, Stack: args[0], Member: args[1]}); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Removed member: %s -> %s\n", args[1], args[0])
			return nil
		},
	}
}

// newRemoveCommand 创建 stack 归档命令。
func newRemoveCommand() *cobra.Command {
	var purge bool
	command := &cobra.Command{
		Use:   "remove NAME",
		Short: "Archive a stack file",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			if err := agentconfig.RemoveStack(agentconfig.RemoveOptions{ConfigPath: configPath, Name: args[0], Purge: purge}); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Removed stack: %s\n", args[0])
			return nil
		},
	}
	command.Flags().BoolVar(&purge, "purge", false, "Remove generated files recorded in manifest")
	return command
}

// splitCSV 解析逗号分隔参数并丢弃空白项。
func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
