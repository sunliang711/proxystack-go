package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/diagnostics"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/systemd"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var queryIPInfo = diagnostics.QueryIPInfo

// newDoctorCommand 创建本机 agent 环境只读诊断命令。
func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local agent configuration and environment",
		RunE: func(command *cobra.Command, args []string) error {
			configPath, err := agentConfigPath(command)
			if err != nil {
				return err
			}
			report, err := runDoctor(configPath)
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

type doctorReport struct {
	Checks []string
	Issues []string
}

// runDoctor 执行配置、目录、二进制和 unit 文件诊断。
func runDoctor(configPath string) (doctorReport, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return doctorReport{}, fmt.Errorf("agent config is missing: %s; run `psctl --base-dir %s setup local` first", configPath, filepath.Dir(configPath))
		}
		return doctorReport{}, fmt.Errorf("doctor config failed: %w", err)
	}
	report := doctorReport{Checks: []string{"config loaded"}}
	if _, err := config.LoadStacks(cfg, false); err != nil {
		report.Issues = append(report.Issues, "configuration validation failed: "+err.Error())
	} else {
		report.Checks = append(report.Checks, "stacks configuration validated")
	}
	uid, gid, hasServiceAccount := addDoctorServiceAccountIssues(&report)
	addDoctorMetadataIssues(&report, cfg, uid, gid, hasServiceAccount)
	addDoctorBinaryIssues(&report, cfg)
	addDoctorUnitIssues(&report)
	return report, nil
}

// addDoctorServiceAccountIssues 检查 systemd unit 使用的运行用户和组是否存在。
func addDoctorServiceAccountIssues(report *doctorReport) (int, int, bool) {
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
	report.Checks = append(report.Checks, formatDoctorServiceAccountCheck(uid, gid))
	return uid, gid, true
}

// formatDoctorServiceAccountCheck 格式化服务账户检查摘要。
func formatDoctorServiceAccountCheck(uid int, gid int) string {
	return fmt.Sprintf("service account: user=%s uid=%d group=%s gid=%d", systemd.DefaultServiceUser, uid, systemd.DefaultServiceGroup, gid)
}

// addDoctorMetadataIssues 检查标准目录和文件权限、owner 是否符合部署规格。
func addDoctorMetadataIssues(report *doctorReport, cfg domain.GlobalConfig, uid int, gid int, checkOwner bool) {
	rules := systemd.StandardMetadataRules(cfg)
	for _, rule := range rules {
		info, err := os.Stat(rule.Path)
		if err != nil {
			if os.IsNotExist(err) {
				if isDoctorOptionalMetadataPath(cfg, rule.Path) {
					continue
				}
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
			addDoctorOwnerIssue(report, rule.Path, info, uid, gid)
		}
	}
	if checkOwner {
		report.Checks = append(report.Checks, fmt.Sprintf("filesystem metadata checked: expected owner=%s:%s mode rules=%d", systemd.DefaultServiceUser, systemd.DefaultServiceGroup, len(rules)))
	} else {
		report.Checks = append(report.Checks, fmt.Sprintf("filesystem metadata checked: mode rules=%d owner check skipped", len(rules)))
	}
}

// isDoctorOptionalMetadataPath 判断 doctor 中允许尚未生成的按需 runtime 目录。
func isDoctorOptionalMetadataPath(cfg domain.GlobalConfig, path string) bool {
	generatedDir := cfg.ResolvePath(cfg.Paths.Generated)
	cleanPath := filepath.Clean(path)
	for _, optionalPath := range []string{
		filepath.Join(generatedDir, "xray"),
		filepath.Join(generatedDir, "mihomo"),
	} {
		if cleanPath == filepath.Clean(optionalPath) {
			return true
		}
	}
	return false
}

// addDoctorOwnerIssue 检查单个路径的 uid/gid 是否匹配服务运行账户。
func addDoctorOwnerIssue(report *doctorReport, path string, info os.FileInfo, uid int, gid int) {
	if runtime.GOOS == "windows" {
		return
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		report.Issues = append(report.Issues, "path owner could not be checked: "+path)
		return
	}
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		report.Issues = append(report.Issues, fmt.Sprintf("path owner mismatch: %s got=%s want=%s", path, formatDoctorOwnerLabel(int(stat.Uid), int(stat.Gid)), formatDoctorOwnerLabel(uid, gid)))
	}
}

// formatDoctorOwnerLabel 格式化 owner，用名称提升可读性，保留数字 ID 便于排查。
func formatDoctorOwnerLabel(uid int, gid int) string {
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

// addDoctorBinaryIssues 检查托管二进制和 geo 数据是否已安装。
func addDoctorBinaryIssues(report *doctorReport, cfg domain.GlobalConfig) {
	binDir := cfg.ResolvePath(cfg.Paths.Bin)
	for _, name := range []string{"mihomo", "xray"} {
		path := filepath.Join(binDir, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				report.Issues = append(report.Issues, "managed binary is missing: "+path)
				continue
			}
			report.Issues = append(report.Issues, "managed binary could not be checked: "+path+" "+err.Error())
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			report.Issues = append(report.Issues, "managed binary is not executable: "+path)
		}
	}
	geoDir := cfg.ResolvePath(cfg.Paths.Geo)
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		path := filepath.Join(geoDir, name)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				report.Issues = append(report.Issues, "geo data is missing: "+path)
				continue
			}
			report.Issues = append(report.Issues, "geo data could not be checked: "+path+" "+err.Error())
		}
	}
	report.Checks = append(report.Checks, "managed files checked")
}

// addDoctorUnitIssues 检查标准 systemd unit 文件是否存在。
func addDoctorUnitIssues(report *doctorReport) {
	for _, name := range []string{systemd.XrayUnitTemplate, systemd.ClashUnitTemplate, systemd.SubUnit} {
		path := filepath.Join(systemd.DefaultUnitDir, name)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				report.Issues = append(report.Issues, "systemd unit is missing: "+path)
				continue
			}
			report.Issues = append(report.Issues, "systemd unit could not be checked: "+path+" "+err.Error())
		}
	}
	report.Checks = append(report.Checks, "systemd units checked")
}

// newIPInfoCommand 创建出口 IP 诊断命令。
func newIPInfoCommand() *cobra.Command {
	var family string
	var timeout float64
	command := &cobra.Command{
		Use:   "ipinfo STACK",
		Short: "Query outbound IP through a stack mihomo socks listener",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runIPInfoCommand(command, args[0], family, timeout)
		},
	}
	command.Flags().StringVar(&family, "family", "all", "IP family: all, ipv4, ipv6")
	command.Flags().Float64Var(&timeout, "timeout", 8.0, "Per-source curl timeout in seconds")
	return command
}

// runIPInfoCommand 执行 ipinfo 查询，并根据输出目标选择交互式状态或普通报告。
func runIPInfoCommand(command *cobra.Command, stackName string, family string, timeout float64) error {
	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	output := command.OutOrStdout()
	if shouldUseIPInfoInteractiveOutput(output) {
		renderer := newIPInfoStatusRenderer(output, true)
		_, err = queryIPInfo(command.Context(), diagnostics.QueryOptions{
			ConfigPath:       configPath,
			StackName:        stackName,
			Family:           family,
			Timeout:          timeout,
			ProgressCallback: renderer.Handle,
		})
		return err
	}
	report, err := queryIPInfo(command.Context(), diagnostics.QueryOptions{
		ConfigPath: configPath,
		StackName:  stackName,
		Family:     family,
		Timeout:    timeout,
	})
	if err != nil {
		return err
	}
	for _, line := range diagnostics.FormatIPInfoReport(report) {
		fmt.Fprintln(output, line)
	}
	return nil
}

// shouldUseIPInfoInteractiveOutput 判断输出目标是否支持 ANSI 原地刷新。
func shouldUseIPInfoInteractiveOutput(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0 && isatty.IsTerminal(file.Fd())
}

type ipInfoStatusRenderer struct {
	writer      io.Writer
	interactive bool
	rows        []string
	rowByFamily map[string]int
}

// newIPInfoStatusRenderer 创建 ipinfo family 状态渲染器。
func newIPInfoStatusRenderer(writer io.Writer, interactive bool) *ipInfoStatusRenderer {
	return &ipInfoStatusRenderer{
		writer:      writer,
		interactive: interactive,
		rows:        make([]string, 0, 2),
		rowByFamily: map[string]int{},
	}
}

// Handle 根据进度事件新增或替换对应 family 的固定两行状态块。
func (r *ipInfoStatusRenderer) Handle(progress diagnostics.IPInfoProgress) {
	block := formatIPInfoProgressBlock(progress)
	rowIndex, exists := r.rowByFamily[progress.Family]
	if !exists {
		r.rowByFamily[progress.Family] = len(r.rows)
		r.rows = append(r.rows, block[0], block[1])
		fmt.Fprintln(r.writer, block[0])
		fmt.Fprintln(r.writer, block[1])
		return
	}
	r.rows[rowIndex] = block[0]
	r.rows[rowIndex+1] = block[1]
	if !r.interactive {
		fmt.Fprintln(r.writer, block[0])
		fmt.Fprintln(r.writer, block[1])
		return
	}
	linesUp := len(r.rows) - rowIndex
	fmt.Fprintf(r.writer, "\x1b[%dA\r\x1b[2K%s\n\r\x1b[2K%s\x1b[%dB\r", linesUp, block[0], block[1], linesUp-1)
}

// formatIPInfoProgressBlock 格式化一个 family 的固定两行检测状态。
func formatIPInfoProgressBlock(progress diagnostics.IPInfoProgress) [2]string {
	label := progress.Label
	if label == "" {
		label = progress.Family
	}
	indent := strings.Repeat(" ", len(label)+2)
	if progress.State == diagnostics.IPInfoProgressDetecting {
		return [2]string{fmt.Sprintf("%s  Detecting ...", label), ""}
	}
	if progress.Err != nil {
		return [2]string{fmt.Sprintf("%s  Failed", label), indent + progress.Err.Error()}
	}
	result := progress.Result
	if result.Label == "" {
		result.Label = label
	}
	if result.Family == "" {
		result.Family = progress.Family
	}
	return [2]string{
		fmt.Sprintf("%s  %s", result.Label, firstNonEmptyString(result.IP, "IP not resolved")),
		indent + formatIPInfoRegionSummary(result.Region),
	}
}

// formatIPInfoRegionSummary 将来源解析出的地区字段压缩成适合状态块展示的一行。
func formatIPInfoRegionSummary(region string) string {
	parts := make([]string, 0)
	for _, part := range strings.Split(region, "/") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "Region unknown"
	}
	for index, part := range parts {
		if strings.HasPrefix(part, "AS") {
			location := strings.Join(parts[:index], ", ")
			asn := strings.Join(parts[index:], " / ")
			if location == "" {
				return asn
			}
			return location + " · " + asn
		}
	}
	return strings.Join(parts, ", ")
}

// firstNonEmptyString 返回第一段非空字符串。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
