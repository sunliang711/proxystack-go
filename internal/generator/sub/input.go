package sub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/eagle/proxystack-go/internal/domain"
)

var supportedInputExtensions = map[string]bool{
	".yaml": true,
	".yml":  true,
	".json": true,
}

// RenderStackInputAt 从所有启用 stack 的 sub inbound 生成订阅 input。
func RenderStackInputAt(stackSet domain.StackSet, source string, generatedAt string) (Input, error) {
	nodes := make([]Node, 0)
	for _, stack := range stackSet.Stacks {
		if !stack.Enabled || !stack.Xrelay.Enabled {
			continue
		}
		stackNodes, err := renderStackNodes(stackSet, stack)
		if err != nil {
			return Input{}, err
		}
		nodes = append(nodes, stackNodes...)
	}
	if source == "" {
		source = "local"
	}
	input := Input{
		InputSchema:  InputSchema,
		InputVersion: InputVersion,
		Source:       source,
		GeneratedAt:  generatedAt,
		Nodes:        nodes,
	}
	return input, input.Validate()
}

// RenderSingleStackInputAt 从指定 stack 的 sub inbound 生成订阅 input。
func RenderSingleStackInputAt(stackSet domain.StackSet, stackName string, generatedAt string) (Input, error) {
	stack, ok := stackSet.ByName()[stackName]
	if !ok {
		return Input{}, GeneratorError{Message: "stack does not exist: " + stackName}
	}
	nodes, err := renderStackNodes(stackSet, *stack)
	if err != nil {
		return Input{}, err
	}
	input := Input{
		InputSchema:  InputSchema,
		InputVersion: InputVersion,
		Source:       stackName,
		GeneratedAt:  generatedAt,
		Nodes:        nodes,
	}
	return input, input.Validate()
}

func renderStackNodes(stackSet domain.StackSet, stack domain.Stack) ([]Node, error) {
	if !stack.Enabled || !stack.Xrelay.Enabled {
		return nil, nil
	}
	nodes := make([]Node, 0)
	for _, inbound := range stack.Xrelay.Inbounds {
		if !inbound.Sub {
			continue
		}
		inboundNodes, err := renderInboundNodes(stackSet, stack, inbound)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, inboundNodes...)
	}
	return nodes, nil
}

func renderInboundNodes(stackSet domain.StackSet, stack domain.Stack, inbound domain.Inbound) ([]Node, error) {
	switch {
	case inbound.Protocol == "vmess":
		nodes := make([]Node, 0, len(inbound.Users))
		for _, user := range inbound.Users {
			node, err := renderVmessUserNode(stackSet, stack, inbound, user)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
		return nodes, nil
	case inbound.Protocol == "shadowsocks" && len(inbound.Users) > 0:
		nodes := make([]Node, 0, len(inbound.Users))
		for _, user := range inbound.Users {
			node, err := renderShadowsocksUserNode(stackSet, stack, inbound, user)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
		return nodes, nil
	default:
		node, err := renderSingleInboundNode(stackSet, stack, inbound)
		if err != nil {
			return nil, err
		}
		return []Node{node}, nil
	}
}

func renderSingleInboundNode(stackSet domain.StackSet, stack domain.Stack, inbound domain.Inbound) (Node, error) {
	user := inbound.User
	if user == "" {
		user = "default"
	}
	node := Node{
		ID:       stack.Name + ":" + inbound.Name,
		User:     user,
		Protocol: inbound.Protocol,
		Server:   subscriptionServer(stackSet, inbound),
		Port:     inbound.Port,
		Tag:      inbound.TagOrDefault(),
		Remark:   subscriptionRemark(stack.Name, inbound, user, inbound.Remark),
		Region:   inbound.Region,
	}
	applyInboundUDP(&node, inbound)
	switch inbound.Protocol {
	case "shadowsocks":
		method := inbound.MethodOrCipher()
		node.Method = method
		node.Cipher = method
		node.Password = inbound.Password
	case "socks5", "http":
		if inbound.Auth != nil {
			node.Auth = &Auth{Type: inbound.Auth.Type, Username: inbound.Auth.Username, Password: inbound.Auth.Password}
		}
	}
	if err := node.Validate(); err != nil {
		return Node{}, err
	}
	return node, nil
}

func renderVmessUserNode(stackSet domain.StackSet, stack domain.Stack, inbound domain.Inbound, user domain.InboundUser) (Node, error) {
	tag := user.Tag
	if tag == "" {
		tag = inbound.TagOrDefault() + ":" + user.User
	}
	node := Node{
		ID:       stack.Name + ":" + inbound.Name + ":" + user.User,
		User:     user.User,
		Protocol: inbound.Protocol,
		Server:   subscriptionServer(stackSet, inbound),
		Port:     inbound.Port,
		Tag:      tag,
		Remark:   subscriptionRemark(stack.Name, inbound, user.User, user.Remark),
		Region:   inbound.Region,
		UUID:     user.UUID,
		Network:  inbound.Network,
		WSOpts:   inbound.WSOpts,
		GRPCOpts: inbound.GRPCOpts,
	}
	applyInboundUDP(&node, inbound)
	if err := node.Validate(); err != nil {
		return Node{}, err
	}
	return node, nil
}

func renderShadowsocksUserNode(stackSet domain.StackSet, stack domain.Stack, inbound domain.Inbound, user domain.InboundUser) (Node, error) {
	tag := user.Tag
	if tag == "" {
		tag = inbound.TagOrDefault() + ":" + user.User
	}
	method := firstNonEmpty(user.Method, user.Cipher, inbound.MethodOrCipher())
	node := Node{
		ID:       stack.Name + ":" + inbound.Name + ":" + user.User,
		User:     user.User,
		Protocol: inbound.Protocol,
		Server:   subscriptionServer(stackSet, inbound),
		Port:     inbound.Port,
		Tag:      tag,
		Remark:   subscriptionRemark(stack.Name, inbound, user.User, user.Remark),
		Region:   inbound.Region,
		Method:   method,
		Cipher:   method,
		Password: shadowsocksNodePassword(inbound, user),
	}
	applyInboundUDP(&node, inbound)
	if err := node.Validate(); err != nil {
		return Node{}, err
	}
	return node, nil
}

// applyInboundUDP 把显式 udp 配置原样传递给订阅节点。
func applyInboundUDP(node *Node, inbound domain.Inbound) {
	if inbound.UDPConfigured() {
		node.UDP = boolPtr(inbound.UDP)
	}
}

func subscriptionServer(stackSet domain.StackSet, inbound domain.Inbound) string {
	if inbound.Server != "" {
		return inbound.Server
	}
	return stackSet.Config.ExternalHost
}

func subscriptionRemark(stackName string, inbound domain.Inbound, user string, configuredRemark string) string {
	remark := configuredRemark
	if remark == "" {
		remark = inbound.Name
	}
	return fmt.Sprintf("%s@%s-%s:%d-%s", user, stackName, inbound.Protocol, inbound.Port, remark)
}

func shadowsocksNodePassword(inbound domain.Inbound, user domain.InboundUser) string {
	if domain.IsShadowsocks2022Method(inbound.MethodOrCipher()) {
		return inbound.Password + ":" + user.Password
	}
	return user.Password
}

// ScanInputFiles 扫描 inputs 目录，按文件名稳定排序。
func ScanInputFiles(inputDir string) ([]string, error) {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return nil, GeneratorError{Message: fmt.Sprintf("input directory could not be read: %s (%v)", inputDir, err)}
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := filepath.Ext(entry.Name())
		if !supportedInputExtensions[extension] {
			continue
		}
		paths = append(paths, filepath.Join(inputDir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// LoadInputFile 读取并严格校验单个订阅 input 文件。
func LoadInputFile(path string) (Input, error) {
	extension := filepath.Ext(path)
	if !supportedInputExtensions[extension] {
		return Input{}, GeneratorError{Message: "unsupported input extension: " + filepath.Base(path)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Input{}, GeneratorError{Message: fmt.Sprintf("subscription input could not be read: %s (%v)", path, err)}
	}
	input, err := LoadInputContent(filepath.Base(path), data)
	if err != nil {
		return Input{}, err
	}
	return input, nil
}

// LoadInputContent 从 bundle 成员内容读取并严格校验 input。
func LoadInputContent(name string, data []byte) (Input, error) {
	var input Input
	switch filepath.Ext(name) {
	case ".json":
		decoder := json.NewDecoder(bytesReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return Input{}, GeneratorError{Message: fmt.Sprintf("invalid subscription input %s: %v", name, err)}
		}
	case ".yaml", ".yml":
		if err := decodeStrictYAML(data, &input); err != nil {
			return Input{}, GeneratorError{Message: fmt.Sprintf("invalid subscription input %s: %v", name, err)}
		}
	default:
		return Input{}, GeneratorError{Message: "unsupported input extension: " + name}
	}
	if input.InputSchema == "" {
		input.InputSchema = InputSchema
	}
	input = input.resolveExternalHostDefault()
	if err := input.Validate(); err != nil {
		return Input{}, GeneratorError{Message: fmt.Sprintf("invalid subscription input %s: %v", name, err)}
	}
	return input, nil
}

// LoadInputs 读取 inputs 目录中的所有 input。
func LoadInputs(inputDir string) ([]InputFile, error) {
	paths, err := ScanInputFiles(inputDir)
	if err != nil {
		return nil, err
	}
	inputs := make([]InputFile, 0, len(paths))
	for _, path := range paths {
		input, err := LoadInputFile(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, InputFile{Name: filepath.Base(path), Input: input})
	}
	return inputs, nil
}

// MergeInputFiles 扫描并合并 inputs 目录。
func MergeInputFiles(inputDir string, access Access, generatedAt string) (Index, error) {
	inputs, err := LoadInputs(inputDir)
	if err != nil {
		return Index{}, err
	}
	return MergeInputs(inputs, access, generatedAt)
}

// MergeInputs 按给定顺序合并 input，并校验重复 node.id 和同用户代理名。
func MergeInputs(inputs []InputFile, access Access, generatedAt string) (Index, error) {
	nodes := make([]Node, 0)
	sources := make([]string, 0, len(inputs))
	nodeSources := map[string]string{}
	proxyNameSources := map[string]string{}
	for _, inputFile := range inputs {
		input := inputFile.Input.resolveExternalHostDefault()
		if err := input.Validate(); err != nil {
			return Index{}, err
		}
		sources = append(sources, input.Source)
		for _, node := range input.Nodes {
			if firstSource, ok := nodeSources[node.ID]; ok {
				return Index{}, GeneratorError{Message: fmt.Sprintf("duplicate node id: %s, first seen in %s, repeated in %s", node.ID, firstSource, inputFile.Name)}
			}
			nodeSources[node.ID] = inputFile.Name
			proxyNameKey := node.User + "\x00" + node.Remark
			if firstSource, ok := proxyNameSources[proxyNameKey]; ok {
				return Index{}, GeneratorError{Message: fmt.Sprintf("duplicate proxy name for user: user=%s name=%s, first seen in %s, repeated in %s", node.User, node.Remark, firstSource, inputFile.Name)}
			}
			proxyNameSources[proxyNameKey] = inputFile.Name
			nodes = append(nodes, node)
		}
	}
	return BuildIndex(nodes, sources, access, generatedAt)
}

// BuildIndex 根据节点列表生成按 user 分组的 index。
func BuildIndex(nodes []Node, sources []string, access Access, generatedAt string) (Index, error) {
	if generatedAt == "" {
		return Index{}, GeneratorError{Message: "generated_at is required"}
	}
	if access.Type == "" {
		access.Type = "none"
	}
	if err := access.Validate(); err != nil {
		return Index{}, err
	}
	users := map[string][]Node{}
	for _, node := range nodes {
		if err := node.Validate(); err != nil {
			return Index{}, err
		}
		users[node.User] = append(users[node.User], node)
	}
	orderedUsers := map[string][]Node{}
	userNames := make([]string, 0, len(users))
	for user := range users {
		userNames = append(userNames, user)
	}
	sort.Strings(userNames)
	for _, user := range userNames {
		orderedUsers[user] = users[user]
	}
	index := Index{
		IndexVersion: IndexVersion,
		GeneratedAt:  generatedAt,
		Sources:      sources,
		Nodes:        nodes,
		Users:        orderedUsers,
		Access:       access,
	}
	return index, index.Validate()
}

func boolPtr(value bool) *bool {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func bytesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
