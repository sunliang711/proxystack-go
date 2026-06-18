package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	mihomogen "github.com/eagle/proxystack-go/internal/generator/mihomo"
	xraygen "github.com/eagle/proxystack-go/internal/generator/xray"
	"github.com/eagle/proxystack-go/internal/graph"
	"gopkg.in/yaml.v3"
)

const (
	ManifestVersion = 1

	ActionCreate   = "create"
	ActionUpdate   = "update"
	ActionDelete   = "delete"
	ActionNoChange = "no-change"
)

// GeneratedFile 表示 runtime plan 期望存在的一份生成文件。
type GeneratedFile struct {
	Path         string
	RelativePath string
	Service      string
	Content      []byte
	Mode         os.FileMode
}

// FileChange 表示生成文件相对当前 manifest 和磁盘内容的变化。
type FileChange struct {
	Action       string
	Path         string
	RelativePath string
	Service      string
	OldSHA256    string
	NewSHA256    string
	Content      []byte
	Mode         os.FileMode
}

// ManifestFile 是 manifest 中记录的一份生成文件。
type ManifestFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	Service      string `json:"service"`
}

// Manifest 记录上一次 apply 后的生成结果和输入 hash。
type Manifest struct {
	ManifestVersion int               `json:"manifest_version"`
	ConfigHash      string            `json:"config_hash"`
	StackHashes     map[string]string `json:"stack_hashes"`
	GeneratedAt     string            `json:"generated_at"`
	Files           []ManifestFile    `json:"files"`
}

// Plan 是 check/start/restart 共享的 runtime 中间结果。
type Plan struct {
	Config         domain.GlobalConfig
	StackSet       domain.StackSet
	Scope          graph.TargetScope
	GeneratedFiles []GeneratedFile
	Changes        []FileChange
	Manifest       Manifest
	OldManifest    *Manifest
}

// BuildOptions 控制 runtime plan 的加载路径、目标和端口检查行为。
type BuildOptions struct {
	ConfigPath      string
	Target          string
	SkipSystemPorts bool
	Now             func() time.Time
}

// BuildPlan 加载配置、解析 target、生成期望文件并计算 manifest diff。
func BuildPlan(options BuildOptions) (Plan, error) {
	cfg, err := config.LoadConfig(options.ConfigPath)
	if err != nil {
		return Plan{}, err
	}
	stackSet, err := config.LoadStacks(cfg, !options.SkipSystemPorts)
	if err != nil {
		return Plan{}, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return Plan{}, err
	}
	scope, err := graph.ResolveTargetScope(referenceGraph, options.Target)
	if err != nil {
		return Plan{}, err
	}
	files, err := BuildGeneratedFiles(stackSet, scope)
	if err != nil {
		return Plan{}, err
	}
	manifestPath := ManifestPath(cfg)
	oldManifest, err := ReadManifest(manifestPath)
	if err != nil {
		return Plan{}, err
	}
	changes, err := DiffGeneratedFiles(files, oldManifest, scope)
	if err != nil {
		return Plan{}, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	manifest := BuildManifest(stackSet, scope, files, oldManifest, changes, now().Local().Format(time.RFC3339))
	return Plan{
		Config:         cfg,
		StackSet:       stackSet,
		Scope:          scope,
		GeneratedFiles: files,
		Changes:        changes,
		Manifest:       manifest,
		OldManifest:    oldManifest,
	}, nil
}

// ManifestPath 返回全局配置对应的 runtime manifest 路径。
func ManifestPath(cfg domain.GlobalConfig) string {
	return filepath.Join(cfg.ResolvePath(cfg.Paths.Runtime), "manifest.json")
}

// BuildGeneratedFiles 为 target scope 中的 xrelay/clash 服务生成稳定内容。
func BuildGeneratedFiles(stackSet domain.StackSet, scope graph.TargetScope) ([]GeneratedFile, error) {
	generatedDir := stackSet.Config.ResolvePath(stackSet.Config.Paths.Generated)
	files := make([]GeneratedFile, 0, len(scope.Nodes))
	for _, node := range scope.Nodes {
		switch node.Component {
		case "xrelay":
			content, err := xraygen.DumpsConfig(stackSet, node.Stack)
			if err != nil {
				return nil, err
			}
			files = append(files, GeneratedFile{
				Path:         filepath.Join(generatedDir, "xray", node.Stack+".json"),
				RelativePath: filepath.ToSlash(filepath.Join("generated", "xray", node.Stack+".json")),
				Service:      node.ServiceName(),
				Content:      []byte(content),
				Mode:         0o640,
			})
		case "clash":
			content, err := mihomogen.DumpsConfig(stackSet, node.Stack)
			if err != nil {
				return nil, err
			}
			files = append(files, GeneratedFile{
				Path:         filepath.Join(generatedDir, "mihomo", node.Stack+".yaml"),
				RelativePath: filepath.ToSlash(filepath.Join("generated", "mihomo", node.Stack+".yaml")),
				Service:      node.ServiceName(),
				Content:      []byte(content),
				Mode:         0o640,
			})
		}
	}
	sort.Slice(files, func(i int, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	return files, nil
}

// ReadManifest 读取 manifest；文件不存在时返回 nil，便于首轮 create diff。
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("manifest could not be read: %s (%w)", path, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("manifest contains invalid JSON: %s\n%w", path, err)
	}
	if manifest.ManifestVersion != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version: %d", manifest.ManifestVersion)
	}
	return &manifest, nil
}

// DiffGeneratedFiles 对比期望文件、磁盘内容和旧 manifest，生成稳定变化列表。
func DiffGeneratedFiles(files []GeneratedFile, oldManifest *Manifest, scope graph.TargetScope) ([]FileChange, error) {
	changes := make([]FileChange, 0, len(files))
	expected := make(map[string]GeneratedFile, len(files))
	for _, file := range files {
		expected[file.RelativePath] = file
		oldSHA := ""
		if oldManifest != nil {
			if oldFile, ok := oldManifest.FileByRelativePath(file.RelativePath); ok {
				oldSHA = oldFile.SHA256
			}
		}
		newSHA := sha256Bytes(file.Content)
		action := ActionNoChange
		current, err := os.ReadFile(file.Path)
		if err != nil {
			if os.IsNotExist(err) {
				action = ActionCreate
			} else {
				return nil, fmt.Errorf("generated file could not be read: %s (%w)", file.Path, err)
			}
		} else if sha256Bytes(current) != newSHA {
			action = ActionUpdate
		}
		changes = append(changes, FileChange{
			Action:       action,
			Path:         file.Path,
			RelativePath: file.RelativePath,
			Service:      file.Service,
			OldSHA256:    oldSHA,
			NewSHA256:    newSHA,
			Content:      file.Content,
			Mode:         file.Mode,
		})
	}
	if oldManifest != nil {
		for _, file := range oldManifest.Files {
			if !manifestFileMatchesTarget(file, scope) {
				continue
			}
			if _, ok := expected[file.RelativePath]; ok {
				continue
			}
			changes = append(changes, FileChange{
				Action:       ActionDelete,
				Path:         file.Path,
				RelativePath: file.RelativePath,
				Service:      file.Service,
				OldSHA256:    file.SHA256,
			})
		}
	}
	sort.Slice(changes, func(i int, j int) bool {
		if changes[i].RelativePath != changes[j].RelativePath {
			return changes[i].RelativePath < changes[j].RelativePath
		}
		return changes[i].Action < changes[j].Action
	})
	return changes, nil
}

// FileByRelativePath 按 relative_path 查询 manifest 文件项。
func (m Manifest) FileByRelativePath(relativePath string) (ManifestFile, bool) {
	for _, file := range m.Files {
		if file.RelativePath == relativePath {
			return file, true
		}
	}
	return ManifestFile{}, false
}

// BuildManifest 根据当前 scope 文件和旧 manifest 构造要写入的 manifest。
func BuildManifest(stackSet domain.StackSet, scope graph.TargetScope, files []GeneratedFile, oldManifest *Manifest, changes []FileChange, generatedAt string) Manifest {
	if oldManifest != nil && !hasContentChange(changes) {
		generatedAt = oldManifest.GeneratedAt
	}
	manifestFiles := make([]ManifestFile, 0, len(files))
	if oldManifest != nil {
		for _, file := range oldManifest.Files {
			if !manifestFileMatchesTarget(file, scope) {
				manifestFiles = append(manifestFiles, file)
			}
		}
	}
	for _, file := range files {
		manifestFiles = append(manifestFiles, ManifestFile{
			Path:         file.Path,
			RelativePath: file.RelativePath,
			SHA256:       sha256Bytes(file.Content),
			Service:      file.Service,
		})
	}
	sort.Slice(manifestFiles, func(i int, j int) bool { return manifestFiles[i].RelativePath < manifestFiles[j].RelativePath })
	return Manifest{
		ManifestVersion: ManifestVersion,
		ConfigHash:      hashYAML(stackSet.Config),
		StackHashes:     stackHashes(stackSet.Stacks),
		GeneratedAt:     generatedAt,
		Files:           manifestFiles,
	}
}

// ApplyPlan 原子写入变化文件、删除 scope 内失效文件，并更新 manifest。
func ApplyPlan(plan Plan) error {
	for _, change := range plan.Changes {
		switch change.Action {
		case ActionCreate, ActionUpdate:
			if err := writeFileAtomic(change.Path, change.Content, change.Mode); err != nil {
				return err
			}
		case ActionDelete:
			if err := os.Remove(change.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
		case ActionNoChange:
			if err := os.Chmod(change.Path, change.Mode); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	data, err := json.MarshalIndent(plan.Manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ManifestPath(plan.Config), append(data, '\n'), 0o640)
}

// ChangedServices 返回 create/update/delete 影响到的 systemd 服务名。
func (p Plan) ChangedServices() []string {
	seen := map[string]bool{}
	services := make([]string, 0)
	for _, change := range p.Changes {
		if change.Action == ActionNoChange || change.Service == "" || seen[change.Service] {
			continue
		}
		seen[change.Service] = true
		services = append(services, change.Service)
	}
	sort.Strings(services)
	return services
}

func scopeServiceSet(scope graph.TargetScope) map[string]bool {
	services := make(map[string]bool, len(scope.Nodes))
	for _, node := range scope.Nodes {
		services[node.ServiceName()] = true
	}
	return services
}

func manifestFileMatchesTarget(file ManifestFile, scope graph.TargetScope) bool {
	target := scope.Raw
	if target == "" || target == "all" {
		return true
	}
	if strings.HasPrefix(target, "xrelay/") {
		stack := strings.TrimPrefix(target, "xrelay/")
		return file.Service == "proxystack-xray@"+stack+".service"
	}
	if strings.HasPrefix(target, "clash/") {
		stack := strings.TrimPrefix(target, "clash/")
		return file.Service == "proxystack-clash@"+stack+".service"
	}
	return strings.Contains(file.Service, "@"+target+".service")
}

func hasContentChange(changes []FileChange) bool {
	for _, change := range changes {
		if change.Action != ActionNoChange {
			return true
		}
	}
	return false
}

func stackHashes(stacks []domain.Stack) map[string]string {
	hashes := make(map[string]string, len(stacks))
	for _, stack := range stacks {
		hashes[stack.Name] = hashYAML(stack)
	}
	return hashes
}

func hashYAML(value any) string {
	data, _ := yaml.Marshal(value)
	return sha256Bytes(data)
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
