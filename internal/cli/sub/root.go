package sub

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/eagle/proxystack-go/internal/cli/draftedit"
	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/eagle/proxystack-go/internal/subserver"
	"github.com/eagle/proxystack-go/internal/systemd"
	"github.com/eagle/proxystack-go/internal/version"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	defaultBaseDir        = "/opt/proxystack-sub"
	defaultListen         = "0.0.0.0:3003"
	defaultServiceManager = servicemanager.ManagerAuto
)

var subServiceManagerFactory = servicemanager.NewManager
var subRepairServiceMetadataFunc = repairSubServiceMetadata

const (
	subSetupGroup   = "setup"
	subConfigGroup  = "config"
	subDataGroup    = "data"
	subServiceGroup = "service"
	subDoctorGroup  = "doctor"
	subHelpGroup    = "help"
)

// NewRootCommand 创建 pssub 的根命令和 T01 要求的最小命令树。
func NewRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "pssub",
		Short:         "Serve proxystack subscription data",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.AddGroup(
		&cobra.Group{ID: subSetupGroup, Title: "初始化"},
		&cobra.Group{ID: subConfigGroup, Title: "配置管理"},
		&cobra.Group{ID: subDataGroup, Title: "订阅数据"},
		&cobra.Group{ID: subServiceGroup, Title: "服务控制"},
		&cobra.Group{ID: subDoctorGroup, Title: "诊断工具"},
		&cobra.Group{ID: subHelpGroup, Title: "其它"},
	)
	command.SetHelpCommandGroupID(subHelpGroup)
	command.SetCompletionCommandGroupID(subHelpGroup)
	command.PersistentFlags().String("base-dir", defaultBaseDir, "Subscription base directory")
	command.PersistentFlags().String("listen", defaultListen, "Subscription listen address")
	command.PersistentFlags().String("service-manager", defaultServiceManager, "Service manager: auto, systemd, launchd")
	command.AddCommand(groupedCommand(subHelpGroup, newVersionCommand("pssub")))
	command.AddCommand(groupedCommand(subSetupGroup, newSetupCommand()))
	command.AddCommand(groupedCommand(subServiceGroup, newServeCommand()))
	command.AddCommand(groupedCommand(subDataGroup, newImportCommand()))
	command.AddCommand(groupedCommand(subDataGroup, newInputCommand()))
	command.AddCommand(groupedCommand(subConfigGroup, newConfigCommand()))
	command.AddCommand(groupedCommand(subDataGroup, newClearCommand()))
	command.AddCommand(groupedCommand(subServiceGroup, newServiceCommand()))
	command.AddCommand(groupedCommand(subDoctorGroup, newDoctorCommand()))
	for _, action := range []string{"start", "stop", "restart", "status", "logs", "enable", "disable"} {
		command.AddCommand(groupedCommand(subServiceGroup, newLifecycleCommand(action)))
	}
	return command
}

// newSetupCommand 创建 pssub 本地初始化和 service unit 安装命令。
func newSetupCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "setup",
		Short: "Initialize subscription files and install service file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSubSetupAll(command, force)
		},
	}
	addSubSetupFlags(command, &force)
	command.AddCommand(newSubSetupLocalCommand(&force))
	command.AddCommand(newSubSetupAllCommand(&force))
	return command
}

// newSubSetupLocalCommand 创建只执行 pssub 本地初始化和 service unit 安装的子命令。
func newSubSetupLocalCommand(force *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "local",
		Short: "Initialize subscription files and install service file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSubSetupLocal(command, *force)
		},
	}
	addSubSetupFlags(command, force)
	return command
}

// newSubSetupAllCommand 创建当前等价于 setup local 的完整 setup 子命令。
func newSubSetupAllCommand(force *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "all",
		Short: "Run setup local",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSubSetupAll(command, *force)
		},
	}
	addSubSetupFlags(command, force)
	return command
}

// addSubSetupFlags 注册 pssub 本地初始化相关 flag，兼容 setup、setup local 和 setup all。
func addSubSetupFlags(command *cobra.Command, force *bool) {
	command.Flags().BoolVar(force, "force", false, "Overwrite existing sub config")
}

// runSubSetupAll 当前等价于 runSubSetupLocal，保留 all 入口便于后续扩展。
func runSubSetupAll(command *cobra.Command, force bool) error {
	return runSubSetupLocal(command, force)
}

// runSubSetupLocal 初始化 pssub 目录和配置，并只安装 service unit。
func runSubSetupLocal(command *cobra.Command, force bool) error {
	baseDir, err := subBaseDir(command)
	if err != nil {
		return err
	}
	result, err := initSubLayout(baseDir, force)
	if err != nil {
		return err
	}
	fmt.Fprintf(command.OutOrStdout(), "Initialized subscription layout: data_dir=%s input_dir=%s template_dir=%s config=%s created_config=%t\n", result.DataDir, result.InputDir, result.TemplatesDir, result.ConfigPath, result.CreatedConfig)
	paths, err := installSubServiceUnits(command, baseDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(command.OutOrStdout(), "Installed units: %v\n", paths)
	return nil
}

// groupedCommand 给根命令子命令设置 usage 分组，保持 pssub help 分块展示。
func groupedCommand(groupID string, command *cobra.Command) *cobra.Command {
	command.GroupID = groupID
	return command
}

// newVersionCommand 创建只读 version 子命令。
func newVersionCommand(binary string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(command *cobra.Command, args []string) {
			fmt.Fprintln(command.OutOrStdout(), version.Info(binary))
		},
	}
}

func newServeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve subscription HTTP endpoints",
		RunE: func(command *cobra.Command, args []string) error {
			subConfig, err := loadRuntimeConfig(command)
			if err != nil {
				return err
			}
			if err := configureSubLogger(subConfig.Log, os.Stderr); err != nil {
				return err
			}
			server := subserver.NewServer(subConfig, nowISO)
			ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Start(ctx)
		},
	}
	command.Flags().String("host", "", "Subscription listen host")
	command.Flags().Int("port", 0, "Subscription listen port")
	return command
}

// configureSubLogger 根据 sub 配置切换 serve 运行期日志格式。
func configureSubLogger(logConfig config.LogConfig, output io.Writer) error {
	switch logConfig.Format {
	case config.LogFormatJSON:
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
		log.Logger = zerolog.New(output).With().Timestamp().Logger()
		return nil
	case config.LogFormatConsole:
		zerolog.TimeFieldFormat = time.RFC3339
		writer := zerolog.ConsoleWriter{Out: output, TimeFormat: time.RFC3339}
		log.Logger = zerolog.New(writer).With().Timestamp().Logger()
		return nil
	default:
		return fmt.Errorf("log.format must be json or console")
	}
}

func newImportCommand() *cobra.Command {
	var replaceAll bool
	command := &cobra.Command{
		Use:   "import BUNDLE",
		Short: "Import subscription bundle inputs",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			subConfig, err := loadRuntimeConfig(command)
			if err != nil {
				return err
			}
			result, err := subgen.ExtractBundleInputs(args[0], subConfig.DataDir, replaceAll)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Imported subscription bundle: inputs=%d replace_all=%t\n", len(result.WrittenInputs), result.ReplaceAll)
			return nil
		},
	}
	command.Flags().BoolVar(&replaceAll, "replace-all", false, "Replace all managed inputs")
	return command
}

func newConfigCommand() *cobra.Command {
	var editor string
	command := &cobra.Command{
		Use:   "config",
		Short: "Edit subscription config",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			configPath, changed, err := editSubConfig(command, editor)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(command.OutOrStdout(), "Config updated: %s\n", configPath)
				return nil
			}
			fmt.Fprintf(command.OutOrStdout(), "Config OK: %s\n", configPath)
			return nil
		},
	}
	command.Flags().StringVar(&editor, "editor", "", "Editor command")
	command.AddCommand(newConfigShowCommand())
	command.AddCommand(newConfigCheckCommand())
	return command
}

// newConfigShowCommand 创建打印有效 sub config 的只读命令，默认脱敏 token。
func newConfigShowCommand() *cobra.Command {
	var showSecrets bool
	command := &cobra.Command{
		Use:   "show",
		Short: "Print effective subscription config",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			subConfig, err := loadRuntimeConfig(command)
			if err != nil {
				return err
			}
			if !showSecrets && subConfig.Access.Token != "" {
				subConfig.Access.Token = "[REDACTED]"
			}
			data, err := yaml.Marshal(subConfig)
			if err != nil {
				return err
			}
			_, err = command.OutOrStdout().Write(data)
			return err
		},
	}
	command.Flags().BoolVar(&showSecrets, "show-secrets", false, "Print sensitive config values")
	return command
}

// newConfigCheckCommand 创建只校验 sub config 的只读命令。
func newConfigCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate subscription config",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := subConfigPath(command)
			if err != nil {
				return err
			}
			if _, err := config.LoadSubServerConfig(configPath); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Config OK: %s\n", configPath)
			return nil
		},
	}
}

func newClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear subscription input files",
		RunE: func(command *cobra.Command, args []string) error {
			subConfig, err := loadRuntimeConfig(command)
			if err != nil {
				return err
			}
			inputDir := filepath.Join(subConfig.DataDir, "inputs")
			paths, err := subgen.ScanInputFiles(inputDir)
			if err != nil {
				return err
			}
			for _, path := range paths {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
			fmt.Fprintf(command.OutOrStdout(), "Cleared subscription inputs: count=%d\n", len(paths))
			return nil
		},
	}
}

// editSubConfig 通过草稿编辑 config.yaml，校验通过后再替换真实文件。
func editSubConfig(command *cobra.Command, editor string) (string, bool, error) {
	configPath, err := subConfigPath(command)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return "", false, fmt.Errorf("sub config does not exist: %s; run pssub setup local first", configPath)
		}
		return "", false, err
	}
	original, err := os.ReadFile(configPath)
	if err != nil {
		return "", false, err
	}
	mode := os.FileMode(0o640)
	if info, err := os.Stat(configPath); err == nil {
		mode = info.Mode().Perm()
	}
	result, err := draftedit.EditAndCommit(draftedit.Options{
		TargetPath: configPath,
		Initial:    original,
		Mode:       mode,
		Editor:     editor,
	}, func(path string, data []byte) error {
		_, err := config.LoadSubServerConfig(path)
		return err
	}, func(data []byte) (bool, error) {
		return writeTextFileIfChanged(configPath, data)
	})
	return configPath, result.Changed, err
}

type initSubResult struct {
	DataDir       string
	InputDir      string
	TemplatesDir  string
	ConfigPath    string
	CreatedConfig bool
}

// initSubLayout 幂等创建 pssub 独立运行所需的目录和默认配置文件。
func initSubLayout(baseDir string, force bool) (initSubResult, error) {
	dataDir := subDataDir(baseDir)
	inputDir := filepath.Join(dataDir, "inputs")
	templatesDir := filepath.Join(dataDir, "templates")
	configPath := subConfigPathForBaseDir(baseDir)
	result := initSubResult{DataDir: dataDir, InputDir: inputDir, TemplatesDir: templatesDir, ConfigPath: configPath}
	for _, dir := range []string{dataDir, inputDir, templatesDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return initSubResult{}, err
		}
	}
	info, err := os.Stat(configPath)
	if err == nil && info.IsDir() {
		return initSubResult{}, fmt.Errorf("sub config path is a directory: %s", configPath)
	}
	if err == nil && !force {
		return result, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return initSubResult{}, err
	}
	mode := os.FileMode(0o640)
	uid := -1
	gid := -1
	if err == nil {
		// --force 覆盖已有配置时保留 owner，避免 sudo 重置后服务用户无法读取。
		mode = info.Mode().Perm()
		if ownerUID, ownerGID, ok := fileOwnerIDs(info); ok {
			uid = ownerUID
			gid = ownerGID
		}
	}
	if err := writeFileAtomicWithOwner(configPath, []byte(config.DefaultSubServerConfigYAML()), mode, uid, gid); err != nil {
		return initSubResult{}, err
	}
	result.CreatedConfig = true
	return result, nil
}

func loadRuntimeConfig(command *cobra.Command) (config.SubServerConfig, error) {
	baseDir, err := subBaseDir(command)
	if err != nil {
		return config.SubServerConfig{}, err
	}
	dataDir := subDataDir(baseDir)
	configPath := subConfigPathForBaseDir(baseDir)
	var subConfig config.SubServerConfig
	if _, err := os.Stat(configPath); err == nil {
		subConfig, err = config.LoadSubServerConfig(configPath)
		if err != nil {
			return config.SubServerConfig{}, err
		}
	} else if os.IsNotExist(err) {
		subConfig = config.SubServerConfig{}
		subConfig.ApplyDefaults()
	} else {
		return config.SubServerConfig{}, err
	}
	subConfig.DataDir = dataDir
	listen, err := getStringFlag(command, "listen")
	if err != nil {
		return config.SubServerConfig{}, err
	}
	if flag := command.Flag("listen"); flag != nil && flag.Changed {
		subConfig.Listen = listen
	}
	if err := applyHostPortOverrides(command, &subConfig); err != nil {
		return config.SubServerConfig{}, err
	}
	if err := subConfig.Validate(); err != nil {
		return config.SubServerConfig{}, err
	}
	return subConfig, nil
}

// subConfigPath 返回当前 base dir 下固定的 config.yaml 路径。
func subConfigPath(command *cobra.Command) (string, error) {
	baseDir, err := subBaseDir(command)
	if err != nil {
		return "", err
	}
	return subConfigPathForBaseDir(baseDir), nil
}

// subDataDir 返回 pssub 的运行数据根目录。
func subDataDir(baseDir string) string {
	return baseDir
}

// subConfigPathForBaseDir 返回 pssub 根目录下的配置文件路径。
func subConfigPathForBaseDir(baseDir string) string {
	return filepath.Join(subDataDir(baseDir), "config.yaml")
}

// subBaseDir 读取 pssub 全局 base dir，并解析为绝对路径。
func subBaseDir(command *cobra.Command) (string, error) {
	flag := command.Flag("base-dir")
	if flag == nil {
		return "", fmt.Errorf("flag accessed but not defined: base-dir")
	}
	baseDir := flag.Value.String()
	if baseDir == "" {
		baseDir = defaultBaseDir
	}
	return filepath.Abs(baseDir)
}

func getStringFlag(command *cobra.Command, name string) (string, error) {
	flag := command.Flag(name)
	if flag == nil {
		return "", fmt.Errorf("flag accessed but not defined: %s", name)
	}
	return flag.Value.String(), nil
}

func applyHostPortOverrides(command *cobra.Command, subConfig *config.SubServerConfig) error {
	hostFlag := command.Flag("host")
	portFlag := command.Flag("port")
	hostChanged := hostFlag != nil && hostFlag.Changed
	portChanged := portFlag != nil && portFlag.Changed
	if !hostChanged && !portChanged {
		return nil
	}
	host, port, err := domain.ParseListen(subConfig.Listen)
	if err != nil {
		return err
	}
	if hostChanged {
		host = hostFlag.Value.String()
		if host == "" {
			return fmt.Errorf("listen host is required")
		}
	}
	if portChanged {
		portValue, err := strconv.Atoi(portFlag.Value.String())
		if err != nil {
			return fmt.Errorf("listen port must be an integer")
		}
		if err := domain.ValidatePort(portValue, "listen port"); err != nil {
			return err
		}
		port = portValue
	}
	subConfig.Listen = host + ":" + strconv.Itoa(port)
	return nil
}

// newServiceCommand 创建 pssub 自身 service 管理命令集合。
func newServiceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "service",
		Short: "Manage proxystack subscription service file",
	}
	command.AddCommand(newServiceInstallCommand())
	return command
}

// newServiceInstallCommand 创建只安装订阅服务 unit/plist 的命令。
func newServiceInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install subscription service file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			baseDir, err := subBaseDir(command)
			if err != nil {
				return err
			}
			paths, err := installSubServiceUnits(command, baseDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Installed units: %v\n", paths)
			return nil
		},
	}
}

// installSubServiceUnits 渲染并安装 pssub 自身 service unit，不启动也不 enable 服务。
func installSubServiceUnits(command *cobra.Command, baseDir string) ([]string, error) {
	manager, err := subServiceManager(command)
	if err != nil {
		return nil, err
	}
	cfg := subOnlyGlobalConfig(baseDir)
	if err := subRepairServiceMetadataFunc(cfg); err != nil {
		return nil, err
	}
	return manager.InstallUnits(cfg, "sub")
}

// newLifecycleCommand 创建 pssub 顶层生命周期命令。
func newLifecycleCommand(action string) *cobra.Command {
	var follow bool
	command := &cobra.Command{
		Use:   action,
		Short: "Run service manager " + action + " for subscription service",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runSubLifecycle(command, action, follow)
		},
	}
	if action == "logs" {
		command.Aliases = []string{"log"}
		command.Flags().BoolVarP(&follow, "follow", "f", false, "Follow service logs")
	}
	return command
}

// runSubLifecycle 分派 pssub 生命周期动作，作用域固定为订阅服务。
func runSubLifecycle(command *cobra.Command, action string, follow bool) error {
	manager, err := subServiceManager(command)
	if err != nil {
		return err
	}
	if action == "start" || action == "restart" {
		baseDir, err := subBaseDir(command)
		if err != nil {
			return err
		}
		if err := subRepairServiceMetadataFunc(subOnlyGlobalConfig(baseDir)); err != nil {
			return err
		}
	}
	return runSubServiceAction(command, context.Background(), manager, action, []string{manager.SubService()}, follow)
}

// runSubServiceAction 执行服务管理动作并把 status/logs 输出转发给 CLI。
func runSubServiceAction(command *cobra.Command, ctx context.Context, manager servicemanager.Manager, action string, services []string, follow bool) error {
	switch action {
	case "start":
		return manager.Start(ctx, services)
	case "stop":
		return manager.Stop(ctx, services)
	case "restart":
		return manager.Restart(ctx, services)
	case "status":
		result, err := manager.Status(ctx, services)
		writeCommandResult(command, result)
		return err
	case "logs":
		result, err := manager.Logs(ctx, services, follow)
		writeCommandResult(command, result)
		return err
	case "enable":
		return manager.Enable(ctx, services)
	case "disable":
		return manager.Disable(ctx, services)
	default:
		return fmt.Errorf("unsupported lifecycle action: %s", action)
	}
}

// subServiceManager 创建当前命令指定的服务管理器。
func subServiceManager(command *cobra.Command) (servicemanager.Manager, error) {
	flag := command.Flag("service-manager")
	if flag == nil {
		return nil, fmt.Errorf("flag accessed but not defined: service-manager")
	}
	return subServiceManagerFactory(flag.Value.String())
}

// subOnlyGlobalConfig 构造只用于渲染订阅服务文件的最小全局配置。
func subOnlyGlobalConfig(baseDir string) domain.GlobalConfig {
	paths := domain.DefaultConfigPaths()
	paths.Sub = "."
	return domain.GlobalConfig{BaseDir: baseDir, Paths: paths}
}

// repairSubServiceMetadata 在 Linux root 启动订阅服务前修复 sub 目录 owner 和 mode。
func repairSubServiceMetadata(config domain.GlobalConfig) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil
	}
	if _, err := os.Stat(config.ResolvePath(config.Paths.Sub)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	uid, gid, err := subServiceAccountOwnerIDs()
	if err != nil {
		return err
	}
	return systemd.RepairSubMetadata(config, uid, gid, systemd.MetadataFixer{})
}

// subServiceAccountOwnerIDs 解析订阅 systemd 服务使用的 proxystack 用户和组。
func subServiceAccountOwnerIDs() (int, int, error) {
	serviceUser, err := user.Lookup(systemd.DefaultServiceUser)
	if err != nil {
		return 0, 0, fmt.Errorf("service user %s is missing; create the service account first: %w", systemd.DefaultServiceUser, err)
	}
	serviceGroup, err := user.LookupGroup(systemd.DefaultServiceGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("service group %s is missing; create the service account first: %w", systemd.DefaultServiceGroup, err)
	}
	uid, err := strconv.Atoi(serviceUser.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("service user %s has invalid uid %q: %w", systemd.DefaultServiceUser, serviceUser.Uid, err)
	}
	gid, err := strconv.Atoi(serviceGroup.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("service group %s has invalid gid %q: %w", systemd.DefaultServiceGroup, serviceGroup.Gid, err)
	}
	return uid, gid, nil
}

// writeCommandResult 把服务管理器捕获输出转发到 Cobra 的 stdout/stderr。
func writeCommandResult(command *cobra.Command, result servicemanager.Result) {
	if result.Stdout != "" {
		fmt.Fprint(command.OutOrStdout(), result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(command.ErrOrStderr(), result.Stderr)
	}
}

// writeTextFileIfChanged 在内容变化时原子替换文本文件。
func writeTextFileIfChanged(path string, data []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if bytes.Equal(current, data) {
		return false, nil
	}
	mode := os.FileMode(0o640)
	uid := -1
	gid := -1
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if ownerUID, ownerGID, ok := fileOwnerIDs(info); ok {
			uid = ownerUID
			gid = ownerGID
		}
	}
	return true, writeFileAtomicWithOwner(path, data, mode, uid, gid)
}

// writeFileAtomic 写入配置文件并以 rename 替换目标，避免半写入配置被读取。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	return writeFileAtomicWithOwner(path, data, mode, -1, -1)
}

// writeFileAtomicWithOwner 写入配置文件，并在替换前尽量保留目标文件 owner。
func writeFileAtomicWithOwner(path string, data []byte, mode os.FileMode, uid int, gid int) error {
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
		if uid >= 0 && gid >= 0 {
			if err := chownFileIfNeeded(temp, uid, gid); err != nil {
				return err
			}
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

// fileOwnerIDs 从文件信息中提取 Unix uid/gid，非 Unix 平台返回不可用。
func fileOwnerIDs(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}

// chownFileIfNeeded 在临时文件 owner 与目标 owner 不一致时执行 chown。
func chownFileIfNeeded(file *os.File, uid int, gid int) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	currentUID, currentGID, ok := fileOwnerIDs(info)
	if !ok || (currentUID == uid && currentGID == gid) {
		return nil
	}
	return file.Chown(uid, gid)
}

// runEditor 执行用户指定或环境默认编辑器。
func runEditor(editor string, targetPath string) error {
	return draftedit.RunEditor(editor, targetPath)
}

func nowISO() string {
	return time.Now().Local().Format(time.RFC3339)
}
