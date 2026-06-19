package sub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
	"github.com/spf13/cobra"
)

// newInputCommand 创建 inputs 文件级查询和维护命令集合。
func newInputCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "input",
		Short: "Inspect and edit subscription input files",
	}
	command.AddCommand(newInputListCommand())
	command.AddCommand(newInputShowCommand())
	command.AddCommand(newInputValidateCommand())
	command.AddCommand(newInputEditCommand())
	command.AddCommand(newInputSetHostCommand())
	command.AddCommand(newInputRemoveCommand())
	return command
}

// newInputListCommand 创建列出 input 文件摘要的只读命令。
func newInputListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List subscription input files",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			paths, err := scanSubInputFiles(inputDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Subscription inputs: count=%d\n", len(paths))
			for _, path := range paths {
				input, err := subgen.LoadInputFile(path)
				if err != nil {
					return fmt.Errorf("subscription input list failed: %s: %w", filepath.Base(path), err)
				}
				fmt.Fprintf(command.OutOrStdout(), "- file=%s source=%s nodes=%d users=%s generated_at=%s\n", filepath.Base(path), input.Source, len(input.Nodes), strings.Join(subInputUsers(input), ","), input.GeneratedAt)
			}
			return nil
		},
	}
}

// newInputShowCommand 创建查看单个 input 的命令，默认脱敏摘要输出。
func newInputShowCommand() *cobra.Command {
	var raw bool
	var showSecrets bool
	command := &cobra.Command{
		Use:   "show SOURCE",
		Short: "Show one subscription input file",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			path, err := resolveSubInputPath(inputDir, args[0])
			if err != nil {
				return err
			}
			if raw {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				_, err = command.OutOrStdout().Write(data)
				return err
			}
			input, err := subgen.LoadInputFile(path)
			if err != nil {
				return err
			}
			if !showSecrets {
				input = redactSubInputSecrets(input)
			}
			_, err = fmt.Fprint(command.OutOrStdout(), subgen.InputToYAML(input))
			return err
		},
	}
	command.Flags().BoolVar(&raw, "raw", false, "Print raw input file content")
	command.Flags().BoolVar(&showSecrets, "show-secrets", false, "Print sensitive values in summary output")
	return command
}

// newInputValidateCommand 创建 input 严格解析和合并校验命令。
func newInputValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [SOURCE]",
		Short: "Validate subscription input files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				inputs, err := loadSubInputsFromSafeDir(inputDir)
				if err != nil {
					return fmt.Errorf("subscription inputs validation failed: %s: %w", inputDir, err)
				}
				index, err := subgen.MergeInputs(inputs, subgen.Access{Type: "none"}, nowISO())
				if err != nil {
					return fmt.Errorf("subscription inputs validation failed: %s: %w", inputDir, err)
				}
				fmt.Fprintf(command.OutOrStdout(), "Subscription inputs OK: sources=%d nodes=%d users=%d\n", len(index.Sources), len(index.Nodes), len(index.Users))
				return nil
			}
			path, err := resolveSubInputPath(inputDir, args[0])
			if err != nil {
				return err
			}
			input, err := subgen.LoadInputFile(path)
			if err != nil {
				return fmt.Errorf("subscription input validation failed: %s: %w", filepath.Base(path), err)
			}
			index, err := subgen.MergeInputs([]subgen.InputFile{{Name: filepath.Base(path), Input: input}}, subgen.Access{Type: "none"}, nowISO())
			if err != nil {
				return fmt.Errorf("subscription input validation failed: %s: %w", filepath.Base(path), err)
			}
			fmt.Fprintf(command.OutOrStdout(), "Subscription input OK: file=%s source=%s nodes=%d users=%d\n", filepath.Base(path), input.Source, len(index.Nodes), len(index.Users))
			return nil
		},
	}
}

// newInputEditCommand 创建通过临时文件编辑 input 的命令，校验通过后才覆盖原文件。
func newInputEditCommand() *cobra.Command {
	var editor string
	command := &cobra.Command{
		Use:   "edit SOURCE",
		Short: "Edit one subscription input file",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			path, err := resolveSubInputPath(inputDir, args[0])
			if err != nil {
				return err
			}
			changed, err := editSubInput(path, editor)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(command.OutOrStdout(), "Input updated: %s\n", path)
				return nil
			}
			fmt.Fprintf(command.OutOrStdout(), "Input OK: %s\n", path)
			return nil
		},
	}
	command.Flags().StringVar(&editor, "editor", "", "Editor command")
	return command
}

// newInputSetHostCommand 创建批量修改 input 节点 server 的命令。
func newInputSetHostCommand() *cobra.Command {
	var all bool
	command := &cobra.Command{
		Use:   "set-host HOST [SOURCE]",
		Short: "Set subscription input node host",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			host := strings.TrimSpace(args[0])
			if host == "" {
				return fmt.Errorf("input host must not be empty")
			}
			if all && len(args) == 2 {
				return fmt.Errorf("SOURCE and --all cannot be used together")
			}
			if !all && len(args) == 1 {
				return fmt.Errorf("SOURCE or --all is required")
			}
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			var paths []string
			if all {
				paths, err = scanSubInputFiles(inputDir)
			} else {
				var path string
				path, err = resolveSubInputPath(inputDir, args[1])
				paths = []string{path}
			}
			if err != nil {
				return err
			}
			changed, unchanged, err := setSubInputHostFiles(paths, host)
			if err != nil {
				return err
			}
			if all {
				if changed == 0 {
					fmt.Fprintf(command.OutOrStdout(), "Input hosts unchanged: total=%d\n", unchanged)
					return nil
				}
				fmt.Fprintf(command.OutOrStdout(), "Input hosts updated: changed=%d unchanged=%d total=%d\n", changed, unchanged, changed+unchanged)
				return nil
			}
			if changed == 0 {
				fmt.Fprintf(command.OutOrStdout(), "Input host unchanged: %s\n", filepath.Base(paths[0]))
				return nil
			}
			fmt.Fprintf(command.OutOrStdout(), "Input host updated: %s\n", filepath.Base(paths[0]))
			return nil
		},
	}
	command.Flags().BoolVar(&all, "all", false, "Set host on all subscription input files")
	return command
}

// newInputRemoveCommand 创建删除单个 input 文件的命令。
func newInputRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove SOURCE",
		Short: "Remove one subscription input file",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			inputDir, err := subInputDir(command)
			if err != nil {
				return err
			}
			path, err := resolveSubInputPath(inputDir, args[0])
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Input removed: %s\n", filepath.Base(path))
			return nil
		},
	}
}

// subInputDir 返回当前 base dir 下固定的 inputs 路径。
func subInputDir(command *cobra.Command) (string, error) {
	baseDir, err := subBaseDir(command)
	if err != nil {
		return "", err
	}
	return filepath.Join(subDataDir(baseDir), "inputs"), nil
}

// editSubInput 把原 input 复制到临时文件编辑，严格校验通过后再原子替换。
func editSubInput(path string, editor string) (bool, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	temp, err := os.CreateTemp("", "proxystack-sub-input-edit-*"+filepath.Ext(path))
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	writeErr := func() error {
		if _, err := temp.Write(original); err != nil {
			return err
		}
		if info, err := os.Stat(path); err == nil {
			if err := temp.Chmod(info.Mode().Perm()); err != nil {
				return err
			}
		}
		return nil
	}()
	closeErr := temp.Close()
	if writeErr != nil {
		return false, writeErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if err := runEditor(editor, tempPath); err != nil {
		return false, err
	}
	edited, err := os.ReadFile(tempPath)
	if err != nil {
		return false, err
	}
	if _, err := validateSubInputContent(filepath.Base(path), edited); err != nil {
		return false, err
	}
	return writeTextFileIfChanged(path, edited)
}

// subInputHostUpdate 保存一次 set-host 预校验后的待写入内容。
type subInputHostUpdate struct {
	Path    string
	Data    []byte
	Changed bool
}

// setSubInputHostFiles 先校验全部目标 input，再把所有节点 server 写成指定 host。
func setSubInputHostFiles(paths []string, host string) (int, int, error) {
	updates := make([]subInputHostUpdate, 0, len(paths))
	inputs := make([]subgen.InputFile, 0, len(paths))
	for _, path := range paths {
		input, err := subgen.LoadInputFile(path)
		if err != nil {
			return 0, 0, fmt.Errorf("subscription input host update failed: %s: %w", filepath.Base(path), err)
		}
		changed := setSubInputHost(&input, host)
		data, err := subInputWriteData(path, input)
		if err != nil {
			return 0, 0, fmt.Errorf("subscription input host update failed: %s: %w", filepath.Base(path), err)
		}
		validatedInput, err := validateSubInputContent(filepath.Base(path), data)
		if err != nil {
			return 0, 0, fmt.Errorf("subscription input host update failed: %s: %w", filepath.Base(path), err)
		}
		updates = append(updates, subInputHostUpdate{Path: path, Data: data, Changed: changed})
		inputs = append(inputs, subgen.InputFile{Name: filepath.Base(path), Input: validatedInput})
	}
	if _, err := subgen.MergeInputs(inputs, subgen.Access{Type: "none"}, nowISO()); err != nil {
		return 0, 0, fmt.Errorf("subscription input host update failed: %w", err)
	}
	changedCount := 0
	unchangedCount := 0
	for _, update := range updates {
		if !update.Changed {
			unchangedCount++
			continue
		}
		changed, err := writeTextFileIfChanged(update.Path, update.Data)
		if err != nil {
			return changedCount, unchangedCount, fmt.Errorf("subscription input host write failed: %s: %w", filepath.Base(update.Path), err)
		}
		if changed {
			changedCount++
		} else {
			unchangedCount++
		}
	}
	return changedCount, unchangedCount, nil
}

// setSubInputHost 修改 input 内全部节点的 server，并返回是否存在语义变化。
func setSubInputHost(input *subgen.Input, host string) bool {
	changed := false
	if input.ExternalHost != "" && input.ExternalHost != host {
		input.ExternalHost = host
		changed = true
	}
	for index := range input.Nodes {
		if input.Nodes[index].Server == host {
			continue
		}
		input.Nodes[index].Server = host
		changed = true
	}
	return changed
}

// subInputWriteData 按文件扩展名生成可再次严格读取的规范内容。
func subInputWriteData(path string, input subgen.Input) ([]byte, error) {
	if filepath.Ext(path) == ".json" {
		data, err := json.MarshalIndent(input, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	}
	return []byte(subgen.InputToYAML(input)), nil
}

// redactSubInputSecrets 返回已脱敏的 input 副本，用于默认 show 摘要输出。
func redactSubInputSecrets(input subgen.Input) subgen.Input {
	for index := range input.Nodes {
		if input.Nodes[index].UUID != "" {
			input.Nodes[index].UUID = "[REDACTED]"
		}
		if input.Nodes[index].Password != "" {
			input.Nodes[index].Password = "[REDACTED]"
		}
		if input.Nodes[index].Auth != nil && input.Nodes[index].Auth.Password != "" {
			auth := *input.Nodes[index].Auth
			auth.Password = "[REDACTED]"
			input.Nodes[index].Auth = &auth
		}
	}
	return input
}

// resolveSubInputPath 把 SOURCE 解析为 inputs 目录下的安全 input 文件路径。
func resolveSubInputPath(inputDir string, source string) (string, error) {
	if err := validateSubInputSource(source); err != nil {
		return "", err
	}
	if subInputExt(source) != "" {
		return checkedSubInputPath(inputDir, source)
	}
	paths, err := scanSubInputFiles(inputDir)
	if err != nil {
		return "", err
	}
	matches := make([]string, 0)
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.TrimSuffix(name, filepath.Ext(name)) == source {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		for _, path := range paths {
			input, err := subgen.LoadInputFile(path)
			if err != nil {
				return "", fmt.Errorf("subscription input source lookup failed: %s: %w", filepath.Base(path), err)
			}
			if input.Source == source {
				matches = append(matches, path)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("subscription input does not exist: %s", source)
	case 1:
		return checkedSubInputPath(inputDir, filepath.Base(matches[0]))
	default:
		return "", fmt.Errorf("ambiguous subscription input source: %s matched %s", source, strings.Join(subInputBaseNames(matches), ", "))
	}
}

// validateSubInputSource 拒绝绝对路径、路径分隔符和明显的路径穿越片段。
func validateSubInputSource(source string) error {
	if source == "" || filepath.IsAbs(source) || filepath.Base(source) != source || strings.Contains(source, "\\") || strings.Contains(source, "/") || strings.Contains(source, "\x00") || source == "." || source == ".." {
		return fmt.Errorf("unsafe subscription input source: %s", source)
	}
	extension := filepath.Ext(source)
	if extension != "" && subInputExt(source) == "" {
		return fmt.Errorf("unsupported input extension: %s", source)
	}
	return nil
}

// checkedSubInputPath 确认目标是 inputs 目录内支持扩展名的普通文件。
func checkedSubInputPath(inputDir string, name string) (string, error) {
	if err := ensureSubInputDir(inputDir); err != nil {
		return "", err
	}
	if err := validateSubInputSource(name); err != nil {
		return "", err
	}
	if subInputExt(name) == "" {
		return "", fmt.Errorf("unsupported input extension: %s", name)
	}
	path := filepath.Join(inputDir, name)
	if err := ensurePathInsideDir(inputDir, path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("subscription input does not exist: %s", name)
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("subscription input must be a regular file: %s", name)
	}
	return path, nil
}

// scanSubInputFiles 扫描 inputs 目录中的安全 input 文件并按文件名排序。
func scanSubInputFiles(inputDir string) ([]string, error) {
	if err := ensureSubInputDir(inputDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("input directory could not be read: %s (%w)", inputDir, err)
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || subInputExt(entry.Name()) == "" {
			continue
		}
		path := filepath.Join(inputDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// ensureSubInputDir 确认 inputs 是真实目录，避免通过目录符号链接逃逸。
func ensureSubInputDir(inputDir string) error {
	info, err := os.Lstat(inputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("input directory does not exist: %s", inputDir)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("input directory must not be a symlink: %s", inputDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("input path must be a directory: %s", inputDir)
	}
	return nil
}

// ensurePathInsideDir 校验目标路径没有逃出 inputs 目录。
func ensurePathInsideDir(inputDir string, path string) error {
	inputDirAbs, err := filepath.Abs(inputDir)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(inputDirAbs, pathAbs)
	if err != nil {
		return err
	}
	if relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || relative == ".." {
		return fmt.Errorf("unsafe subscription input path: %s", path)
	}
	return nil
}

// loadSubInputsFromSafeDir 使用 CLI 安全扫描规则读取全部 input。
func loadSubInputsFromSafeDir(inputDir string) ([]subgen.InputFile, error) {
	paths, err := scanSubInputFiles(inputDir)
	if err != nil {
		return nil, err
	}
	inputs := make([]subgen.InputFile, 0, len(paths))
	for _, path := range paths {
		input, err := subgen.LoadInputFile(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, subgen.InputFile{Name: filepath.Base(path), Input: input})
	}
	return inputs, nil
}

// validateSubInputContent 执行与单文件 validate 等价的 schema 和合并校验。
func validateSubInputContent(name string, data []byte) (subgen.Input, error) {
	input, err := subgen.LoadInputContent(name, data)
	if err != nil {
		return subgen.Input{}, err
	}
	if _, err := subgen.MergeInputs([]subgen.InputFile{{Name: name, Input: input}}, subgen.Access{Type: "none"}, nowISO()); err != nil {
		return subgen.Input{}, err
	}
	return input, nil
}

// subInputExt 返回受支持的 input 文件扩展名。
func subInputExt(name string) string {
	extension := filepath.Ext(name)
	switch extension {
	case ".yaml", ".yml", ".json":
		return extension
	default:
		return ""
	}
}

// subInputUsers 返回 input 中出现过的用户列表，用于 list 摘要展示。
func subInputUsers(input subgen.Input) []string {
	seen := map[string]bool{}
	for _, node := range input.Nodes {
		seen[node.User] = true
	}
	users := make([]string, 0, len(seen))
	for user := range seen {
		users = append(users, user)
	}
	sort.Strings(users)
	return users
}

// subInputBaseNames 返回匹配路径的文件名列表，用于错误提示。
func subInputBaseNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	return names
}
