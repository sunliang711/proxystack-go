package sub

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const (
	ClashTemplateName        = "clash.yaml.j2"
	PremiumClashTemplateName = "premium-clash.yaml.j2"
	SurgeTemplateName        = "surge.conf.j2"

	testURL               = "http://www.gstatic.com/generate_204"
	surgeProxyListIconURL = "https://raw.githubusercontent.com/sunliang711/icons/main/surge.webp"
	surgeAutoIconURL      = "https://raw.githubusercontent.com/sunliang711/icons/main/auto.png"
	surgeSkipProxy        = "127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local"
)

var (
	defaultClashRules = []string{
		"DOMAIN-SUFFIX,local,DIRECT",
		"DOMAIN,localhost,DIRECT",
		"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
		"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
		"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
		"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
		"GEOIP,CN,DIRECT",
		"MATCH,Final",
	}
	defaultSurgeRules = []string{
		"DOMAIN-SUFFIX,local,DIRECT",
		"DOMAIN,localhost,DIRECT",
		"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
		"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
		"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
		"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
		"GEOIP,CN,DIRECT",
		"FINAL,FinalList",
	}
	commonRegionCodes = []string{"HK", "JP", "US", "DE", "TW", "KR", "SG", "GB", "CA", "AU", "FR", "NL", "TR", "IN"}
	regionMetadata    = map[string]map[string]string{
		"HK": {"name": "🇭🇰 香港节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/Hong_Kong.png"},
		"JP": {"name": "🇯🇵 日本节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/Japan.png"},
		"US": {"name": "🇺🇸 美国节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/United_States.png"},
		"DE": {"name": "🇩🇪 德国节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/Germany.png"},
		"TW": {"name": "🇨🇳 台湾节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/China.png"},
		"KR": {"name": "🇰🇷 韩国节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/South_Korea.png"},
		"SG": {"name": "🇸🇬 新加坡节点", "icon_url": "https://raw.githubusercontent.com/Semporia/Hand-Painted-icon/master/Rounded_Rectangle/Singapore.png"},
		"GB": {"name": "🇬🇧 英国节点"},
		"CA": {"name": "🇨🇦 加拿大节点"},
		"AU": {"name": "🇦🇺 澳大利亚节点"},
		"FR": {"name": "🇫🇷 法国节点"},
		"NL": {"name": "🇳🇱 荷兰节点"},
		"TR": {"name": "🇹🇷 土耳其节点"},
		"IN": {"name": "🇮🇳 印度节点"},
	}
	regionPrefixPattern = regexp.MustCompile(`^\[([A-Z]{2})\]|^([A-Z]{2})[-_]`)
)

// ClashProxyGroup 是模板 yaml_block 使用的有序 Clash proxy-group 结构。
type ClashProxyGroup struct {
	Name     string   `yaml:"name" json:"name"`
	Type     string   `yaml:"type" json:"type"`
	URL      string   `yaml:"url,omitempty" json:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty" json:"interval,omitempty"`
	Strategy string   `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	Proxies  []string `yaml:"proxies" json:"proxies"`
}

type surgeTemplateContext struct {
	proxyLines   []string
	proxyNames   []string
	regionGroups []map[string]any
}

// RenderClashSubscription 渲染普通 Clash 订阅。
func RenderClashSubscription(index Index, user string, templateDir string, dataDir string) (string, error) {
	context, err := BuildTemplateContext(index, user)
	if err != nil {
		return "", err
	}
	return RenderTemplate(ClashTemplateName, context, templateDir, dataDir)
}

// RenderPremiumClashSubscription 渲染 Premium Clash 订阅。
func RenderPremiumClashSubscription(index Index, user string, templateDir string, dataDir string) (string, error) {
	context, err := BuildTemplateContext(index, user)
	if err != nil {
		return "", err
	}
	return RenderTemplate(PremiumClashTemplateName, context, templateDir, dataDir)
}

// RenderSurgeSubscription 渲染 Surge 订阅。
func RenderSurgeSubscription(index Index, user string, templateDir string, dataDir string, managedURL string, managedInterval int, managedStrict bool) (string, error) {
	context, err := BuildTemplateContext(index, user)
	if err != nil {
		return "", err
	}
	surgeContext := buildSurgeTemplateContext(NodesForUser(index, user))
	context["surge_proxy_lines"] = surgeContext.proxyLines
	context["surge_proxy_names"] = surgeContext.proxyNames
	context["surge_region_groups"] = surgeContext.regionGroups
	context["managed_config_url"] = managedURL
	context["managed_config_interval"] = managedInterval
	if managedStrict {
		context["managed_config_strict"] = "true"
	} else {
		context["managed_config_strict"] = "false"
	}
	return RenderTemplate(SurgeTemplateName, context, templateDir, dataDir)
}

// BuildTemplateContext 生成三类订阅模板共享上下文。
func BuildTemplateContext(index Index, user string) (map[string]any, error) {
	nodes := NodesForUser(index, user)
	if len(nodes) == 0 {
		return nil, GeneratorError{Message: "subscription user has no nodes: " + user}
	}
	proxies := make([]*yaml.Node, 0, len(nodes))
	proxyNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		proxy := RenderClashProxy(node)
		proxies = append(proxies, proxy)
		proxyNames = append(proxyNames, node.Remark)
	}
	return map[string]any{
		"user":                     user,
		"generated_at":             index.GeneratedAt,
		"sources":                  append([]string(nil), index.Sources...),
		"nodes":                    nodes,
		"proxies":                  proxies,
		"proxy_names":              proxyNames,
		"proxy_groups":             RenderClashProxyGroups(proxyNames),
		"clash_rules":              append([]string(nil), defaultClashRules...),
		"surge_proxy_lines":        []string{},
		"surge_proxy_names":        []string{},
		"surge_region_groups":      RenderSurgeRegionGroups(nodes),
		"surge_rules":              append([]string(nil), defaultSurgeRules...),
		"test_url":                 testURL,
		"surge_skip_proxy":         surgeSkipProxy,
		"surge_proxylist_icon_url": surgeProxyListIconURL,
		"surge_auto_icon_url":      surgeAutoIconURL,
		"managed_config_url":       "",
		"managed_config_interval":  86400,
		"managed_config_strict":    "true",
	}, nil
}

// NodesForUser 返回用户节点，用户不存在时返回空切片。
func NodesForUser(index Index, user string) []Node {
	nodes := index.Users[user]
	return append([]Node(nil), nodes...)
}

// RenderClashProxy 把订阅节点转换为 Clash proxy YAML 节点。
func RenderClashProxy(node Node) *yaml.Node {
	if node.Direct {
		return renderDirectClashProxy(node)
	}
	pairs := []yamlNodePair{
		yamlPair("name", yamlString(node.Remark)),
		yamlPair("type", yamlString(clashProtocolType(node.Protocol))),
		yamlPair("server", yamlString(node.Server)),
		yamlPair("port", yamlInt(node.Port)),
	}
	switch node.Protocol {
	case "vmess":
		alterID := 0
		pairs = append(pairs,
			yamlPair("uuid", yamlString(node.UUID)),
			yamlPair("alterId", yamlInt(alterID)),
			yamlPair("cipher", yamlString("auto")),
			yamlPair("network", yamlString(node.Network)),
		)
		if node.WSOpts != nil {
			pairs = append(pairs, yamlPair("ws-opts", websocketOptionsToYAML(node.WSOpts)))
		}
		if node.GRPCOpts != nil {
			pairs = append(pairs, yamlPair("grpc-opts", grpcOptionsToClashYAML(node.GRPCOpts)))
		}
	case "shadowsocks":
		pairs = append(pairs,
			yamlPair("cipher", yamlString(firstNonEmpty(node.Cipher, node.Method))),
			yamlPair("password", yamlString(node.Password)),
		)
	case "socks5", "http":
		if node.Auth != nil && node.Auth.Type == "password" {
			pairs = append(pairs,
				yamlPair("username", yamlString(node.Auth.Username)),
				yamlPair("password", yamlString(node.Auth.Password)),
			)
		}
	}
	if node.UDP != nil && supportsSubscriptionUDPProtocol(node.Protocol) {
		pairs = append(pairs, yamlPair("udp", yamlBool(*node.UDP)))
	}
	return yamlMapping(pairs...)
}

func renderDirectClashProxy(node Node) *yaml.Node {
	pairs := []yamlNodePair{
		yamlPair("name", yamlString(node.Remark)),
		yamlPair("type", yamlString(directClashType(node))),
	}
	seen := map[string]bool{"name": true, "type": true}
	hasAlterID := false
	hasCipher := false
	for _, field := range node.RawFields() {
		if field.key == nil || directClashSkipField(field.key.Value) || seen[field.key.Value] {
			continue
		}
		if field.key.Value == "alterId" {
			hasAlterID = true
		}
		if field.key.Value == "cipher" {
			hasCipher = true
		}
		pairs = append(pairs, field)
		seen[field.key.Value] = true
	}
	if node.Protocol == "vmess" {
		if !hasAlterID {
			pairs = append(pairs, yamlPair("alterId", yamlInt(0)))
		}
		if !hasCipher {
			pairs = append(pairs, yamlPair("cipher", yamlString("auto")))
		}
	}
	return yamlMapping(pairs...)
}

// RenderClashProxyGroups 生成默认 Clash proxy-groups。
func RenderClashProxyGroups(proxyNames []string) []ClashProxyGroup {
	return []ClashProxyGroup{
		{Name: "AllProxy", Type: "select", Proxies: append(append([]string{"auto", "loadbalance"}, proxyNames...), "DIRECT")},
		{Name: "loadbalance", Type: "load-balance", URL: testURL, Interval: 300, Strategy: "round-robin", Proxies: append([]string(nil), proxyNames...)},
		{Name: "auto", Type: "url-test", URL: testURL, Interval: 300, Proxies: append([]string(nil), proxyNames...)},
		{Name: "Final", Type: "select", Proxies: append(append([]string{"AllProxy", "DIRECT"}, proxyNames...), []string{}...)},
	}
}

// buildSurgeTemplateContext 生成 Surge 专用代理上下文，并过滤 Surge 不支持的节点。
func buildSurgeTemplateContext(nodes []Node) surgeTemplateContext {
	supportedNodes := filterSurgeSupportedNodes(nodes)
	return surgeTemplateContext{
		proxyLines:   renderSurgeProxyLines(supportedNodes),
		proxyNames:   proxyNamesForNodes(supportedNodes),
		regionGroups: RenderSurgeRegionGroups(supportedNodes),
	}
}

// RenderSurgeProxyLines 生成 Surge [Proxy] 行。
func RenderSurgeProxyLines(nodes []Node) []string {
	return renderSurgeProxyLines(filterSurgeSupportedNodes(nodes))
}

// renderSurgeProxyLines 渲染已确认 Surge 支持的节点列表。
func renderSurgeProxyLines(nodes []Node) []string {
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		line := RenderSurgeProxy(node)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// RenderSurgeProxy 生成单个 Surge proxy 行。
func RenderSurgeProxy(node Node) string {
	if node.Direct {
		return RenderDirectSurgeProxy(node)
	}
	switch node.Protocol {
	case "vmess":
		parts := []string{
			fmt.Sprintf("%s = vmess", node.Remark),
			node.Server,
			fmt.Sprintf("%d", node.Port),
			"username=" + node.UUID,
			"network=" + node.Network,
			"vmess-aead=true",
		}
		if nodeUsesWebSocket(node) {
			parts = append(parts, "ws=true")
		}
		if wsPath := nodeWSPath(node); wsPath != "" {
			parts = append(parts, "ws-path="+wsPath)
		}
		if wsHeaders := nodeWSHeaders(node); wsHeaders != "" {
			parts = append(parts, "ws-headers="+wsHeaders)
		}
		return strings.Join(parts, ", ")
	case "shadowsocks":
		return fmt.Sprintf("%s = ss, %s, %d, encrypt-method=%s, password=%s", node.Remark, node.Server, node.Port, firstNonEmpty(node.Cipher, node.Method), node.Password)
	case "socks5":
		udp := ""
		if node.UDP != nil && *node.UDP {
			udp = ", udp-relay=true"
		}
		return fmt.Sprintf("%s = socks5, %s, %d%s%s", node.Remark, node.Server, node.Port, surgeAuth(node), udp)
	case "http":
		return fmt.Sprintf("%s = http, %s, %d%s", node.Remark, node.Server, node.Port, surgeAuth(node))
	default:
		return ""
	}
}

// nodeUsesWebSocket 判断普通订阅节点是否使用 websocket 传输。
func nodeUsesWebSocket(node Node) bool {
	network := strings.ToLower(node.Network)
	return network == "ws" || network == "websocket" || node.WSOpts != nil
}

// nodeWSPath 返回普通订阅节点的 websocket path。
func nodeWSPath(node Node) string {
	if node.WSOpts == nil {
		return ""
	}
	return node.WSOpts.Path
}

// nodeWSHeaders 返回普通订阅节点的 websocket headers。
func nodeWSHeaders(node Node) string {
	if node.WSOpts == nil || len(node.WSOpts.Headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(node.WSOpts.Headers))
	for key := range node.WSOpts.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+":"+node.WSOpts.Headers[key])
	}
	return strings.Join(values, "|")
}

// RenderDirectSurgeProxy 把 direct 节点中常见 Clash vmess 扩展映射为 Surge 参数。
func RenderDirectSurgeProxy(node Node) string {
	if node.Protocol != "vmess" {
		normal := node
		normal.Direct = false
		return RenderSurgeProxy(normal)
	}
	parts := []string{
		fmt.Sprintf("%s = vmess", node.Remark),
		node.Server,
		fmt.Sprintf("%d", node.Port),
		"username=" + node.UUID,
	}
	network := directNodeSurgeNetwork(node)
	if network != "" {
		parts = append(parts, "network="+network)
	}
	parts = append(parts, "vmess-aead=true")
	if directNodeBool(node, "tls") {
		parts = append(parts, "tls=true")
	}
	if directNodeBool(node, "skip-cert-verify") {
		parts = append(parts, "skip-cert-verify=true")
	}
	if serverName := directNodeString(node, "servername"); serverName != "" {
		parts = append(parts, "sni="+serverName)
	}
	if directNodeUsesWebSocket(node, network) {
		parts = append(parts, "ws=true")
	}
	if wsPath := directNodeWSPath(node); wsPath != "" {
		parts = append(parts, "ws-path="+wsPath)
	}
	if wsHeaders := directNodeWSHeaders(node); wsHeaders != "" {
		parts = append(parts, "ws-headers="+wsHeaders)
	}
	return strings.Join(parts, ", ")
}

// filterSurgeSupportedNodes 返回 Surge 可确认支持的节点，并对跳过项输出 warning。
func filterSurgeSupportedNodes(nodes []Node) []Node {
	supported := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if reason := unsupportedSurgeNodeReason(node); reason != "" {
			log.Warn().
				Str("node_id", node.ID).
				Str("proxy", node.Remark).
				Str("protocol", node.Protocol).
				Str("reason", reason).
				Msg("skipping unsupported surge subscription node")
			continue
		}
		supported = append(supported, node)
	}
	return supported
}

// unsupportedSurgeNodeReason 返回节点不能安全渲染为 Surge proxy 的原因。
func unsupportedSurgeNodeReason(node Node) string {
	switch node.Protocol {
	case "vmess":
		network := node.Network
		if node.Direct {
			network = directNodeSurgeNetwork(node)
		}
		switch strings.ToLower(network) {
		case "", "raw", "tcp", "ws", "websocket":
			return ""
		default:
			return "unsupported vmess network: " + network
		}
	case "shadowsocks", "socks5", "http":
		return ""
	default:
		return "unsupported surge protocol: " + node.Protocol
	}
}

// RenderSurgeRegionGroups 生成 Surge 地区分组上下文。
func RenderSurgeRegionGroups(nodes []Node) []map[string]any {
	grouped := map[string][]string{"OTHER": {}}
	for _, region := range commonRegionCodes {
		grouped[region] = []string{}
	}
	extraRegions := map[string]bool{}
	for _, node := range nodes {
		region := resolveNodeRegion(node)
		if _, ok := grouped[region]; !ok {
			grouped[region] = []string{}
			extraRegions[region] = true
		}
		grouped[region] = append(grouped[region], node.Remark)
	}
	orderedRegions := append([]string(nil), commonRegionCodes...)
	extras := make([]string, 0, len(extraRegions))
	for region := range extraRegions {
		extras = append(extras, region)
	}
	sort.Strings(extras)
	orderedRegions = append(orderedRegions, extras...)
	orderedRegions = append(orderedRegions, "OTHER")
	result := make([]map[string]any, 0)
	for _, region := range orderedRegions {
		proxyNames := grouped[region]
		if len(proxyNames) == 0 {
			continue
		}
		result = append(result, map[string]any{
			"region":      region,
			"name":        regionGroupName(region),
			"icon_url":    regionIconURL(region),
			"proxy_names": proxyNames,
		})
	}
	return result
}

// proxyNamesForNodes 提取节点代理名，用于 Surge 过滤后的分组渲染。
func proxyNamesForNodes(nodes []Node) []string {
	proxyNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		proxyNames = append(proxyNames, node.Remark)
	}
	return proxyNames
}

func clashProtocolType(protocol string) string {
	if protocol == "shadowsocks" {
		return "ss"
	}
	return protocol
}

func directClashType(node Node) string {
	if rawType := directNodeString(node, "type"); rawType != "" {
		return rawType
	}
	return clashProtocolType(node.Protocol)
}

func directClashSkipField(field string) bool {
	switch field {
	case "id", "user", "direct", "tag", "remark", "region", "protocol", "type", "name":
		return true
	default:
		return false
	}
}

func surgeAuth(node Node) string {
	if node.Auth == nil || node.Auth.Type != "password" {
		return ""
	}
	return ", " + node.Auth.Username + ", " + node.Auth.Password
}

func resolveNodeRegion(node Node) string {
	if node.Region != "" {
		return node.Region
	}
	matches := regionPrefixPattern.FindStringSubmatch(node.Remark)
	if len(matches) == 3 {
		if matches[1] != "" {
			return matches[1]
		}
		return matches[2]
	}
	return "OTHER"
}

func regionGroupName(region string) string {
	if region == "OTHER" {
		return "🌐 其他地区"
	}
	if metadata, ok := regionMetadata[region]; ok {
		return metadata["name"]
	}
	return region + "地区"
}

func regionIconURL(region string) string {
	if metadata, ok := regionMetadata[region]; ok {
		return metadata["icon_url"]
	}
	return ""
}

func directNodeString(node Node, field string) string {
	value := node.RawField(field)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func directNodeBool(node Node, field string) bool {
	value := node.RawField(field)
	if value == nil {
		return false
	}
	var result bool
	if err := value.Decode(&result); err != nil {
		return false
	}
	return result
}

func directNodeNetwork(node Node) string {
	if network := directNodeString(node, "network"); network != "" {
		return normalizeSurgeNetwork(network)
	}
	return normalizeSurgeNetwork(node.Network)
}

// directNodeSurgeNetwork 返回 direct 节点在 Surge 中可使用的 VMess network。
func directNodeSurgeNetwork(node Node) string {
	network := directNodeNetwork(node)
	if network == "" && directNodeFieldAny(node, "ws-opts", "ws_opts") != nil {
		return "ws"
	}
	return network
}

func directNodeUsesWebSocket(node Node, network string) bool {
	return network == "ws" || directNodeFieldAny(node, "ws-opts", "ws_opts") != nil
}

func directNodeFieldAny(node Node, fields ...string) *yaml.Node {
	for _, field := range fields {
		if value := node.RawField(field); value != nil {
			return value
		}
	}
	return nil
}

func directNodeWSPath(node Node) string {
	wsOpts := directNodeFieldAny(node, "ws-opts", "ws_opts")
	if wsOpts == nil || wsOpts.Kind != yaml.MappingNode {
		return ""
	}
	for index := 0; index+1 < len(wsOpts.Content); index += 2 {
		if wsOpts.Content[index].Value == "path" && wsOpts.Content[index+1].Kind == yaml.ScalarNode {
			return wsOpts.Content[index+1].Value
		}
	}
	return ""
}

func directNodeWSHeaders(node Node) string {
	wsOpts := directNodeFieldAny(node, "ws-opts", "ws_opts")
	if wsOpts == nil || wsOpts.Kind != yaml.MappingNode {
		return ""
	}
	for index := 0; index+1 < len(wsOpts.Content); index += 2 {
		if wsOpts.Content[index].Value != "headers" {
			continue
		}
		headers := wsOpts.Content[index+1]
		switch headers.Kind {
		case yaml.ScalarNode:
			return headers.Value
		case yaml.MappingNode:
			values := make([]string, 0, len(headers.Content)/2)
			for headerIndex := 0; headerIndex+1 < len(headers.Content); headerIndex += 2 {
				if headers.Content[headerIndex+1].Kind == yaml.ScalarNode {
					values = append(values, headers.Content[headerIndex].Value+":"+headers.Content[headerIndex+1].Value)
				}
			}
			return strings.Join(values, "|")
		}
	}
	return ""
}

func normalizeSurgeNetwork(value string) string {
	switch strings.ToLower(value) {
	case "websocket":
		return "ws"
	default:
		return value
	}
}

func toJSONString(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func firstIdentifier(expression string) string {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ""
	}
	for _, delimiter := range []string{"|", " ", ".", "(", "[", "==", "!=", ">", "<"} {
		if index := strings.Index(expression, delimiter); index >= 0 {
			expression = expression[:index]
		}
	}
	expression = strings.TrimSpace(expression)
	if expression == "" || expression == "true" || expression == "false" || expression == "none" || expression == "nil" {
		return ""
	}
	if strings.HasPrefix(expression, "\"") || strings.HasPrefix(expression, "'") {
		return ""
	}
	return expression
}
