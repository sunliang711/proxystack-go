package agentconfig

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/domain/validation"
	"gopkg.in/yaml.v3"
)

const templateVmessUUIDPlaceholder = "11111111-1111-4111-8111-111111111111"

// stackTemplateFiles 保存带片段 include 指令的内置 stack 模板。
//
//go:embed templates/stack.*.yaml.tmpl
var stackTemplateFiles embed.FS

// stackDocument 保存带注释和 YAML 样式的 stack 文档。
type stackDocument struct {
	node *yaml.Node
	root *yaml.Node
}

// buildStackDocument 从内置模板或外部文件构造可写回的 stack YAML 文档。
func buildStackDocument(options AddOptions) (*stackDocument, error) {
	if err := domain.ValidateIdentifier(options.Name, "stack name"); err != nil {
		return nil, err
	}
	if options.FromFile != "" {
		return loadStackDocumentFromFile(options.FromFile, options.Name)
	}
	templateName := firstNonEmpty(options.Template, "pair")
	if !isBuiltInTemplate(templateName) {
		return nil, fmt.Errorf("unknown stack template: %s", templateName)
	}
	document, err := loadStackTemplateDocument(templateName, options.Name)
	if err != nil {
		return nil, err
	}
	sourceName := scalarValue(mappingValue(document.root, "name"))
	setMappingScalar(document.root, "name", options.Name)
	rewriteSelfRefsInNode(document.root, sourceName, options.Name)
	if err := replaceTemplateVmessUUIDs(document.root); err != nil {
		return nil, err
	}
	if len(options.Members) > 0 {
		if err := applyAutoMembersToDocument(document.root, options.Members); err != nil {
			return nil, err
		}
	} else if isAutoTemplate(templateName) {
		setMappingBool(document.root, "enabled", false)
	}
	return document, nil
}

// loadStackDocumentFromFile 读取外部 stack YAML，并按 Python 版规则拒绝隐式改名。
func loadStackDocumentFromFile(path string, expectedName string) (*stackDocument, error) {
	document, err := loadStackDocument(path, "Stack file")
	if err != nil {
		return nil, err
	}
	sourceName := scalarValue(mappingValue(document.root, "name"))
	if strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) != sourceName {
		return nil, fmt.Errorf("from-file name must match its file name")
	}
	if sourceName != expectedName {
		return nil, fmt.Errorf("from-file stack name must match add target")
	}
	return document, nil
}

// loadStackTemplateDocument 读取包内 stack 模板文档。
func loadStackTemplateDocument(templateName string, stackName string) (*stackDocument, error) {
	data, err := stackTemplateFiles.ReadFile("templates/stack." + templateName + ".yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("stack template could not be read: %s (%w)", templateName, err)
	}
	context := stackTemplateSnippetContext(templateName)
	if stackName != "" {
		context.StackName = stackName
	}
	data, err = renderStackTemplate(templateName, data, context)
	if err != nil {
		return nil, err
	}
	return decodeStackDocument(data, "Stack template", "stack."+templateName+".yaml.tmpl")
}

// loadStackDocument 读取已有 stack 文件为 YAML 文档节点。
func loadStackDocument(path string, label string) (*stackDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %s (%w)", label, path, err)
	}
	return decodeStackDocument(data, label, path)
}

// decodeStackDocument 解码并确保 stack 文档顶层是 mapping。
func decodeStackDocument(data []byte, label string, path string) (*stackDocument, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("%s contains invalid YAML: %s\n%w", label, path, err)
	}
	if len(node.Content) == 0 {
		node.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping: %s", label, path)
	}
	return &stackDocument{node: &node, root: root}, nil
}

// stackFromDocument 将 YAML 文档解析为强类型 stack 并执行文件内校验。
func stackFromDocument(document *stackDocument, expectedName string, sourcePath string) (domain.Stack, error) {
	var stack domain.Stack
	if err := document.root.Decode(&stack); err != nil {
		return domain.Stack{}, err
	}
	if err := stack.Validate(); err != nil {
		return domain.Stack{}, err
	}
	if stack.Name != expectedName {
		return domain.Stack{}, fmt.Errorf("stack name must be %s", expectedName)
	}
	if sourcePath != "" {
		fileName := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
		if fileName != stack.Name {
			return domain.Stack{}, fmt.Errorf("stack name must match file name: %s", fileName)
		}
		stack.SourcePath = sourcePath
	}
	return stack, nil
}

// stackCandidateFromDocument 校验候选 stack，并编码成可落盘的 YAML 内容。
func stackCandidateFromDocument(cfg domain.GlobalConfig, stackSet domain.StackSet, name string, document *stackDocument, mode os.FileMode) (StackCandidate, error) {
	stackPath := filepath.Join(cfg.StacksDir(), name+".yaml")
	stack, err := stackFromDocument(document, name, stackPath)
	if err != nil {
		return StackCandidate{}, err
	}
	nextStacks := append(append([]domain.Stack(nil), stackSet.Stacks...), stack)
	nextStackSet, err := domain.ResolveStackSetUserRefs(domain.StackSet{Config: cfg, Stacks: nextStacks})
	if err != nil {
		return StackCandidate{}, err
	}
	if err := validation.ValidateStackSet(nextStackSet, validation.WithPortChecker(validation.NoopPortChecker{})); err != nil {
		return StackCandidate{}, err
	}
	data, err := stackDocumentData(document)
	if err != nil {
		return StackCandidate{}, err
	}
	return StackCandidate{Name: name, Path: stackPath, Data: data, Mode: mode}, nil
}

// writeExistingStackDocument 校验候选 stack 后覆盖原 stack 文件。
func writeExistingStackDocument(cfg domain.GlobalConfig, stackSet domain.StackSet, name string, sourcePath string, document *stackDocument) error {
	stack, err := stackFromDocument(document, name, sourcePath)
	if err != nil {
		return err
	}
	nextStacks := replaceStack(stackSet.Stacks, stack)
	nextStackSet, err := domain.ResolveStackSetUserRefs(domain.StackSet{Config: cfg, Stacks: nextStacks})
	if err != nil {
		return err
	}
	if err := validation.ValidateStackSet(nextStackSet, validation.WithPortChecker(validation.NoopPortChecker{})); err != nil {
		return err
	}
	return writeStackDocument(sourcePath, document)
}

// writeStackDocument 以稳定缩进写回 YAML 文档，并保留已有注释。
func writeStackDocument(path string, document *stackDocument) error {
	data, err := stackDocumentData(document)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o640)
}

// stackDocumentData 把 YAML 文档编码为稳定缩进的字节内容。
func stackDocumentData(document *stackDocument) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(document.node); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// applyAutoMembersToDocument 按 auto 模板规则替换成员 upstream 和代理组引用。
func applyAutoMembersToDocument(root *yaml.Node, members []string) error {
	for _, member := range members {
		if err := domain.ValidateIdentifier(member, "member stack name"); err != nil {
			return err
		}
	}
	clash := ensureMappingValue(root, "clash")
	upstreamNames := make([]string, 0, len(members))
	upstreams := &yaml.Node{Kind: yaml.SequenceNode}
	for _, member := range members {
		upstreamName := member + "-local"
		upstreamNames = append(upstreamNames, upstreamName)
		upstreams.Content = append(upstreams.Content, mappingNode(
			"name", scalarNode(upstreamName),
			"type", scalarNode("xray-socks5"),
			"ref", scalarNode(member+".relay"),
		))
	}
	setMappingValue(clash, "upstreams", upstreams)
	groups := mappingValue(clash, "groups")
	if groups == nil || groups.Kind != yaml.SequenceNode {
		return fmt.Errorf("clash.groups must be a list")
	}
	autoGroupName := ""
	for _, group := range groups.Content {
		if group.Kind != yaml.MappingNode {
			return fmt.Errorf("clash.groups items must be mappings")
		}
		groupType := scalarValue(mappingValue(group, "type"))
		if groupType == "url-test" || groupType == "load-balance" {
			setMappingValue(group, "proxies", flowSequenceNode(upstreamNames))
			autoGroupName = scalarValue(mappingValue(group, "name"))
		}
	}
	if autoGroupName == "" {
		return fmt.Errorf("auto members require a url-test or load-balance group")
	}
	selectProxies := append([]string{autoGroupName}, upstreamNames...)
	selectProxies = append(selectProxies, "DIRECT")
	for _, group := range groups.Content {
		if scalarValue(mappingValue(group, "type")) == "select" {
			setMappingValue(group, "proxies", flowSequenceNode(selectProxies))
		}
	}
	return nil
}

// addMemberToDocument 向 auto stack 文档追加一个成员。
func addMemberToDocument(root *yaml.Node, memberName string) error {
	if err := domain.ValidateIdentifier(memberName, "member stack name"); err != nil {
		return err
	}
	clash, err := autoClashMapping(root)
	if err != nil {
		return err
	}
	upstreams := ensureSequenceValue(clash, "upstreams")
	upstreamName := memberName + "-local"
	ref := memberName + ".relay"
	for _, upstream := range upstreams.Content {
		if upstream.Kind != yaml.MappingNode {
			return fmt.Errorf("clash.upstreams items must be mappings")
		}
		if scalarValue(mappingValue(upstream, "name")) == upstreamName {
			return fmt.Errorf("upstream already exists: %s", upstreamName)
		}
		if scalarValue(mappingValue(upstream, "type")) == "xray-socks5" && scalarValue(mappingValue(upstream, "ref")) == ref {
			return fmt.Errorf("member already exists: %s", memberName)
		}
	}
	upstreams.Content = append(upstreams.Content, mappingNode(
		"name", scalarNode(upstreamName),
		"type", scalarNode("xray-socks5"),
		"ref", scalarNode(ref),
	))
	return syncMemberProxyAdd(clash, upstreamName)
}

// removeMemberFromDocument 从 auto stack 文档移除一个成员。
func removeMemberFromDocument(root *yaml.Node, memberName string) error {
	if err := domain.ValidateIdentifier(memberName, "member stack name"); err != nil {
		return err
	}
	clash, err := autoClashMapping(root)
	if err != nil {
		return err
	}
	upstreams := ensureSequenceValue(clash, "upstreams")
	upstreamName := memberName + "-local"
	ref := memberName + ".relay"
	removed := map[string]bool{}
	next := make([]*yaml.Node, 0, len(upstreams.Content))
	for _, upstream := range upstreams.Content {
		if upstream.Kind == yaml.MappingNode &&
			scalarValue(mappingValue(upstream, "type")) == "xray-socks5" &&
			(scalarValue(mappingValue(upstream, "name")) == upstreamName || scalarValue(mappingValue(upstream, "ref")) == ref) {
			removed[scalarValue(mappingValue(upstream, "name"))] = true
			continue
		}
		next = append(next, upstream)
	}
	if len(removed) == 0 {
		return fmt.Errorf("member does not exist: %s", memberName)
	}
	upstreams.Content = next
	return syncMemberProxyRemove(clash, removed)
}

// autoClashMapping 校验当前 stack 支持成员命令并返回 clash mapping。
func autoClashMapping(root *yaml.Node) (*yaml.Node, error) {
	if scalarValue(mappingValue(root, "role")) != "auto" {
		return nil, fmt.Errorf("stack does not support member commands: %s", scalarValue(mappingValue(root, "name")))
	}
	clash := mappingValue(root, "clash")
	if clash == nil || clash.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("clash must be a mapping")
	}
	groups := mappingValue(clash, "groups")
	if groups == nil || groups.Kind != yaml.SequenceNode || len(autoProxyGroupNames(groups)) == 0 {
		return nil, fmt.Errorf("stack does not support member commands: %s", scalarValue(mappingValue(root, "name")))
	}
	return clash, nil
}

// syncMemberProxyAdd 把新增成员加入自动组，并插入 select 组的 DIRECT 前。
func syncMemberProxyAdd(clash *yaml.Node, upstreamName string) error {
	groups := ensureSequenceValue(clash, "groups")
	autoGroups := autoProxyGroupNames(groups)
	if len(autoGroups) == 0 {
		return fmt.Errorf("member commands require a url-test or load-balance group")
	}
	for _, group := range groups.Content {
		if group.Kind != yaml.MappingNode {
			return fmt.Errorf("clash.groups items must be mappings")
		}
		proxies := ensureSequenceValue(group, "proxies")
		groupType := scalarValue(mappingValue(group, "type"))
		if groupType == "url-test" || groupType == "load-balance" {
			appendProxyName(proxies, upstreamName)
			continue
		}
		if groupType == "select" && shouldSyncSelectGroup(proxies, autoGroups) {
			insertProxyNameBeforeDirect(proxies, upstreamName)
		}
	}
	return nil
}

// syncMemberProxyRemove 从所有代理组中删除成员名称。
func syncMemberProxyRemove(clash *yaml.Node, upstreamNames map[string]bool) error {
	groups := ensureSequenceValue(clash, "groups")
	for _, group := range groups.Content {
		if group.Kind != yaml.MappingNode {
			return fmt.Errorf("clash.groups items must be mappings")
		}
		proxies := ensureSequenceValue(group, "proxies")
		next := make([]*yaml.Node, 0, len(proxies.Content))
		for _, proxy := range proxies.Content {
			if !upstreamNames[scalarValue(proxy)] {
				next = append(next, proxy)
			}
		}
		proxies.Content = next
	}
	return nil
}

// allocateStackDocumentPorts 按全局端口池修改 stack 文档中的监听端口。
func allocateStackDocumentPorts(document *stackDocument, stackSet domain.StackSet) error {
	used := usedPorts(stackSet)
	xray := ensureMappingValue(document.root, "xray")
	inbounds := ensureSequenceValue(xray, "inbounds")
	xrayPorts, err := stackSet.Config.PortRanges.XrayInbound.Allocate(used, len(inbounds.Content))
	if err != nil {
		return err
	}
	for index, inbound := range inbounds.Content {
		setMappingInt(inbound, "port", xrayPorts[index])
		used[xrayPorts[index]] = true
	}
	api := mappingValue(xray, "api")
	if api != nil && api.Kind == yaml.MappingNode && boolValue(mappingValue(api, "enabled")) {
		host := "127.0.0.1"
		if listen := scalarValue(mappingValue(api, "listen")); listen != "" {
			if parsedHost, _, err := domain.ParseListen(listen); err == nil {
				host = parsedHost
			}
		}
		ports, err := stackSet.Config.PortRanges.XrayAPIRange.Allocate(used, 1)
		if err != nil {
			return err
		}
		setMappingScalar(api, "listen", fmt.Sprintf("%s:%d", host, ports[0]))
		used[ports[0]] = true
	}
	clash := ensureMappingValue(document.root, "clash")
	listeners := ensureMappingValue(clash, "listeners")
	if socks := mappingValue(listeners, "socks"); socks != nil && socks.Kind == yaml.SequenceNode {
		if err := allocateListenerPorts(socks, stackSet.Config.PortRanges.ClashSocks, used); err != nil {
			return err
		}
	}
	if http := mappingValue(listeners, "http"); http != nil && http.Kind == yaml.SequenceNode {
		if err := allocateListenerPorts(http, stackSet.Config.PortRanges.ClashHTTP, used); err != nil {
			return err
		}
	}
	controller := ensureMappingValue(clash, "controller")
	host := "127.0.0.1"
	if listen := scalarValue(mappingValue(controller, "listen")); listen != "" {
		if parsedHost, _, err := domain.ParseListen(listen); err == nil {
			host = parsedHost
		}
	}
	controllerPorts, err := stackSet.Config.PortRanges.ClashControler.Allocate(used, 1)
	if err != nil {
		return err
	}
	setMappingScalar(controller, "listen", fmt.Sprintf("%s:%d", host, controllerPorts[0]))
	used[controllerPorts[0]] = true
	return nil
}

// allocateListenerPorts 为 listener 列表分配端口。
func allocateListenerPorts(listeners *yaml.Node, portRange domain.PortRange, used map[int]bool) error {
	ports, err := portRange.Allocate(used, len(listeners.Content))
	if err != nil {
		return err
	}
	for index, listener := range listeners.Content {
		setMappingInt(listener, "port", ports[index])
		used[ports[index]] = true
	}
	return nil
}

// replaceTemplateVmessUUIDs 替换内置模板中的 vmess UUID 占位符。
func replaceTemplateVmessUUIDs(root *yaml.Node) error {
	xray := mappingValue(root, "xray")
	if xray == nil || xray.Kind != yaml.MappingNode {
		return nil
	}
	inbounds := mappingValue(xray, "inbounds")
	if inbounds == nil || inbounds.Kind != yaml.SequenceNode {
		return nil
	}
	for _, inbound := range inbounds.Content {
		if inbound.Kind != yaml.MappingNode || scalarValue(mappingValue(inbound, "protocol")) != "vmess" {
			continue
		}
		users := mappingValue(inbound, "users")
		if users == nil || users.Kind != yaml.SequenceNode {
			continue
		}
		for _, user := range users.Content {
			uuid := mappingValue(user, "uuid")
			if scalarValue(uuid) != templateVmessUUIDPlaceholder {
				continue
			}
			generatedUUID, err := randomUUID()
			if err != nil {
				return err
			}
			uuid.Value = generatedUUID
		}
	}
	return nil
}

// rewriteSelfRefsInNode 只改写指向当前 stack 自身资源的 ref 字段。
func rewriteSelfRefsInNode(node *yaml.Node, source string, target string) {
	if source == "" || source == target {
		return
	}
	rewrites := selfRefRewrites(node, source, target)
	if len(rewrites) == 0 {
		return
	}
	rewriteXrayOutboundRef(node, rewrites)
	rewriteClashUpstreamRefs(node, rewrites)
}

// selfRefRewrites 收集当前文档内可被自身 ref 指向的资源名称。
func selfRefRewrites(root *yaml.Node, source string, target string) map[string]string {
	rewrites := map[string]string{}
	xray := mappingValue(root, "xray")
	if xray != nil && xray.Kind == yaml.MappingNode {
		inbounds := mappingValue(xray, "inbounds")
		if inbounds != nil && inbounds.Kind == yaml.SequenceNode {
			for _, inbound := range inbounds.Content {
				if inbound.Kind != yaml.MappingNode {
					continue
				}
				name := scalarValue(mappingValue(inbound, "name"))
				if name != "" {
					rewrites[source+"."+name] = target + "." + name
				}
			}
		}
	}
	clash := mappingValue(root, "clash")
	if clash != nil && clash.Kind == yaml.MappingNode {
		listeners := mappingValue(clash, "listeners")
		if listeners != nil && listeners.Kind == yaml.MappingNode {
			if socks := mappingValue(listeners, "socks"); socks != nil && socks.Kind == yaml.SequenceNode && len(socks.Content) > 0 {
				rewrites[source+".clash.socks"] = target + ".clash.socks"
			}
			if http := mappingValue(listeners, "http"); http != nil && http.Kind == yaml.SequenceNode && len(http.Content) > 0 {
				rewrites[source+".clash.http"] = target + ".clash.http"
			}
		}
	}
	return rewrites
}

// rewriteXrayOutboundRef 只改写 xray.outbound.ref 中指向本 stack 的 clash 监听引用。
func rewriteXrayOutboundRef(root *yaml.Node, rewrites map[string]string) {
	xray := mappingValue(root, "xray")
	if xray == nil || xray.Kind != yaml.MappingNode {
		return
	}
	outbound := mappingValue(xray, "outbound")
	if outbound == nil || outbound.Kind != yaml.MappingNode {
		return
	}
	if scalarValue(mappingValue(outbound, "type")) != "clash" {
		return
	}
	ref := mappingValue(outbound, "ref")
	if ref == nil || ref.Kind != yaml.ScalarNode {
		return
	}
	if replacement, ok := rewrites[ref.Value]; ok {
		ref.Value = replacement
	}
}

// rewriteClashUpstreamRefs 只改写 xray-socks5 upstream 的 schema ref 字段。
func rewriteClashUpstreamRefs(root *yaml.Node, rewrites map[string]string) {
	clash := mappingValue(root, "clash")
	if clash == nil || clash.Kind != yaml.MappingNode {
		return
	}
	upstreams := mappingValue(clash, "upstreams")
	if upstreams == nil || upstreams.Kind != yaml.SequenceNode {
		return
	}
	for _, upstream := range upstreams.Content {
		if upstream.Kind != yaml.MappingNode || scalarValue(mappingValue(upstream, "type")) != "xray-socks5" {
			continue
		}
		ref := mappingValue(upstream, "ref")
		if ref == nil || ref.Kind != yaml.ScalarNode {
			continue
		}
		if replacement, ok := rewrites[ref.Value]; ok {
			ref.Value = replacement
		}
	}
}

// rewriteSubscriptionRemarksInNode 只改写订阅 remark 中以前 stack 名开头的值。
func rewriteSubscriptionRemarksInNode(root *yaml.Node, source string, target string) {
	if source == "" || source == target {
		return
	}
	xray := mappingValue(root, "xray")
	if xray == nil || xray.Kind != yaml.MappingNode {
		return
	}
	inbounds := mappingValue(xray, "inbounds")
	if inbounds == nil || inbounds.Kind != yaml.SequenceNode {
		return
	}
	for _, inbound := range inbounds.Content {
		if inbound.Kind != yaml.MappingNode {
			continue
		}
		replaceRemarkPrefix(inbound, source, target)
		rewriteRemarkList(mappingValue(inbound, "users"), source, target)
		rewriteRemarkList(mappingValue(inbound, "user_refs"), source, target)
	}
}

// rewriteRemarkList 改写 users/user_refs 列表项中的 remark 前缀。
func rewriteRemarkList(list *yaml.Node, source string, target string) {
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	for _, item := range list.Content {
		if item.Kind == yaml.MappingNode {
			replaceRemarkPrefix(item, source, target)
		}
	}
}

// replaceRemarkPrefix 将 remark 中以原 stack 名开头的值替换为目标 stack 名。
func replaceRemarkPrefix(mapping *yaml.Node, source string, target string) {
	remark := mappingValue(mapping, "remark")
	if remark == nil || remark.Kind != yaml.ScalarNode {
		return
	}
	if remark.Value == source {
		remark.Value = target
		return
	}
	prefix := source + " "
	if strings.HasPrefix(remark.Value, prefix) {
		remark.Value = target + strings.TrimPrefix(remark.Value, source)
	}
}

// autoProxyGroupNames 返回 url-test/load-balance 组名称。
func autoProxyGroupNames(groups *yaml.Node) map[string]bool {
	names := map[string]bool{}
	for _, group := range groups.Content {
		if group.Kind != yaml.MappingNode {
			continue
		}
		groupType := scalarValue(mappingValue(group, "type"))
		if groupType != "url-test" && groupType != "load-balance" {
			continue
		}
		name := scalarValue(mappingValue(group, "name"))
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// shouldSyncSelectGroup 判断 select 组是否包含自动组。
func shouldSyncSelectGroup(proxies *yaml.Node, autoGroups map[string]bool) bool {
	for _, proxy := range proxies.Content {
		if autoGroups[scalarValue(proxy)] {
			return true
		}
	}
	return false
}

// appendProxyName 追加代理名，已存在则保持不变。
func appendProxyName(proxies *yaml.Node, upstreamName string) {
	for _, proxy := range proxies.Content {
		if scalarValue(proxy) == upstreamName {
			return
		}
	}
	proxies.Content = append(proxies.Content, scalarNode(upstreamName))
}

// insertProxyNameBeforeDirect 将代理名插入 DIRECT 前。
func insertProxyNameBeforeDirect(proxies *yaml.Node, upstreamName string) {
	for _, proxy := range proxies.Content {
		if scalarValue(proxy) == upstreamName {
			return
		}
	}
	for index, proxy := range proxies.Content {
		if scalarValue(proxy) == "DIRECT" {
			proxies.Content = append(proxies.Content[:index], append([]*yaml.Node{scalarNode(upstreamName)}, proxies.Content[index:]...)...)
			return
		}
	}
	proxies.Content = append(proxies.Content, scalarNode(upstreamName))
}

// membersFromDocument 返回 auto stack 中的成员名。
func membersFromDocument(root *yaml.Node) []string {
	clash := mappingValue(root, "clash")
	if clash == nil || clash.Kind != yaml.MappingNode {
		return nil
	}
	upstreams := mappingValue(clash, "upstreams")
	if upstreams == nil || upstreams.Kind != yaml.SequenceNode {
		return nil
	}
	members := make([]string, 0)
	for _, upstream := range upstreams.Content {
		if upstream.Kind != yaml.MappingNode || scalarValue(mappingValue(upstream, "type")) != "xray-socks5" {
			continue
		}
		ref := scalarValue(mappingValue(upstream, "ref"))
		parts := strings.Split(ref, ".")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			members = append(members, parts[0])
		} else {
			members = append(members, "-")
		}
	}
	sort.Strings(members)
	return members
}

// isBuiltInTemplate 判断模板名是否为内置 stack 模板。
func isBuiltInTemplate(templateName string) bool {
	return templateName == "pair" || templateName == "auto-url-test" || templateName == "load-balance"
}

// isAutoTemplate 判断模板是否为 auto 成员模板。
func isAutoTemplate(templateName string) bool {
	return templateName == "auto-url-test" || templateName == "load-balance"
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func ensureMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	value := mappingValue(mapping, key)
	if value != nil && value.Kind == yaml.MappingNode {
		return value
	}
	value = &yaml.Node{Kind: yaml.MappingNode}
	setMappingValue(mapping, key, value)
	return value
}

func ensureSequenceValue(mapping *yaml.Node, key string) *yaml.Node {
	value := mappingValue(mapping, key)
	if value != nil && value.Kind == yaml.SequenceNode {
		return value
	}
	value = &yaml.Node{Kind: yaml.SequenceNode}
	setMappingValue(mapping, key, value)
	return value
}

func setMappingScalar(mapping *yaml.Node, key string, value string) {
	setMappingValue(mapping, key, scalarNode(value))
}

func setMappingBool(mapping *yaml.Node, key string, value bool) {
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool"}
	if value {
		scalar.Value = "true"
	} else {
		scalar.Value = "false"
	}
	setMappingValue(mapping, key, scalar)
}

func setMappingInt(mapping *yaml.Node, key string, value int) {
	setMappingValue(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)})
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func mappingNode(pairs ...any) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for index := 0; index+1 < len(pairs); index += 2 {
		key, _ := pairs[index].(string)
		value, _ := pairs[index+1].(*yaml.Node)
		node.Content = append(node.Content, scalarNode(key), value)
	}
	return node
}

func flowSequenceNode(values []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, value := range values {
		node.Content = append(node.Content, scalarNode(value))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func scalarValue(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

func boolValue(node *yaml.Node) bool {
	return node != nil && node.Value == "true"
}

// loadAutoStackDocument 加载 auto stack 的原始 YAML 文档。
func loadAutoStackDocument(configPath string, stackName string) (domain.GlobalConfig, domain.StackSet, *stackDocument, domain.Stack, error) {
	cfg, stackSet, err := loadConfigAndStacks(configPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	stackPath := filepath.Join(cfg.StacksDir(), stackName+".yaml")
	document, err := loadStackDocument(stackPath, "Stack file")
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	stack, err := stackFromDocument(document, stackName, stackPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	if _, err := autoClashMapping(document.root); err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	return cfg, stackSet, document, stack, nil
}

// loadConfigAndStackDocument 加载 config、stack set 和指定 stack 文档。
func loadConfigAndStackDocument(configPath string, stackName string) (domain.GlobalConfig, domain.StackSet, *stackDocument, domain.Stack, error) {
	cfg, stackSet, err := loadConfigAndStacks(configPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	stackPath := filepath.Join(cfg.StacksDir(), stackName+".yaml")
	document, err := loadStackDocument(stackPath, "Stack file")
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	stack, err := stackFromDocument(document, stackName, stackPath)
	if err != nil {
		return domain.GlobalConfig{}, domain.StackSet{}, nil, domain.Stack{}, err
	}
	return cfg, stackSet, document, stack, nil
}
