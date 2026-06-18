package subserver

import (
	"path/filepath"
	"sort"
	"sync"

	"github.com/eagle/proxystack-go/internal/config"
	subgen "github.com/eagle/proxystack-go/internal/generator/sub"
)

// State 保存订阅服务当前可用 index，reload 失败时保留上一份成功状态。
type State struct {
	mu        sync.RWMutex
	index     *subgen.Index
	lastError string
	dataDir   string
	access    subgen.Access
	now       func() string
}

// NewState 创建订阅状态容器。
func NewState(dataDir string, access subgen.Access, now func() string) *State {
	return &State{dataDir: dataDir, access: access, now: now}
}

// Load 初次加载 inputs，失败时不写入 index。
func (s *State) Load() error {
	index, err := subgen.MergeInputFiles(filepath.Join(s.dataDir, "inputs"), s.access, s.now())
	if err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.index = &index
	s.lastError = ""
	s.mu.Unlock()
	return nil
}

// Reload 运行期重载 inputs；失败时保留旧 index，只更新 last_error。
func (s *State) Reload() error {
	index, err := subgen.MergeInputFiles(filepath.Join(s.dataDir, "inputs"), s.access, s.now())
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastError = err.Error()
		return err
	}
	s.index = &index
	s.lastError = ""
	return nil
}

// Snapshot 返回当前 index 副本、用户列表和最近错误。
func (s *State) Snapshot() (*subgen.Index, []string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]string, 0)
	if s.index != nil {
		for user := range s.index.Users {
			users = append(users, user)
		}
	}
	sort.Strings(users)
	if s.index == nil {
		return nil, users, s.lastError
	}
	index := *s.index
	return &index, users, s.lastError
}

func accessFromConfig(access config.AccessConfig) subgen.Access {
	return subgen.Access{Type: access.Type, Token: access.Token}.Normalized()
}
