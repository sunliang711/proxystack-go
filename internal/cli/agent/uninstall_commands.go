package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/spf13/cobra"
)

const (
	uninstallPreservedConfig = "config.yaml"
	uninstallPreservedStacks = "stacks"
)

// newUninstallCommand 创建顶层 uninstall 命令，用于卸载服务文件和安装产物。
func newUninstallCommand() *cobra.Command {
	var purge bool
	command := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall proxystack service files and installed runtime files",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runUninstall(command, purge)
		},
	}
	command.Flags().BoolVar(&purge, "purge", false, "Purge base directory during uninstall")
	return command
}

// runUninstall 执行顶层卸载流程，并按 purge 决定是否保留用户配置数据。
func runUninstall(command *cobra.Command, purge bool) error {
	baseDir, err := agentBaseDir(command)
	if err != nil {
		return err
	}
	if err := guardUninstallBaseDir(baseDir); err != nil {
		return err
	}
	manager, err := agentServiceManager(command)
	if err != nil {
		return err
	}
	stdout := command.OutOrStdout()
	services, err := uninstallServiceNames(filepath.Join(baseDir, uninstallPreservedConfig), manager)
	if err != nil {
		return err
	}
	for _, service := range uninstallServiceDisplayNames(services) {
		fmt.Fprintf(stdout, "Stopping service: %s\n", service)
	}
	if err := manager.Stop(context.Background(), services); err != nil {
		return err
	}
	cfg, err := uninstallConfig(filepath.Join(baseDir, uninstallPreservedConfig), baseDir)
	if err != nil {
		return err
	}
	serviceFiles, err := manager.UninstallUnits(cfg, "")
	if err != nil {
		return err
	}
	for _, serviceFile := range serviceFiles {
		fmt.Fprintf(stdout, "Removing service file: %s\n", serviceFile)
	}
	if purge {
		fmt.Fprintf(stdout, "Removing base directory: %s\n", baseDir)
		if err := os.RemoveAll(baseDir); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Uninstall OK")
		return nil
	}
	removed, err := removeBaseDirInstallEntries(baseDir)
	if err != nil {
		return err
	}
	for _, entry := range removed {
		fmt.Fprintf(stdout, "Removing base directory entry: %s\n", entry)
	}
	fmt.Fprintf(stdout, "Preserving: %s\n", filepath.Join(baseDir, uninstallPreservedConfig))
	fmt.Fprintf(stdout, "Preserving: %s\n", filepath.Join(baseDir, uninstallPreservedStacks))
	fmt.Fprintln(stdout, "Uninstall OK")
	return nil
}

// guardUninstallBaseDir 拒绝明显危险或不可作为安装根目录的卸载路径。
func guardUninstallBaseDir(baseDir string) error {
	cleaned := filepath.Clean(baseDir)
	switch cleaned {
	case "", ".", string(os.PathSeparator), "/root", "/home", "/Users", "/opt", "/usr", "/usr/local", "/etc", "/var", "/tmp":
		return fmt.Errorf("base directory is too broad: %s", baseDir)
	}
	info, err := os.Lstat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("base directory must not be a symlink: %s", baseDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("base directory is not a directory: %s", baseDir)
	}
	if homeDir, err := os.UserHomeDir(); err == nil && cleaned == filepath.Clean(homeDir) {
		return fmt.Errorf("base directory must not be user home: %s", baseDir)
	}
	if workingDir, err := os.Getwd(); err == nil && pathContains(cleaned, workingDir) {
		return fmt.Errorf("base directory must not be current working directory or its parent: %s", baseDir)
	}
	marked, err := hasProxystackInstallMarker(cleaned)
	if err != nil {
		return err
	}
	if !marked {
		return fmt.Errorf("base directory does not look like a proxystack installation: %s", baseDir)
	}
	return nil
}

// uninstallServiceNames 返回顶层卸载前需要停止的 stack 服务。
func uninstallServiceNames(configPath string, manager servicemanager.Manager) ([]string, error) {
	services := make([]string, 0)
	if _, err := os.Stat(configPath); err == nil {
		stackServices, err := resolveServiceNames(configPath, "", manager)
		if err != nil {
			return nil, err
		}
		services = append(services, stackServices...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return services, nil
}

// uninstallConfig 加载卸载服务文件所需配置，缺失 config 时回退到 base-dir 默认路径。
func uninstallConfig(configPath string, baseDir string) (domain.GlobalConfig, error) {
	cfg, err := config.LoadConfig(configPath)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return domain.GlobalConfig{BaseDir: baseDir, Paths: domain.DefaultConfigPaths()}, nil
	}
	return domain.GlobalConfig{}, err
}

// uninstallServiceDisplayNames 把底层服务名合并为适合 CLI 展示的 stack 名。
func uninstallServiceDisplayNames(services []string) []string {
	names := make([]string, 0, len(services))
	seen := map[string]bool{}
	for _, service := range services {
		name := uninstallServiceDisplayName(service)
		if name == "" {
			name = service
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// uninstallServiceDisplayName 从 systemd unit 或 launchd label 提取用户关心的服务名。
func uninstallServiceDisplayName(service string) string {
	for _, prefix := range []string{"proxystack-clash@", "proxystack-xray@"} {
		if strings.HasPrefix(service, prefix) && strings.HasSuffix(service, ".service") {
			return strings.TrimSuffix(strings.TrimPrefix(service, prefix), ".service")
		}
	}
	for _, prefix := range []string{"com.proxystack.mihomo.", "com.proxystack.xray."} {
		if strings.HasPrefix(service, prefix) {
			return strings.TrimPrefix(service, prefix)
		}
	}
	return ""
}

// hasProxystackInstallMarker 检查目录中是否存在 proxystack 安装或配置标记。
func hasProxystackInstallMarker(baseDir string) (bool, error) {
	markers := []string{
		uninstallPreservedConfig,
		filepath.Join("bin", "ps-agent"),
		filepath.Join("bin", "ps-sub"),
		filepath.Join("bin", "xray"),
		filepath.Join("bin", "mihomo"),
	}
	for _, marker := range markers {
		_, err := os.Stat(filepath.Join(baseDir, marker))
		if err == nil {
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

// pathContains 判断 parent 是否等于 child 或为 child 的上级目录。
func pathContains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative))
}

// removeBaseDirInstallEntries 删除 base dir 中除 config.yaml 和 stacks 外的安装产物。
func removeBaseDirInstallEntries(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	removed := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == uninstallPreservedConfig || name == uninstallPreservedStacks {
			continue
		}
		path := filepath.Join(baseDir, name)
		if err := os.RemoveAll(path); err != nil {
			return nil, err
		}
		removed = append(removed, path)
	}
	sort.Strings(removed)
	return removed, nil
}
