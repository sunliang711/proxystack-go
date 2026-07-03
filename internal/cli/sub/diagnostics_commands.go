package sub

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	servicemanager "github.com/eagle/proxystack-go/internal/service"
	"github.com/eagle/proxystack-go/internal/systemd"
	"github.com/spf13/cobra"
)

// newDoctorCommand 创建 pssub 本机订阅服务只读诊断命令。
func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local subscription configuration and environment",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := subConfigPath(command)
			if err != nil {
				return err
			}
			managerKind, err := subServiceManagerKind(command)
			if err != nil {
				return err
			}
			report, err := runSubDoctor(configPath, managerKind)
			if err != nil {
				return err
			}
			for _, check := range report.Checks {
				fmt.Fprintf(command.OutOrStdout(), "OK %s\n", check)
			}
			for _, issue := range report.Issues {
				fmt.Fprintf(command.OutOrStdout(), "ISSUE %s\n", issue)
			}
			if len(report.Issues) > 0 {
				return fmt.Errorf("doctor found %d issue(s)", len(report.Issues))
			}
			fmt.Fprintln(command.OutOrStdout(), "Doctor OK")
			return nil
		},
	}
}

type subDoctorReport struct {
	Checks []string
	Issues []string
}

// runSubDoctor 执行订阅配置、inputs、metadata 和服务文件诊断。
func runSubDoctor(configPath string, managerKind string) (subDoctorReport, error) {
	subConfig, err := config.LoadSubServerConfig(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			baseDir := filepath.Dir(configPath)
			return subDoctorReport{}, fmt.Errorf("sub config is missing: %s; run `pssub --base-dir %s setup local` first", configPath, baseDir)
		}
		return subDoctorReport{}, fmt.Errorf("doctor config failed: %w", err)
	}
	baseDir := filepath.Dir(configPath)
	subConfig.DataDir = subDataDir(baseDir)
	report := subDoctorReport{Checks: []string{"config loaded"}}
	addSubDoctorInputIssues(&report, subConfig)
	uid, gid, hasServiceAccount := addSubDoctorServiceAccountIssues(&report)
	addSubDoctorMetadataIssues(&report, subOnlyGlobalConfig(baseDir), uid, gid, hasServiceAccount)
	addSubDoctorServiceFileIssues(&report, managerKind)
	return report, nil
}

// addSubDoctorInputIssues 校验 inputs 能否按运行时逻辑合并为订阅索引。
func addSubDoctorInputIssues(report *subDoctorReport, subConfig config.SubServerConfig) {
	access := subgen.Access{Type: subConfig.Access.Type, Token: subConfig.Access.Token}
	inputDir := filepath.Join(subConfig.DataDir, "inputs")
	if _, err := subgen.MergeInputFiles(inputDir, access, nowISO()); err != nil {
		report.Issues = append(report.Issues, "subscription inputs validation failed: "+err.Error())
		return
	}
	report.Checks = append(report.Checks, "subscription inputs validated")
}

// addSubDoctorServiceAccountIssues 检查订阅 systemd unit 使用的运行用户和组是否存在。
func addSubDoctorServiceAccountIssues(report *subDoctorReport) (int, int, bool) {
	serviceUser, userErr := user.Lookup(systemd.DefaultServiceUser)
	if userErr != nil {
		report.Issues = append(report.Issues, "service user is missing: "+systemd.DefaultServiceUser)
	}
	serviceGroup, groupErr := user.LookupGroup(systemd.DefaultServiceGroup)
	if groupErr != nil {
		report.Issues = append(report.Issues, "service group is missing: "+systemd.DefaultServiceGroup)
	}
	if userErr != nil || groupErr != nil {
		return 0, 0, false
	}
	uid, err := strconv.Atoi(serviceUser.Uid)
	if err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("service user has invalid uid: %s uid=%q", systemd.DefaultServiceUser, serviceUser.Uid))
		return 0, 0, false
	}
	gid, err := strconv.Atoi(serviceGroup.Gid)
	if err != nil {
		report.Issues = append(report.Issues, fmt.Sprintf("service group has invalid gid: %s gid=%q", systemd.DefaultServiceGroup, serviceGroup.Gid))
		return 0, 0, false
	}
	report.Checks = append(report.Checks, formatSubDoctorServiceAccountCheck(uid, gid))
	return uid, gid, true
}

// formatSubDoctorServiceAccountCheck 格式化订阅服务账户检查摘要。
func formatSubDoctorServiceAccountCheck(uid int, gid int) string {
	return fmt.Sprintf("service account: user=%s uid=%d group=%s gid=%d", systemd.DefaultServiceUser, uid, systemd.DefaultServiceGroup, gid)
}

// addSubDoctorMetadataIssues 检查 sub 目录树权限和 owner 是否符合部署规格。
func addSubDoctorMetadataIssues(report *subDoctorReport, cfg domain.GlobalConfig, uid int, gid int, checkOwner bool) {
	rules, err := systemd.SubMetadataRules(cfg)
	if err != nil {
		report.Issues = append(report.Issues, "sub metadata could not be checked: "+err.Error())
		return
	}
	if len(rules) == 0 {
		report.Issues = append(report.Issues, "path is missing: "+cfg.ResolvePath(cfg.Paths.Sub))
		return
	}
	for _, rule := range rules {
		info, err := os.Stat(rule.Path)
		if err != nil {
			if os.IsNotExist(err) {
				report.Issues = append(report.Issues, "path is missing: "+rule.Path)
				continue
			}
			report.Issues = append(report.Issues, "path could not be checked: "+rule.Path+" "+err.Error())
			continue
		}
		if info.Mode().Perm() != rule.Mode {
			report.Issues = append(report.Issues, fmt.Sprintf("path mode mismatch: %s got=%#o want=%#o", rule.Path, info.Mode().Perm(), rule.Mode))
		}
		if checkOwner {
			addSubDoctorOwnerIssue(report, rule.Path, info, uid, gid)
		}
	}
	if checkOwner {
		report.Checks = append(report.Checks, fmt.Sprintf("sub filesystem metadata checked: expected owner=%s:%s mode rules=%d", systemd.DefaultServiceUser, systemd.DefaultServiceGroup, len(rules)))
	} else {
		report.Checks = append(report.Checks, fmt.Sprintf("sub filesystem metadata checked: mode rules=%d owner check skipped", len(rules)))
	}
}

// addSubDoctorOwnerIssue 检查单个 sub 路径的 uid/gid 是否匹配服务运行账户。
func addSubDoctorOwnerIssue(report *subDoctorReport, path string, info os.FileInfo, uid int, gid int) {
	if runtime.GOOS == "windows" {
		return
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		report.Issues = append(report.Issues, "path owner could not be checked: "+path)
		return
	}
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		report.Issues = append(report.Issues, fmt.Sprintf("path owner mismatch: %s got=%s want=%s", path, formatSubDoctorOwnerLabel(int(stat.Uid), int(stat.Gid)), formatSubDoctorOwnerLabel(uid, gid)))
	}
}

// formatSubDoctorOwnerLabel 格式化 owner，保留数字 ID 便于排查权限问题。
func formatSubDoctorOwnerLabel(uid int, gid int) string {
	uidText := strconv.Itoa(uid)
	gidText := strconv.Itoa(gid)
	userName := uidText
	if serviceUser, err := user.LookupId(uidText); err == nil && serviceUser.Username != "" {
		userName = serviceUser.Username
	}
	groupName := gidText
	if serviceGroup, err := user.LookupGroupId(gidText); err == nil && serviceGroup.Name != "" {
		groupName = serviceGroup.Name
	}
	return fmt.Sprintf("%s:%s (uid=%d gid=%d)", userName, groupName, uid, gid)
}

// addSubDoctorServiceFileIssues 检查当前服务管理器对应的订阅服务文件是否存在。
func addSubDoctorServiceFileIssues(report *subDoctorReport, managerKind string) {
	switch managerKind {
	case servicemanager.ManagerSystemd:
		addSubDoctorSystemdUnitIssues(report)
	case servicemanager.ManagerLaunchd:
		addSubDoctorLaunchdPlistIssues(report)
	default:
		report.Issues = append(report.Issues, "unsupported service manager: "+managerKind)
	}
}

// addSubDoctorSystemdUnitIssues 检查订阅 systemd unit 文件及启动命令。
func addSubDoctorSystemdUnitIssues(report *subDoctorReport) {
	path := filepath.Join(systemd.DefaultUnitDir, systemd.SubUnit)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			report.Issues = append(report.Issues, "systemd unit is missing: "+path)
			return
		}
		report.Issues = append(report.Issues, "systemd unit could not be checked: "+path+" "+err.Error())
		return
	}
	if !bytes.Contains(data, []byte("/usr/local/bin/pssub")) {
		report.Issues = append(report.Issues, "systemd unit command mismatch: "+path+" want /usr/local/bin/pssub")
	}
	report.Checks = append(report.Checks, "systemd unit checked")
}

// addSubDoctorLaunchdPlistIssues 检查订阅 launchd plist 文件是否存在。
func addSubDoctorLaunchdPlistIssues(report *subDoctorReport) {
	path := filepath.Join(servicemanager.DefaultLaunchdDir, servicemanager.LaunchdSubLabel+".plist")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			report.Issues = append(report.Issues, "launchd plist is missing: "+path)
			return
		}
		report.Issues = append(report.Issues, "launchd plist could not be checked: "+path+" "+err.Error())
		return
	}
	report.Checks = append(report.Checks, "launchd plist checked")
}

// subServiceManagerKind 解析 doctor 使用的服务管理器后端。
func subServiceManagerKind(command *cobra.Command) (string, error) {
	flag := command.Flag("service-manager")
	if flag == nil {
		return "", fmt.Errorf("flag accessed but not defined: service-manager")
	}
	return servicemanager.ResolveManagerKind(flag.Value.String(), runtime.GOOS)
}
