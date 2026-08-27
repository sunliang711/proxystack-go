package subserver

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher 监听 inputs 目录变化并防抖触发 reload。
type Watcher struct {
	dataDir  string
	interval time.Duration
	debounce time.Duration
	reload   func() error

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewWatcher 创建可停止的订阅 input watcher。
func NewWatcher(dataDir string, interval time.Duration, debounce time.Duration, reload func() error) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if debounce < 0 {
		debounce = 0
	}
	return &Watcher{
		dataDir:  dataDir,
		interval: interval,
		debounce: debounce,
		reload:   reload,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start 启动 watcher goroutine。
func (w *Watcher) Start() error {
	inputDir := filepath.Join(w.dataDir, "inputs")
	if err := os.MkdirAll(inputDir, 0o770); err != nil {
		return err
	}
	fileWatcher, _ := fsnotify.NewWatcher()
	if fileWatcher != nil {
		_ = fileWatcher.Add(inputDir)
	}
	go w.run(inputDir, fileWatcher)
	return nil
}

// Stop 停止 watcher 并等待 goroutine 退出。
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		<-w.doneCh
	})
}

func (w *Watcher) run(inputDir string, fileWatcher *fsnotify.Watcher) {
	defer close(w.doneCh)
	if fileWatcher != nil {
		defer fileWatcher.Close()
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	lastSnapshot := scanSnapshot(inputDir)
	scheduleReload := func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.NewTimer(w.debounce)
		debounceC = debounceTimer.C
	}
	for {
		select {
		case <-w.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		case event := <-watcherEvents(fileWatcher):
			if event.Name != "" && isInputChange(event.Name) && event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				scheduleReload()
			}
		case <-watcherErrors(fileWatcher):
		case <-ticker.C:
			current := scanSnapshot(inputDir)
			if !sameSnapshot(lastSnapshot, current) {
				lastSnapshot = current
				scheduleReload()
			}
		case <-debounceC:
			debounceC = nil
			if debounceTimer != nil {
				debounceTimer.Stop()
				debounceTimer = nil
			}
			_ = w.reload()
			lastSnapshot = scanSnapshot(inputDir)
		}
	}
}

func watcherEvents(watcher *fsnotify.Watcher) <-chan fsnotify.Event {
	if watcher == nil {
		return nil
	}
	return watcher.Events
}

func watcherErrors(watcher *fsnotify.Watcher) <-chan error {
	if watcher == nil {
		return nil
	}
	return watcher.Errors
}

func scanSnapshot(inputDir string) map[string]time.Time {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return map[string]time.Time{}
	}
	snapshot := map[string]time.Time{}
	for _, entry := range entries {
		if entry.IsDir() || !isInputChange(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		snapshot[entry.Name()] = info.ModTime()
	}
	return snapshot
}

func sameSnapshot(left map[string]time.Time, right map[string]time.Time) bool {
	if len(left) != len(right) {
		return false
	}
	for name, leftTime := range left {
		rightTime, ok := right[name]
		if !ok || !rightTime.Equal(leftTime) {
			return false
		}
	}
	return true
}

func isInputChange(path string) bool {
	name := filepath.Base(path)
	if name == "" || name[0] == '.' {
		return false
	}
	switch filepath.Ext(name) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
