package mihomo

import (
	"bytes"
	"fmt"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"gopkg.in/yaml.v3"
)

var defaultRuleProfile = []string{
	"DOMAIN-SUFFIX,local,DIRECT",
	"DOMAIN,localhost,DIRECT",
	"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
	"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
	"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
}

// GeneratorError 表示 mihomo 配置生成失败。
type GeneratorError struct {
	Message string
}

// Error 返回生成失败原因。
func (e GeneratorError) Error() string {
	return e.Message
}

// DumpsConfig 将指定 stack 的 mihomo 配置编码为稳定 YAML 文本。
func DumpsConfig(stackSet domain.StackSet, stackName string) (string, error) {
	node, err := RenderConfigNode(stackSet, stackName)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// RenderConfigNode 生成指定启用 stack 的 mihomo YAML 节点。
func RenderConfigNode(stackSet domain.StackSet, stackName string) (*yaml.Node, error) {
	stack, err := enabledClashStack(stackSet, stackName)
	if err != nil {
		return nil, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return nil, err
	}
	socksListener, err := singleSocksListener(stack)
	if err != nil {
		return nil, err
	}
	httpListener, hasHTTPListener, err := optionalHTTPListener(stack)
	if err != nil {
		return nil, err
	}
	proxies, err := renderProxies(stackSet, referenceGraph, stack.Clash.Upstreams)
	if err != nil {
		return nil, err
	}
	return mapping(
		pair("mode", scalar(stack.Clash.Mode)),
		pair("log-level", scalar(domain.ResolveClashLogLevel(stackSet.Config.Defaults.Clash, stack.Clash))),
		pair("ipv6", boolScalar(true)),
		pair("allow-lan", boolScalar(listenersAllowLAN(socksListener, httpListener, hasHTTPListener))),
		pair("listeners", renderListeners(socksListener, httpListener, hasHTTPListener)),
		pair("external-controller", scalar(stack.Clash.Controller.Listen)),
		pair("secret", scalar(stack.Clash.Controller.Secret)),
		pair("proxies", sequence(proxies...)),
		pair("proxy-groups", renderGroups(stack.Clash.Groups)),
		pair("rules", renderRules(stack.Clash.Rules)),
	), nil
}

func enabledClashStack(stackSet domain.StackSet, stackName string) (domain.Stack, error) {
	stack, ok := stackSet.ByName()[stackName]
	if !ok {
		return domain.Stack{}, GeneratorError{Message: "stack does not exist: " + stackName}
	}
	if !stack.Enabled {
		return domain.Stack{}, GeneratorError{Message: "stack is disabled: " + stackName}
	}
	if !stack.Clash.Enabled {
		return domain.Stack{}, GeneratorError{Message: "clash is disabled: " + stackName}
	}
	return *stack, nil
}

func singleSocksListener(stack domain.Stack) (domain.SocksListener, error) {
	if len(stack.Clash.Listeners.Socks) != 1 {
		return domain.SocksListener{}, GeneratorError{Message: "exactly one clash socks listener is required: " + stack.Name}
	}
	return stack.Clash.Listeners.Socks[0], nil
}

func optionalHTTPListener(stack domain.Stack) (domain.HTTPListener, bool, error) {
	if len(stack.Clash.Listeners.HTTP) > 1 {
		return domain.HTTPListener{}, false, GeneratorError{Message: "at most one clash http listener is supported: " + stack.Name}
	}
	if len(stack.Clash.Listeners.HTTP) == 0 {
		return domain.HTTPListener{}, false, nil
	}
	return stack.Clash.Listeners.HTTP[0], true, nil
}

func listenersAllowLAN(socksListener domain.SocksListener, httpListener domain.HTTPListener, hasHTTPListener bool) bool {
	if !domain.IsLoopbackHost(socksListener.Listen) {
		return true
	}
	return hasHTTPListener && !domain.IsLoopbackHost(httpListener.Listen)
}

func renderListeners(socksListener domain.SocksListener, httpListener domain.HTTPListener, hasHTTPListener bool) *yaml.Node {
	usedNames := map[string]bool{}
	nodes := []*yaml.Node{renderSocksListener(socksListener, usedNames)}
	if hasHTTPListener {
		nodes = append(nodes, renderHTTPListener(httpListener, usedNames))
	}
	return sequence(nodes...)
}

func renderSocksListener(listener domain.SocksListener, usedNames map[string]bool) *yaml.Node {
	return renderListener("socks", listener.Name, listener.Listen, listener.Port, listener.Users, usedNames)
}

func renderHTTPListener(listener domain.HTTPListener, usedNames map[string]bool) *yaml.Node {
	return renderListener("http", listener.Name, listener.Listen, listener.Port, listener.Users, usedNames)
}

func renderListener(listenerType string, name string, listen string, port int, users []domain.ClashListenerUser, usedNames map[string]bool) *yaml.Node {
	pairs := []nodePair{
		pair("name", scalar(uniqueListenerName(listenerType, name, usedNames))),
		pair("type", scalar(listenerType)),
		pair("listen", scalar(listen)),
		pair("port", intScalar(port)),
	}
	if users != nil {
		userNodes := make([]*yaml.Node, 0, len(users))
		for _, user := range users {
			userNodes = append(userNodes, mapping(
				pair("username", scalar(user.Username)),
				pair("password", scalar(user.Password)),
			))
		}
		pairs = append(pairs, pair("users", sequence(userNodes...)))
	}
	return mapping(pairs...)
}

func uniqueListenerName(listenerType string, name string, usedNames map[string]bool) string {
	candidates := []string{name, name + "-" + listenerType}
	suffix := 2
	for {
		for _, candidate := range candidates {
			if !usedNames[candidate] {
				usedNames[candidate] = true
				return candidate
			}
		}
		candidates = []string{fmt.Sprintf("%s-%s-%d", name, listenerType, suffix)}
		suffix++
	}
}

func renderProxies(stackSet domain.StackSet, referenceGraph graph.ReferenceGraph, upstreams []domain.ClashUpstream) ([]*yaml.Node, error) {
	nodes := make([]*yaml.Node, 0, len(upstreams))
	for _, upstream := range upstreams {
		node, err := renderProxy(stackSet, referenceGraph, upstream)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func renderProxy(stackSet domain.StackSet, referenceGraph graph.ReferenceGraph, upstream domain.ClashUpstream) (*yaml.Node, error) {
	switch upstream.Type {
	case "raw":
		return renderRawProxy(upstream)
	case "xrelay-socks5":
		return renderXrelaySocks5Proxy(stackSet, referenceGraph, upstream)
	default:
		return nil, GeneratorError{Message: "unsupported mihomo upstream type: " + upstream.Type}
	}
}

func renderRawProxy(upstream domain.ClashUpstream) (*yaml.Node, error) {
	configNode := upstream.ConfigNode()
	if configNode == nil || configNode.Kind != yaml.MappingNode {
		return nil, GeneratorError{Message: "raw upstream config is required: " + upstream.Name}
	}
	pairs := []nodePair{pair("name", scalar(upstream.Name))}
	hasAlterID := false
	hasCipher := false
	proxyType := ""
	for index := 0; index+1 < len(configNode.Content); index += 2 {
		key := configNode.Content[index].Value
		if key == "name" {
			continue
		}
		if key == "type" {
			proxyType = configNode.Content[index+1].Value
		}
		if key == "alterId" {
			hasAlterID = true
		}
		if key == "cipher" {
			hasCipher = true
		}
		pairs = append(pairs, nodePair{key: scalar(key), value: cloneNode(configNode.Content[index+1])})
	}
	if proxyType == "vmess" {
		if !hasAlterID {
			pairs = append(pairs, pair("alterId", intScalar(0)))
		}
		if !hasCipher {
			pairs = append(pairs, pair("cipher", scalar("auto")))
		}
	}
	return mapping(pairs...), nil
}

func renderXrelaySocks5Proxy(stackSet domain.StackSet, referenceGraph graph.ReferenceGraph, upstream domain.ClashUpstream) (*yaml.Node, error) {
	endpoint, ok := referenceGraph.Index.ResolveXrelayInbound(upstream.Ref)
	if !ok {
		return nil, GeneratorError{Message: "xrelay inbound ref does not exist: " + upstream.Ref}
	}
	if endpoint.Kind != "socks5" {
		return nil, GeneratorError{Message: "xrelay-socks5 ref must target socks5 inbound: " + upstream.Ref}
	}
	targetStack, ok := stackSet.ByName()[endpoint.Stack]
	if !ok {
		return nil, GeneratorError{Message: "xrelay inbound stack does not exist: " + endpoint.Stack}
	}
	for _, inbound := range targetStack.Xrelay.Inbounds {
		if inbound.Name != endpoint.Name {
			continue
		}
		pairs := []nodePair{
			pair("name", scalar(upstream.Name)),
			pair("type", scalar("socks5")),
			pair("server", scalar(domain.NormalizeInternalEndpointAddress(inbound.Listen))),
			pair("port", intScalar(inbound.Port)),
			pair("udp", boolScalar(inbound.UDP)),
		}
		if inbound.Auth != nil && inbound.Auth.Type == "password" {
			pairs = append(pairs,
				pair("username", scalar(inbound.Auth.Username)),
				pair("password", scalar(inbound.Auth.Password)),
			)
		}
		return mapping(pairs...), nil
	}
	return nil, GeneratorError{Message: "xrelay inbound ref does not exist: " + upstream.Ref}
}

func renderGroups(groups []domain.ClashGroup) *yaml.Node {
	nodes := make([]*yaml.Node, 0, len(groups))
	for _, group := range groups {
		pairs := []nodePair{
			pair("name", scalar(group.Name)),
			pair("type", scalar(group.Type)),
			pair("proxies", stringSequence(group.Proxies)),
		}
		if group.URL != "" {
			pairs = append(pairs, pair("url", scalar(group.URL)))
		}
		if group.Interval != 0 {
			pairs = append(pairs, pair("interval", intScalar(group.Interval)))
		}
		if group.Type == "load-balance" && group.Strategy != "" {
			pairs = append(pairs, pair("strategy", scalar(group.Strategy)))
		}
		nodes = append(nodes, mapping(pairs...))
	}
	return sequence(nodes...)
}

func renderRules(rules domain.ClashRules) *yaml.Node {
	values := make([]string, 0, len(rules.Extra)+len(defaultRuleProfile)+1)
	values = append(values, rules.Extra...)
	values = append(values, defaultRuleProfile...)
	values = append(values, "MATCH,"+rules.Final)
	return stringSequence(values)
}

type nodePair struct {
	key   *yaml.Node
	value *yaml.Node
}

func pair(key string, value *yaml.Node) nodePair {
	return nodePair{key: scalar(key), value: value}
}

func mapping(pairs ...nodePair) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, pair := range pairs {
		node.Content = append(node.Content, pair.key, pair.value)
	}
	return node
}

func sequence(values ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: values}
}

func stringSequence(values []string) *yaml.Node {
	nodes := make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		nodes = append(nodes, scalar(value))
	}
	return sequence(nodes...)
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func intScalar(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
}

func boolScalar(value bool) *yaml.Node {
	if value {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func cloneNode(value *yaml.Node) *yaml.Node {
	if value == nil {
		return nil
	}
	cloned := *value
	if len(value.Content) > 0 {
		cloned.Content = make([]*yaml.Node, 0, len(value.Content))
		for _, child := range value.Content {
			cloned.Content = append(cloned.Content, cloneNode(child))
		}
	}
	return &cloned
}
