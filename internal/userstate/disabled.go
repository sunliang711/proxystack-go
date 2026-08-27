// Package userstate 管理 runtime/disabled.json，记录被临时禁用的用户。
//
// 禁用状态是 runtime 覆盖而不是配置：config.yaml 和 stack 文件仍然由人手写，
// 这里只记录“谁在哪个 stack 上被暂时停掉”，生成 Xray 配置时据此过滤。
package userstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/eagle/proxystack-go/internal/domain"
)

// StateVersion 是 disabled.json 的结构版本。
const StateVersion = 1

// Entry 记录单条禁用状态；Stack 始终是具体 stack 名，不使用通配。
type Entry struct {
	User  string `json:"user"`
	Stack string `json:"stack"`
	Since string `json:"since"`
}

// State 是 disabled.json 的完整内容。
type State struct {
	Version  int     `json:"version"`
	Disabled []Entry `json:"disabled"`
}

// Path 返回全局配置对应的 disabled.json 路径。
func Path(config domain.GlobalConfig) string {
	return filepath.Join(config.ResolvePath(config.Paths.Runtime), "disabled.json")
}

// Load 读取禁用状态；文件不存在时返回空状态，便于首次使用。
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{Version: StateVersion, Disabled: []Entry{}}, nil
		}
		return State{}, fmt.Errorf("disabled state could not be read: %s (%w)", path, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("disabled state contains invalid JSON: %s\n%w", path, err)
	}
	if state.Version != StateVersion {
		return State{}, fmt.Errorf("unsupported disabled state version: %d (%s)", state.Version, path)
	}
	for _, entry := range state.Disabled {
		if entry.User == "" || entry.Stack == "" {
			return State{}, fmt.Errorf("disabled state entry requires user and stack: %s", path)
		}
	}
	state.sort()
	return state, nil
}

// Save 原子写入禁用状态，保持条目顺序稳定。
func Save(path string, state State) error {
	state.Version = StateVersion
	if state.Disabled == nil {
		state.Disabled = []Entry{}
	}
	state.sort()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o640)
}

// IsDisabled 判断某 stack 上的用户是否被禁用。
func (s State) IsDisabled(stack string, user string) bool {
	_, ok := s.EntryFor(stack, user)
	return ok
}

// EntryFor 返回某 stack 上用户对应的禁用条目。
func (s State) EntryFor(stack string, user string) (Entry, bool) {
	for _, entry := range s.Disabled {
		if entry.Stack == stack && entry.User == user {
			return entry, true
		}
	}
	return Entry{}, false
}

// Disable 记录禁用状态，已存在时返回 false 表示无变化。
func (s *State) Disable(stack string, user string, since string) bool {
	if s.IsDisabled(stack, user) {
		return false
	}
	s.Disabled = append(s.Disabled, Entry{User: user, Stack: stack, Since: since})
	s.sort()
	return true
}

// Enable 移除禁用状态，不存在时返回 false 表示无变化。
func (s *State) Enable(stack string, user string) bool {
	kept := make([]Entry, 0, len(s.Disabled))
	changed := false
	for _, entry := range s.Disabled {
		if entry.Stack == stack && entry.User == user {
			changed = true
			continue
		}
		kept = append(kept, entry)
	}
	s.Disabled = kept
	return changed
}

// UserSet 转换为生成器使用的禁用集合。
func (s State) UserSet() domain.DisabledUserSet {
	if len(s.Disabled) == 0 {
		return nil
	}
	users := domain.DisabledUserSet{}
	for _, entry := range s.Disabled {
		users.Add(entry.Stack, entry.User)
	}
	return users
}

func (s *State) sort() {
	sort.Slice(s.Disabled, func(i int, j int) bool {
		if s.Disabled[i].Stack != s.Disabled[j].Stack {
			return s.Disabled[i].Stack < s.Disabled[j].Stack
		}
		return s.Disabled[i].User < s.Disabled[j].User
	})
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
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
