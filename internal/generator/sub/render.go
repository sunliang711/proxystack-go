package sub

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
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

// ClashProxy 是模板 yaml_block 使用的有序 Clash proxy 结构。
type ClashProxy struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Server   string `yaml:"server" json:"server"`
	Port     int    `yaml:"port" json:"port"`
	UUID     string `yaml:"uuid,omitempty" json:"uuid,omitempty"`
	AlterID  *int   `yaml:"alterId,omitempty" json:"alterId,omitempty"`
	Cipher   string `yaml:"cipher,omitempty" json:"cipher,omitempty"`
	Network  string `yaml:"network,omitempty" json:"network,omitempty"`
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	UDP      *bool  `yaml:"udp,omitempty" json:"udp,omitempty"`
}

// ClashProxyGroup 是模板 yaml_block 使用的有序 Clash proxy-group 结构。
type ClashProxyGroup struct {
	Name     string   `yaml:"name" json:"name"`
	Type     string   `yaml:"type" json:"type"`
	URL      string   `yaml:"url,omitempty" json:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty" json:"interval,omitempty"`
	Strategy string   `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	Proxies  []string `yaml:"proxies" json:"proxies"`
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
	proxies := make([]ClashProxy, 0, len(nodes))
	proxyNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		proxy := RenderClashProxy(node)
		proxies = append(proxies, proxy)
		proxyNames = append(proxyNames, proxy.Name)
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
		"surge_proxy_lines":        RenderSurgeProxyLines(nodes),
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

// RenderClashProxy 把订阅节点转换为 Clash proxy。
func RenderClashProxy(node Node) ClashProxy {
	proxy := ClashProxy{
		Name:   node.Remark,
		Type:   clashProtocolType(node.Protocol),
		Server: node.Server,
		Port:   node.Port,
	}
	switch node.Protocol {
	case "vmess":
		alterID := 0
		proxy.UUID = node.UUID
		proxy.AlterID = &alterID
		proxy.Cipher = "auto"
		proxy.Network = node.Network
	case "shadowsocks":
		proxy.Cipher = firstNonEmpty(node.Cipher, node.Method)
		proxy.Password = node.Password
	case "socks5", "http":
		if node.Auth != nil && node.Auth.Type == "password" {
			proxy.Username = node.Auth.Username
			proxy.Password = node.Auth.Password
		}
	}
	if node.UDP != nil && (node.Protocol == "socks5" || node.Protocol == "shadowsocks") {
		proxy.UDP = node.UDP
	}
	return proxy
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

// RenderSurgeProxyLines 生成 Surge [Proxy] 行。
func RenderSurgeProxyLines(nodes []Node) []string {
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		lines = append(lines, RenderSurgeProxy(node))
	}
	return lines
}

// RenderSurgeProxy 生成单个 Surge proxy 行。
func RenderSurgeProxy(node Node) string {
	switch node.Protocol {
	case "vmess":
		return fmt.Sprintf("%s = vmess, %s, %d, username=%s, network=%s, vmess-aead=true", node.Remark, node.Server, node.Port, node.UUID, node.Network)
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

func clashProtocolType(protocol string) string {
	if protocol == "shadowsocks" {
		return "ss"
	}
	return protocol
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
