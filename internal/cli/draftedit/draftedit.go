package draftedit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

// Options 保存一次草稿编辑会话所需的输入。
type Options struct {
	TargetPath string
	DraftPath  string
	Initial    []byte
	Mode       os.FileMode
	Editor     string
}

// Result 描述草稿编辑和提交后的结果。
type Result struct {
	DraftPath string
	Changed   bool
}

// ValidateFunc 校验草稿内容，失败时草稿会保留给用户继续编辑。
type ValidateFunc func(path string, data []byte) error

// CommitFunc 在校验通过后把草稿内容落盘到真实目标。
type CommitFunc func(data []byte) (bool, error)

// DraftPathFor 返回目标文件旁边的默认草稿路径。
func DraftPathFor(targetPath string) string {
	return targetPath + ".draft"
}

// EditAndCommit 先编辑草稿，校验通过后提交真实文件并删除草稿。
func EditAndCommit(options Options, validate ValidateFunc, commit CommitFunc) (Result, error) {
	draftPath, err := prepareDraft(options)
	if err != nil {
		return Result{}, err
	}
	if err := RunEditor(options.Editor, draftPath); err != nil {
		return Result{DraftPath: draftPath}, preservedDraftError(draftPath, err)
	}
	data, err := os.ReadFile(draftPath)
	if err != nil {
		return Result{DraftPath: draftPath}, preservedDraftError(draftPath, err)
	}
	if err := validate(draftPath, data); err != nil {
		return Result{DraftPath: draftPath}, preservedDraftError(draftPath, err)
	}
	changed, err := commit(data)
	if err != nil {
		return Result{DraftPath: draftPath}, preservedDraftError(draftPath, err)
	}
	if err := os.Remove(draftPath); err != nil && !os.IsNotExist(err) {
		return Result{DraftPath: draftPath, Changed: changed}, err
	}
	return Result{DraftPath: draftPath, Changed: changed}, nil
}

// preservedDraftError 保留原始错误链，同时告诉用户草稿文件位置。
func preservedDraftError(draftPath string, err error) error {
	return fmt.Errorf("%w; draft preserved: %s", err, draftPath)
}

// RunEditor 执行用户指定或环境默认编辑器。
func RunEditor(editor string, targetPath string) error {
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts, err := SplitCommandLine(editor)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("editor command is empty")
	}
	command := exec.Command(parts[0], append(parts[1:], targetPath)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// SplitCommandLine 解析简单 shell 风格命令行，支持空白分隔、引号和反斜杠转义。
func SplitCommandLine(value string) ([]string, error) {
	parts := make([]string, 0)
	var builder strings.Builder
	var quote rune
	escaped := false
	inToken := false
	for _, r := range value {
		if escaped {
			builder.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}
		if r == '\\' {
			escaped = true
			inToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			builder.WriteRune(r)
			inToken = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			inToken = true
			continue
		}
		if unicode.IsSpace(r) {
			if inToken {
				parts = append(parts, builder.String())
				builder.Reset()
				inToken = false
			}
			continue
		}
		builder.WriteRune(r)
		inToken = true
	}
	if escaped {
		builder.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("editor command contains unterminated quote")
	}
	if inToken {
		parts = append(parts, builder.String())
	}
	return parts, nil
}

// prepareDraft 写入首次草稿；已有草稿会原样复用，避免覆盖用户上次保存结果。
func prepareDraft(options Options) (string, error) {
	draftPath := options.DraftPath
	if draftPath == "" {
		draftPath = DraftPathFor(options.TargetPath)
	}
	if draftPath == "" {
		return "", fmt.Errorf("draft path is required")
	}
	info, err := os.Lstat(draftPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("draft must be a regular file: %s", draftPath)
		}
		return draftPath, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	mode := options.Mode
	if mode == 0 {
		mode = 0o640
	}
	if err := os.MkdirAll(filepath.Dir(draftPath), 0o750); err != nil {
		return "", err
	}
	return draftPath, os.WriteFile(draftPath, options.Initial, mode)
}
