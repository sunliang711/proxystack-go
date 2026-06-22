package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	InputSchema  = "proxystack.subscription-input"
	InputVersion = 1
	IndexVersion = 1

	BundleSchema  = "proxystack.sub-bundle"
	BundleVersion = 1
)

var regionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

var nodeKnownFields = map[string]bool{
	"id":       true,
	"user":     true,
	"protocol": true,
	"server":   true,
	"port":     true,
	"tag":      true,
	"remark":   true,
	"uuid":     true,
	"network":  true,
	"method":   true,
	"cipher":   true,
	"password": true,
	"udp":      true,
	"auth":     true,
	"region":   true,
	"direct":   true,
}

var authKnownFields = map[string]bool{
	"type":     true,
	"username": true,
	"password": true,
}

// GeneratorError 表示订阅生成、合并或导入失败。
type GeneratorError struct {
	Message string
}

// Error 返回错误原因。
func (e GeneratorError) Error() string {
	return e.Message
}

// TemplateError 表示订阅模板读取或渲染失败。
type TemplateError struct {
	Message string
}

// Error 返回错误原因。
func (e TemplateError) Error() string {
	return e.Message
}

// Auth 保存 socks/http 订阅节点认证数据。
type Auth struct {
	Type     string `json:"type" yaml:"type"`
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
}

// Validate 校验认证类型和 password 模式凭据完整性。
func (a Auth) Validate() error {
	switch a.Type {
	case "noauth":
		return nil
	case "password":
		if a.Username == "" || a.Password == "" {
			return fmt.Errorf("username and password are required for password auth")
		}
		return nil
	default:
		return fmt.Errorf("auth.type must be noauth or password")
	}
}

// Node 是 agent 和 sub 之间传递的单个订阅节点。
type Node struct {
	ID       string `json:"id" yaml:"id"`
	User     string `json:"user" yaml:"user"`
	Direct   bool   `json:"direct,omitempty" yaml:"direct,omitempty"`
	Protocol string `json:"protocol" yaml:"protocol"`
	Server   string `json:"server" yaml:"server"`
	Port     int    `json:"port" yaml:"port"`
	Tag      string `json:"tag" yaml:"tag"`
	Remark   string `json:"remark" yaml:"remark"`
	UUID     string `json:"uuid,omitempty" yaml:"uuid,omitempty"`
	Network  string `json:"network,omitempty" yaml:"network,omitempty"`
	Method   string `json:"method,omitempty" yaml:"method,omitempty"`
	Cipher   string `json:"cipher,omitempty" yaml:"cipher,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	UDP      *bool  `json:"udp,omitempty" yaml:"udp,omitempty"`
	Auth     *Auth  `json:"auth,omitempty" yaml:"auth,omitempty"`
	Region   string `json:"region,omitempty" yaml:"region,omitempty"`

	rawFields []yamlNodePair
}

// UnmarshalYAML 按 direct 开关决定是否允许节点级自定义字段。
func (n *Node) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("node must be a mapping")
	}
	direct := yamlMappingBool(value, "direct")
	if !direct {
		if err := rejectUnknownNodeYAMLFields(value); err != nil {
			return err
		}
	}
	type raw Node
	var decoded raw
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*n = Node(decoded)
	n.Direct = direct
	if n.Direct {
		n.rawFields = cloneYAMLMappingPairs(value)
		n.applyDirectAliases(yamlMappingString(value, "type"), yamlMappingString(value, "name"))
	}
	return nil
}

// UnmarshalJSON 按 direct 开关兼容 JSON input 中的直通节点。
func (n *Node) UnmarshalJSON(data []byte) error {
	type raw Node
	var decoded raw
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if !decoded.Direct {
		if err := rejectUnknownNodeJSONFields(fields); err != nil {
			return err
		}
	}
	*n = Node(decoded)
	if n.Direct {
		n.rawFields = jsonFieldsToYAMLPairs(fields)
		n.applyDirectAliases(jsonFieldString(fields, "type"), jsonFieldString(fields, "name"))
	}
	return nil
}

// MarshalJSON 输出 direct 节点时把保留的原始字段合并回 JSON。
func (n Node) MarshalJSON() ([]byte, error) {
	type raw Node
	if !n.Direct {
		return json.Marshal(raw(n))
	}
	fields := map[string]any{}
	for _, field := range n.RawFields() {
		if field.key != nil && field.key.Value != "" {
			fields[field.key.Value] = yamlNodeToJSONValue(field.value)
		}
	}
	fields["id"] = n.ID
	fields["user"] = n.User
	fields["direct"] = true
	fields["protocol"] = n.Protocol
	fields["server"] = n.Server
	fields["port"] = n.Port
	if n.Tag != "" {
		fields["tag"] = n.Tag
	}
	fields["remark"] = n.Remark
	if n.Region != "" {
		fields["region"] = n.Region
	}
	if n.UUID != "" {
		fields["uuid"] = n.UUID
	}
	if n.Network != "" {
		fields["network"] = n.Network
	}
	if n.Method != "" {
		fields["method"] = n.Method
	}
	if n.Cipher != "" {
		fields["cipher"] = n.Cipher
	}
	if n.Password != "" {
		fields["password"] = n.Password
	}
	if n.UDP != nil {
		fields["udp"] = *n.UDP
	}
	if n.Auth != nil {
		fields["auth"] = n.Auth
	}
	return json.Marshal(fields)
}

// RawFields 返回 direct 节点保留的原始字段副本。
func (n Node) RawFields() []yamlNodePair {
	fields := make([]yamlNodePair, 0, len(n.rawFields))
	for _, field := range n.rawFields {
		fields = append(fields, yamlNodePair{key: cloneYAMLNode(field.key), value: cloneYAMLNode(field.value)})
	}
	return fields
}

// RawField 返回 direct 节点中指定原始字段的副本。
func (n Node) RawField(name string) *yaml.Node {
	for _, field := range n.rawFields {
		if field.key != nil && field.key.Value == name {
			return cloneYAMLNode(field.value)
		}
	}
	return nil
}

func (n *Node) applyDirectAliases(proxyType string, proxyName string) {
	if n.Protocol == "" {
		n.Protocol = normalizeDirectProtocol(proxyType)
	}
	if n.Remark == "" {
		n.Remark = proxyName
	}
	if n.Tag == "" {
		n.Tag = n.ID
	}
}

// Validate 校验节点基础字段和各协议必填项。
func (n Node) Validate() error {
	if n.ID == "" {
		return fmt.Errorf("node.id is required")
	}
	if n.User == "" {
		return fmt.Errorf("node.user is required")
	}
	if n.Server == "" {
		return fmt.Errorf("node.server is required")
	}
	if n.Port < 1 || n.Port > 65535 {
		return fmt.Errorf("node.port must be between 1 and 65535")
	}
	if n.Direct {
		return n.validateDirect()
	}
	if n.Tag == "" {
		return fmt.Errorf("node.tag is required")
	}
	if n.Remark == "" {
		return fmt.Errorf("node.remark is required")
	}
	if n.Region != "" && !regionPattern.MatchString(n.Region) {
		return fmt.Errorf("node.region must use two uppercase letters")
	}
	if n.Auth != nil {
		if err := n.Auth.Validate(); err != nil {
			return err
		}
	}
	switch n.Protocol {
	case "vmess":
		if n.UUID == "" {
			return fmt.Errorf("uuid is required for vmess node")
		}
		if n.Network == "" {
			return fmt.Errorf("network is required for vmess node")
		}
	case "shadowsocks":
		if n.Password == "" {
			return fmt.Errorf("password is required for shadowsocks node")
		}
		if n.Method == "" && n.Cipher == "" {
			return fmt.Errorf("method or cipher is required for shadowsocks node")
		}
	case "socks5", "http":
	default:
		return fmt.Errorf("unsupported subscription node protocol: %s", n.Protocol)
	}
	if n.UDP != nil && !supportsSubscriptionUDPProtocol(n.Protocol) {
		return fmt.Errorf("udp is not supported for %s node", n.Protocol)
	}
	return nil
}

// validateDirect 校验直通节点生成订阅所需的最小字段。
func (n Node) validateDirect() error {
	if n.Remark == "" {
		return fmt.Errorf("node.remark is required")
	}
	if n.Region != "" && !regionPattern.MatchString(n.Region) {
		return fmt.Errorf("node.region must use two uppercase letters")
	}
	switch n.Protocol {
	case "vmess":
		if n.UUID == "" {
			return fmt.Errorf("uuid is required for vmess node")
		}
	case "shadowsocks":
		if n.Password == "" {
			return fmt.Errorf("password is required for shadowsocks node")
		}
		if n.Method == "" && n.Cipher == "" {
			return fmt.Errorf("method or cipher is required for shadowsocks node")
		}
	case "socks5", "http":
	default:
		// direct 节点允许 Clash/Mihomo 原生协议；各客户端不支持时在对应渲染阶段跳过。
		return nil
	}
	return nil
}

// Input 是 agent 输出、sub 读取的订阅 input 文件。
type Input struct {
	InputSchema  string `json:"input_schema" yaml:"input_schema"`
	InputVersion int    `json:"input_version" yaml:"input_version"`
	Source       string `json:"source" yaml:"source"`
	GeneratedAt  string `json:"generated_at" yaml:"generated_at"`
	ExternalHost string `json:"external_host,omitempty" yaml:"external_host,omitempty"`
	Nodes        []Node `json:"nodes" yaml:"nodes"`
}

// resolveExternalHostDefault 返回补齐文件级 external_host 后的 input。
func (i Input) resolveExternalHostDefault() Input {
	if i.ExternalHost == "" {
		return i
	}
	input := i
	input.Nodes = append([]Node(nil), i.Nodes...)
	for index := range input.Nodes {
		if input.Nodes[index].Server == "" {
			input.Nodes[index].Server = input.ExternalHost
		}
	}
	return input
}

// Validate 校验 input schema、版本和内部 node.id 唯一性。
func (i Input) Validate() error {
	input := i.resolveExternalHostDefault()
	if input.InputSchema != "" && input.InputSchema != InputSchema {
		return fmt.Errorf("unsupported subscription input schema: %s", input.InputSchema)
	}
	if input.InputVersion != InputVersion {
		return fmt.Errorf("unsupported subscription input version: %d", input.InputVersion)
	}
	if input.Source == "" {
		return fmt.Errorf("input.source is required")
	}
	if input.GeneratedAt == "" {
		return fmt.Errorf("input.generated_at is required")
	}
	seen := map[string]bool{}
	for _, node := range input.Nodes {
		if err := node.Validate(); err != nil {
			return err
		}
		if seen[node.ID] {
			return fmt.Errorf("duplicate node id in input: %s", node.ID)
		}
		seen[node.ID] = true
	}
	return nil
}

// Access 是订阅 HTTP 访问控制信息。
type Access struct {
	Type  string `json:"type" yaml:"type"`
	Token string `json:"token,omitempty" yaml:"token,omitempty"`
}

// Validate 校验访问类型和 token 必填规则。
func (a Access) Validate() error {
	switch a.Type {
	case "", "none":
		return nil
	case "token":
		if a.Token == "" {
			return fmt.Errorf("token is required when access type is token")
		}
		return nil
	default:
		return fmt.Errorf("access.type must be none or token")
	}
}

// Normalized 返回补齐默认值后的 access。
func (a Access) Normalized() Access {
	if a.Type == "" {
		a.Type = "none"
	}
	return a
}

// Index 是合并后的订阅索引。
type Index struct {
	IndexVersion int               `json:"index_version"`
	GeneratedAt  string            `json:"generated_at"`
	Sources      []string          `json:"sources"`
	Nodes        []Node            `json:"nodes"`
	Users        map[string][]Node `json:"users"`
	Access       Access            `json:"access"`
}

// Validate 校验 index schema 和 access。
func (i Index) Validate() error {
	if i.IndexVersion != IndexVersion {
		return fmt.Errorf("unsupported subscription index version: %d", i.IndexVersion)
	}
	if i.GeneratedAt == "" {
		return fmt.Errorf("index.generated_at is required")
	}
	if err := i.Access.Validate(); err != nil {
		return err
	}
	for _, node := range i.Nodes {
		if err := node.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// InputFile 表示一个已加载 input 文件及其文件名。
type InputFile struct {
	Name  string
	Input Input
}

// InputToYAML 把订阅 input 编码为稳定 YAML 文本。
func InputToYAML(input Input) string {
	pairs := []yamlNodePair{
		yamlPair("input_schema", yamlString(InputSchema)),
		yamlPair("input_version", yamlInt(InputVersion)),
		yamlPair("source", yamlString(input.Source)),
		yamlPair("generated_at", yamlSingleQuotedString(input.GeneratedAt)),
	}
	if input.ExternalHost != "" {
		pairs = append(pairs, yamlPair("external_host", yamlString(input.ExternalHost)))
	}
	pairs = append(pairs, yamlPair("nodes", nodesToYAML(input.Nodes)))
	root := yamlMapping(pairs...)
	return encodeYAML(root)
}

// IndexToJSON 把订阅 index 编码为稳定 JSON 文本。
func IndexToJSON(index Index) (string, error) {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func decodeStrictYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

func encodeYAML(node *yaml.Node) string {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	_ = encoder.Encode(node)
	_ = encoder.Close()
	return buffer.String()
}

type yamlNodePair struct {
	key   *yaml.Node
	value *yaml.Node
}

func yamlPair(key string, value *yaml.Node) yamlNodePair {
	return yamlNodePair{key: yamlString(key), value: value}
}

func yamlMapping(pairs ...yamlNodePair) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, pair := range pairs {
		node.Content = append(node.Content, pair.key, pair.value)
	}
	return node
}

func yamlSequence(values ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: values}
}

func yamlString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlSingleQuotedString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: yaml.SingleQuotedStyle}
}

func yamlInt(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
}

func yamlBool(value bool) *yaml.Node {
	if value {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func nodesToYAML(nodes []Node) *yaml.Node {
	rendered := make([]*yaml.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Direct {
			rendered = append(rendered, directNodeToYAML(node))
			continue
		}
		pairs := []yamlNodePair{
			yamlPair("id", yamlString(node.ID)),
			yamlPair("user", yamlString(node.User)),
			yamlPair("protocol", yamlString(node.Protocol)),
			yamlPair("server", yamlString(node.Server)),
			yamlPair("port", yamlInt(node.Port)),
			yamlPair("tag", yamlString(node.Tag)),
			yamlPair("remark", yamlString(node.Remark)),
		}
		if node.Region != "" {
			pairs = append(pairs, yamlPair("region", yamlString(node.Region)))
		}
		if node.UUID != "" {
			pairs = append(pairs, yamlPair("uuid", yamlString(node.UUID)))
		}
		if node.Network != "" {
			pairs = append(pairs, yamlPair("network", yamlString(node.Network)))
		}
		if node.Method != "" {
			pairs = append(pairs, yamlPair("method", yamlString(node.Method)))
		}
		if node.Cipher != "" {
			pairs = append(pairs, yamlPair("cipher", yamlString(node.Cipher)))
		}
		if node.Password != "" {
			pairs = append(pairs, yamlPair("password", yamlString(node.Password)))
		}
		if node.UDP != nil {
			pairs = append(pairs, yamlPair("udp", yamlBool(*node.UDP)))
		}
		if node.Auth != nil {
			authPairs := []yamlNodePair{yamlPair("type", yamlString(node.Auth.Type))}
			if node.Auth.Username != "" {
				authPairs = append(authPairs, yamlPair("username", yamlString(node.Auth.Username)))
			}
			if node.Auth.Password != "" {
				authPairs = append(authPairs, yamlPair("password", yamlString(node.Auth.Password)))
			}
			pairs = append(pairs, yamlPair("auth", yamlMapping(authPairs...)))
		}
		rendered = append(rendered, yamlMapping(pairs...))
	}
	return yamlSequence(rendered...)
}

func directNodeToYAML(node Node) *yaml.Node {
	pairs := []yamlNodePair{
		yamlPair("id", yamlString(node.ID)),
		yamlPair("user", yamlString(node.User)),
		yamlPair("direct", yamlBool(true)),
		yamlPair("protocol", yamlString(node.Protocol)),
		yamlPair("server", yamlString(node.Server)),
		yamlPair("port", yamlInt(node.Port)),
	}
	if node.Tag != "" {
		pairs = append(pairs, yamlPair("tag", yamlString(node.Tag)))
	}
	pairs = append(pairs, yamlPair("remark", yamlString(node.Remark)))
	if node.Region != "" {
		pairs = append(pairs, yamlPair("region", yamlString(node.Region)))
	}
	if node.UUID != "" {
		pairs = append(pairs, yamlPair("uuid", yamlString(node.UUID)))
	}
	if node.Network != "" {
		pairs = append(pairs, yamlPair("network", yamlString(node.Network)))
	}
	if node.Method != "" {
		pairs = append(pairs, yamlPair("method", yamlString(node.Method)))
	}
	if node.Cipher != "" {
		pairs = append(pairs, yamlPair("cipher", yamlString(node.Cipher)))
	}
	if node.Password != "" {
		pairs = append(pairs, yamlPair("password", yamlString(node.Password)))
	}
	if node.UDP != nil {
		pairs = append(pairs, yamlPair("udp", yamlBool(*node.UDP)))
	}
	seen := map[string]bool{}
	for _, pair := range pairs {
		seen[pair.key.Value] = true
	}
	for _, pair := range node.RawFields() {
		if pair.key == nil || seen[pair.key.Value] || pair.key.Value == "type" || pair.key.Value == "name" {
			continue
		}
		pairs = append(pairs, pair)
		seen[pair.key.Value] = true
	}
	return yamlMapping(pairs...)
}

func rejectUnknownNodeYAMLFields(value *yaml.Node) error {
	for index := 0; index+1 < len(value.Content); index += 2 {
		key := value.Content[index]
		if !nodeKnownFields[key.Value] {
			if key.Line > 0 {
				return fmt.Errorf("line %d: field %s not found in type sub.Node", key.Line, key.Value)
			}
			return fmt.Errorf("field %s not found in type sub.Node", key.Value)
		}
		if key.Value == "auth" {
			if err := rejectUnknownAuthYAMLFields(value.Content[index+1]); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectUnknownNodeJSONFields(fields map[string]json.RawMessage) error {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !nodeKnownFields[key] {
			return fmt.Errorf("json: unknown field %q", key)
		}
	}
	return rejectUnknownAuthJSONFields(fields)
}

func rejectUnknownAuthYAMLFields(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		key := value.Content[index]
		if !authKnownFields[key.Value] {
			if key.Line > 0 {
				return fmt.Errorf("line %d: field %s not found in type sub.Auth", key.Line, key.Value)
			}
			return fmt.Errorf("field %s not found in type sub.Auth", key.Value)
		}
	}
	return nil
}

func rejectUnknownAuthJSONFields(fields map[string]json.RawMessage) error {
	data, ok := fields["auth"]
	if !ok || string(data) == "null" {
		return nil
	}
	var authFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &authFields); err != nil {
		return err
	}
	keys := make([]string, 0, len(authFields))
	for key := range authFields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !authKnownFields[key] {
			return fmt.Errorf("json: unknown field %q", key)
		}
	}
	return nil
}

func yamlMappingString(value *yaml.Node, name string) string {
	field := yamlMappingField(value, name)
	if field == nil || field.Kind != yaml.ScalarNode {
		return ""
	}
	return field.Value
}

func yamlMappingBool(value *yaml.Node, name string) bool {
	field := yamlMappingField(value, name)
	if field == nil {
		return false
	}
	var result bool
	if err := field.Decode(&result); err != nil {
		return false
	}
	return result
}

func yamlMappingField(value *yaml.Node, name string) *yaml.Node {
	for index := 0; index+1 < len(value.Content); index += 2 {
		if value.Content[index].Value == name {
			return value.Content[index+1]
		}
	}
	return nil
}

func cloneYAMLMappingPairs(value *yaml.Node) []yamlNodePair {
	pairs := make([]yamlNodePair, 0, len(value.Content)/2)
	for index := 0; index+1 < len(value.Content); index += 2 {
		pairs = append(pairs, yamlNodePair{
			key:   cloneYAMLNode(value.Content[index]),
			value: cloneYAMLNode(value.Content[index+1]),
		})
	}
	return pairs
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	cloned.Content = make([]*yaml.Node, 0, len(node.Content))
	for _, child := range node.Content {
		cloned.Content = append(cloned.Content, cloneYAMLNode(child))
	}
	return &cloned
}

func jsonFieldString(fields map[string]json.RawMessage, name string) string {
	data, ok := fields[name]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	return value
}

func jsonFieldsToYAMLPairs(fields map[string]json.RawMessage) []yamlNodePair {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]yamlNodePair, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, yamlNodePair{key: yamlString(key), value: jsonRawToYAMLNode(fields[key])})
	}
	return pairs
}

func jsonRawToYAMLNode(data json.RawMessage) *yaml.Node {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return yamlString(string(data))
	}
	return jsonValueToYAMLNode(value)
}

func jsonValueToYAMLNode(value any) *yaml.Node {
	switch typed := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case string:
		return yamlString(typed)
	case bool:
		return yamlBool(typed)
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(typed.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: typed.String()}
	case []any:
		values := make([]*yaml.Node, 0, len(typed))
		for _, item := range typed {
			values = append(values, jsonValueToYAMLNode(item))
		}
		return yamlSequence(values...)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		pairs := make([]yamlNodePair, 0, len(keys))
		for _, key := range keys {
			pairs = append(pairs, yamlPair(key, jsonValueToYAMLNode(typed[key])))
		}
		return yamlMapping(pairs...)
	default:
		return yamlString(fmt.Sprintf("%v", typed))
	}
}

func yamlNodeToJSONValue(node *yaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!bool":
			var value bool
			if err := node.Decode(&value); err == nil {
				return value
			}
		case "!!int":
			if value, err := strconv.ParseInt(node.Value, 10, 64); err == nil {
				return value
			}
		case "!!float":
			if value, err := strconv.ParseFloat(node.Value, 64); err == nil {
				return value
			}
		case "!!null":
			return nil
		}
		return node.Value
	case yaml.SequenceNode:
		values := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			values = append(values, yamlNodeToJSONValue(child))
		}
		return values
	case yaml.MappingNode:
		values := map[string]any{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			values[node.Content[index].Value] = yamlNodeToJSONValue(node.Content[index+1])
		}
		return values
	default:
		return node.Value
	}
}

func normalizeDirectProtocol(value string) string {
	switch value {
	case "ss":
		return "shadowsocks"
	default:
		return value
	}
}

// supportsSubscriptionUDPProtocol 判断默认订阅节点协议是否支持 udp 字段。
func supportsSubscriptionUDPProtocol(protocol string) bool {
	switch protocol {
	case "vmess", "shadowsocks", "socks5":
		return true
	default:
		return false
	}
}
