package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
)

const xrayServicePrefix = "proxystack-xray@"

// UserChange 描述一次用户启停预期造成的 clients 变化：stack -> inbound tag -> email 集合。
type UserChange struct {
	Removed map[string]map[string]bool
	Added   map[string]map[string]bool
}

// NewUserChange 构造空的预期变化。
func NewUserChange() UserChange {
	return UserChange{
		Removed: map[string]map[string]bool{},
		Added:   map[string]map[string]bool{},
	}
}

// Add 记录一条预期的用户增删。
func (c UserChange) Add(removing bool, stack string, inboundTag string, email string) {
	target := c.Added
	if removing {
		target = c.Removed
	}
	if target[stack] == nil {
		target[stack] = map[string]bool{}
	}
	target[stack][inboundTag+"\x00"+email] = true
}

// UsersOnlyPlan 校验 plan 中所有变化都恰好等于这次启停预期造成的用户增删。
//
// 两层判断缺一不可：配置里 clients 以外的部分必须完全一致（端口、streamSettings、
// routing 之类 HandlerService 改不了，必须重启）；clients 的增删也必须和本次要热
// 应用的用户逐一对上，否则说明还有别人改过、没重启过的用户变更混在里面——那些
// 变更会被一起写盘并刷新 manifest，但热应用不会带上它们，运行中的实例就此和磁盘
// 分叉，而漂移检测再也看不出来。
//
// 返回第一个不满足条件的文件相对路径，便于提示改用 restart。
func UsersOnlyPlan(plan Plan, change UserChange) (bool, string, error) {
	for _, item := range plan.Changes {
		switch item.Action {
		case ActionNoChange, ActionCreate:
			// 文件还不存在时没有正在运行的旧配置可言，写入即可。
			continue
		case ActionDelete:
			return false, item.RelativePath, nil
		}
		stack := xrayStackFromService(item.Service)
		if stack == "" {
			// 只有 Xray 有 HandlerService；clash 之类的生成物变了就得走重启。
			return false, item.RelativePath, nil
		}
		current, err := os.ReadFile(item.Path)
		if err != nil {
			return false, item.RelativePath, fmt.Errorf("generated file could not be read: %s (%w)", item.Path, err)
		}
		usersOnly, err := usersOnlyChange(current, item.Content, stack, change)
		if err != nil {
			return false, item.RelativePath, err
		}
		if !usersOnly {
			return false, item.RelativePath, nil
		}
	}
	return true, "", nil
}

// usersOnlyChange 判断单份配置的差异是否恰好等于该 stack 上预期的用户增删。
func usersOnlyChange(oldContent []byte, newContent []byte, stack string, change UserChange) (bool, error) {
	oldValue, oldClients, err := decodeSplittingClients(oldContent)
	if err != nil {
		return false, err
	}
	newValue, newClients, err := decodeSplittingClients(newContent)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(oldValue, newValue) {
		return false, nil
	}
	removed := diffClients(oldClients, newClients)
	added := diffClients(newClients, oldClients)
	return sameKeySet(removed, change.Removed[stack]) && sameKeySet(added, change.Added[stack]), nil
}

// decodeSplittingClients 解析 Xray 配置，剥掉每个 inbound 的 clients 并单独返回
// "tag\x00email" 集合，让配置骨架和用户列表可以分别比对。
func decodeSplittingClients(content []byte) (any, map[string]bool, error) {
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, nil, fmt.Errorf("xray config contains invalid JSON: %w", err)
	}
	clients := map[string]bool{}
	root, ok := value.(map[string]any)
	if !ok {
		return value, clients, nil
	}
	inbounds, ok := root["inbounds"].([]any)
	if !ok {
		return value, clients, nil
	}
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			continue
		}
		settings, ok := inbound["settings"].(map[string]any)
		if !ok {
			continue
		}
		list, ok := settings["clients"].([]any)
		delete(settings, "clients")
		if !ok {
			continue
		}
		tag, _ := inbound["tag"].(string)
		for _, entry := range list {
			client, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			email, _ := client["email"].(string)
			clients[tag+"\x00"+email] = true
		}
	}
	return value, clients, nil
}

// diffClients 返回只出现在 left 里的条目。
func diffClients(left map[string]bool, right map[string]bool) map[string]bool {
	only := map[string]bool{}
	for key := range left {
		if !right[key] {
			only[key] = true
		}
	}
	return only
}

// sameKeySet 判断两个集合的键完全一致，nil 与空集合等价。
func sameKeySet(left map[string]bool, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if !right[key] {
			return false
		}
	}
	return true
}

// xrayStackFromService 从服务名解析 Xray stack 名；非 Xray 服务返回空串。
func xrayStackFromService(service string) string {
	if !strings.HasPrefix(service, xrayServicePrefix) || !strings.HasSuffix(service, ".service") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(service, xrayServicePrefix), ".service")
}

// DescribeUserChange 按稳定顺序描述预期变化，用于错误信息和测试断言。
func DescribeUserChange(entries map[string]bool) []string {
	described := make([]string, 0, len(entries))
	for key := range entries {
		described = append(described, strings.Replace(key, "\x00", "/", 1))
	}
	sort.Strings(described)
	return described
}
