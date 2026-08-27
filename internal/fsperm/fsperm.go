// Package fsperm 定义 proxystack 受管目录树的权限规格和修复原语。
//
// 单独成包是为了让 systemd、agentconfig、runtime 等都能引用同一份定义而不成环。
package fsperm

import (
	"fmt"
	"os"
	"path/filepath"
)

// SharedDirMode 是允许 proxystack 组成员免 sudo 写入的目录权限。
//
// 组写位让运维账号（加入 proxystack 组后）不用 sudo 就能改 stack 配置和运行时文件。
// setgid 是配套的必需项：组成员创建的文件默认带自己的主组，服务账号就读不到了；
// setgid 让目录下新建的文件和子目录统一继承 proxystack 组。
//
// 只有 stacks/、runtime/、publish/、sub/ 用它。base dir、bin/ 和 geo/ 一律保持
// ServiceDirMode：rename(2) 只检查父目录权限，父目录一旦组可写，组成员就能把
// bin/ 整个改名再建一个自己的，受管二进制的权限设置形同虚设——而那些二进制是
// systemd 以 proxystack 身份执行的。装二进制和 geo 数据本来就是 sudo 操作，
// 把这两个目录留在组外，代价为零。
const SharedDirMode = os.FileMode(0o770) | os.ModeSetgid

// ServiceDirMode 是只有服务账号能写的目录权限。
const ServiceDirMode = os.FileMode(0o750)

// FileMode 是受管普通文件的权限。
//
// 不给组写位：写文件一律走「建临时文件 + rename」，只需要目录写权限，
// 不需要就地改写已有文件，因此文件本身保持最小权限。
const FileMode = os.FileMode(0o640)

// ExecMode 是受管二进制的权限。
const ExecMode = os.FileMode(0o750)

// modeSpecialBits 是需要和权限位一起比较的特殊位。
//
// 规则里从来不设 setuid/sticky，但组可写目录下组成员能给自己的文件加上它们，
// 所以必须纳入比较，否则 doctor 看不见。
const modeSpecialBits = os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// Matches 判断实际权限是否符合规则，setuid/setgid/sticky 一并比较。
func Matches(actual os.FileMode, want os.FileMode) bool {
	return actual.Perm() == want.Perm() && actual&modeSpecialBits == want&modeSpecialBits
}

// Format 输出便于排查的权限文本，特殊位显式标出。
func Format(mode os.FileMode) string {
	text := fmt.Sprintf("%#o", mode.Perm())
	for _, special := range []struct {
		bit   os.FileMode
		label string
	}{
		{os.ModeSetuid, "+setuid"},
		{os.ModeSetgid, "+setgid"},
		{os.ModeSticky, "+sticky"},
	} {
		if mode&special.bit != 0 {
			text += special.label
		}
	}
	return text
}

// MkdirManaged 创建受管目录并确保权限符合规格。
//
// 不能只靠 os.MkdirAll：它的 mode 会被 umask 削掉（默认 umask 022 下 0o770 会
// 变成 0750），而且 mkdir(2) 从来不设置 setgid 位。所以必须建完再显式 chmod，
// 而且要覆盖 MkdirAll 顺带创建的每一级中间目录——只修最后一级的话，中间那些
// 仍然停在被 umask 削过的权限上。已存在的祖先目录不动，避免越权改别人的目录。
func MkdirManaged(path string, mode os.FileMode) error {
	created := missingAncestors(path)
	if err := os.MkdirAll(path, mode.Perm()); err != nil {
		return err
	}
	// 目标目录本身无论是否新建都要修正——存量安装升级正是靠这一步；
	// 已存在的祖先目录则不动，避免越权改别人的目录。
	for _, dir := range append(created, filepath.Clean(path)) {
		if err := ChmodIfDifferent(dir, mode); err != nil {
			return err
		}
	}
	return nil
}

// missingAncestors 自顶向下返回 path 上尚不存在、将由 MkdirAll 创建的目录。
func missingAncestors(path string) []string {
	missing := make([]string, 0)
	for current := filepath.Clean(path); ; {
		if _, err := os.Lstat(current); err == nil {
			break
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
		missing[i], missing[j] = missing[j], missing[i]
	}
	return missing
}

// ChmodIfDifferent 仅在实际权限与目标不同时执行 chmod；软链接一律跳过。
//
// chmod 要求调用者是文件 owner。受管目录组可写之后，文件可能由运维账号、root 或
// 服务账号任一方创建，无变化时也 chmod 会让非 owner 撞 EPERM。
func ChmodIfDifferent(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || Matches(info.Mode(), mode) {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
