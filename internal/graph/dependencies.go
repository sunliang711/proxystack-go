package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eagle/proxystack-go/internal/domain"
)

// Issue 表示引用图校验问题。
type Issue struct {
	Path    string
	Message string
}

// ServiceNode 表示依赖图中的单个服务实例。
type ServiceNode struct {
	Stack     string
	Component string
}

// ServiceName 返回当前节点对应的 systemd 服务名。
func (s ServiceNode) ServiceName() string {
	if s.Component == "xray" {
		return "proxystack-xray@" + s.Stack + ".service"
	}
	return "proxystack-" + s.Component + "@" + s.Stack + ".service"
}

// Label 返回 check 输出使用的紧凑标签。
func (s ServiceNode) Label() string {
	return s.Stack + "." + s.Component
}

// DependencyPlan 保存目标依赖闭包和建议操作顺序。
type DependencyPlan struct {
	Target          string
	DependencyNodes []ServiceNode
	DependencyEdges [][2]ServiceNode
	OperationOrder  []ServiceNode
}

// ReferenceGraph 保存 endpoint 索引和服务依赖关系。
type ReferenceGraph struct {
	Index        ReferenceIndex
	Dependencies map[ServiceNode]map[ServiceNode]bool
	Nodes        map[ServiceNode]bool
	StackNames   map[string]bool
}

// CompileResult 表示引用图编译结果，允许校验阶段汇总多个问题。
type CompileResult struct {
	Graph  ReferenceGraph
	Issues []Issue
}

// ReferenceGraphError 表示引用图构建失败。
type ReferenceGraphError struct {
	Issues []Issue
}

// Error 输出所有引用图问题。
func (e ReferenceGraphError) Error() string {
	lines := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		lines = append(lines, issue.Path+": "+issue.Message)
	}
	return strings.Join(lines, "\n")
}

// CompileReferenceGraph 编译 stack set 的 ref endpoint 索引和服务依赖图。
func CompileReferenceGraph(stackSet domain.StackSet) CompileResult {
	index := NewReferenceIndex(stackSet.Stacks)
	nodes := CollectServiceNodes(stackSet.Stacks)
	dependencies := make(map[ServiceNode]map[ServiceNode]bool, len(nodes))
	for node := range nodes {
		dependencies[node] = map[ServiceNode]bool{}
	}
	issues := make([]Issue, 0)
	for _, stack := range stackSet.Stacks {
		issues = append(issues, addXrayDependencies(stack, index, dependencies)...)
		issues = append(issues, addClashDependencies(stack, index, dependencies)...)
	}
	stackNames := make(map[string]bool, len(stackSet.Stacks))
	for _, stack := range stackSet.Stacks {
		stackNames[stack.Name] = true
	}
	graph := ReferenceGraph{
		Index:        index,
		Dependencies: dependencies,
		Nodes:        nodes,
		StackNames:   stackNames,
	}
	if len(issues) == 0 {
		issues = append(issues, graph.ValidateAcyclic()...)
	}
	return CompileResult{Graph: graph, Issues: issues}
}

// BuildReferenceGraph 构建无错误的引用图，供 CLI check/start 和生成器使用。
func BuildReferenceGraph(stackSet domain.StackSet) (ReferenceGraph, error) {
	result := CompileReferenceGraph(stackSet)
	if len(result.Issues) > 0 {
		return ReferenceGraph{}, ReferenceGraphError{Issues: result.Issues}
	}
	return result.Graph, nil
}

// CollectServiceNodes 收集当前配置中的启用服务节点。
func CollectServiceNodes(stacks []domain.Stack) map[ServiceNode]bool {
	nodes := make(map[ServiceNode]bool)
	for _, stack := range stacks {
		if !stack.Enabled {
			continue
		}
		if stack.Xray.Enabled {
			nodes[ServiceNode{Stack: stack.Name, Component: "xray"}] = true
		}
		if stack.Clash.Enabled {
			nodes[ServiceNode{Stack: stack.Name, Component: "clash"}] = true
		}
	}
	return nodes
}

// ValidateAcyclic 校验服务依赖图没有循环依赖。
func (r ReferenceGraph) ValidateAcyclic() []Issue {
	cycle := r.FindCycle()
	if len(cycle) == 0 {
		return nil
	}
	return []Issue{{
		Path:    "dependency_graph",
		Message: "dependency cycle detected: " + FormatCycle(cycle),
	}}
}

// FindCycle 使用深度优先搜索寻找一个依赖环。
func (r ReferenceGraph) FindCycle() []ServiceNode {
	states := map[ServiceNode]string{}
	path := make([]ServiceNode, 0, len(r.Nodes))
	for _, node := range sortedNodesFromSet(r.Nodes) {
		if states[node] == "done" {
			continue
		}
		if cycle := r.visitForCycle(node, states, &path); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

// TopologicalOrder 返回依赖优先的服务操作顺序。
func (r ReferenceGraph) TopologicalOrder(selectedNodes map[ServiceNode]bool) []ServiceNode {
	allowedNodes := selectedNodes
	if allowedNodes == nil {
		allowedNodes = r.Nodes
	}
	ordered := make([]ServiceNode, 0, len(allowedNodes))
	visited := map[ServiceNode]bool{}
	for _, node := range sortedNodesFromSet(allowedNodes) {
		r.appendTopological(node, allowedNodes, visited, &ordered)
	}
	return ordered
}

// BuildPlan 按目标 stack 计算依赖闭包和建议操作顺序。
func (r ReferenceGraph) BuildPlan(target string) (DependencyPlan, error) {
	targetNodes, err := r.SelectTargetNodes(target)
	if err != nil {
		return DependencyPlan{}, err
	}
	planNodes := r.CollectDependencyClosure(targetNodes)
	orderedNodes := r.TopologicalOrder(planNodes)
	dependencyNodes := make([]ServiceNode, 0)
	for _, node := range orderedNodes {
		if !targetNodes[node] {
			dependencyNodes = append(dependencyNodes, node)
		}
	}
	return DependencyPlan{
		Target:          target,
		DependencyNodes: dependencyNodes,
		DependencyEdges: r.CollectDependencyEdges(planNodes, orderedNodes),
		OperationOrder:  orderedNodes,
	}, nil
}

// SelectTargetNodes 选择依赖图目标节点；未指定目标时选择全部服务。
func (r ReferenceGraph) SelectTargetNodes(target string) (map[ServiceNode]bool, error) {
	if target == "" {
		return cloneNodeSet(r.Nodes), nil
	}
	if !r.StackNames[target] {
		return nil, fmt.Errorf("stack does not exist: %s", target)
	}
	targetNodes := make(map[ServiceNode]bool)
	for node := range r.Nodes {
		if node.Stack == target {
			targetNodes[node] = true
		}
	}
	return targetNodes, nil
}

// CollectDependencyClosure 收集目标服务和它们递归依赖的全部服务。
func (r ReferenceGraph) CollectDependencyClosure(targetNodes map[ServiceNode]bool) map[ServiceNode]bool {
	collected := make(map[ServiceNode]bool)
	pending := sortedNodesFromSet(targetNodes)
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if collected[node] {
			continue
		}
		collected[node] = true
		for dependency := range r.Dependencies[node] {
			pending = append(pending, dependency)
		}
	}
	return collected
}

// CollectDependencyEdges 收集目标范围内的服务依赖边。
func (r ReferenceGraph) CollectDependencyEdges(planNodes map[ServiceNode]bool, orderedNodes []ServiceNode) [][2]ServiceNode {
	edges := make([][2]ServiceNode, 0)
	for _, node := range orderedNodes {
		for _, dependency := range sortedNodesFromSet(r.Dependencies[node]) {
			if planNodes[dependency] {
				edges = append(edges, [2]ServiceNode{node, dependency})
			}
		}
	}
	return edges
}

// TargetScope 保存 CLI target 解析后的组件集合。
type TargetScope struct {
	Raw   string
	Nodes []ServiceNode
}

// ResolveTargetScope 解析空、stack、xray/name 和 clash/name 四种 target 形态。
func ResolveTargetScope(graph ReferenceGraph, target string) (TargetScope, error) {
	if target == "" {
		return TargetScope{Raw: target, Nodes: graph.TopologicalOrder(nil)}, nil
	}
	if strings.HasPrefix(target, "xray/") || strings.HasPrefix(target, "clash/") {
		parts := strings.SplitN(target, "/", 2)
		if !graph.StackNames[parts[1]] {
			return TargetScope{}, fmt.Errorf("stack does not exist: %s", parts[1])
		}
		node := ServiceNode{Stack: parts[1], Component: parts[0]}
		if !graph.Nodes[node] {
			return TargetScope{Raw: target, Nodes: nil}, nil
		}
		return TargetScope{Raw: target, Nodes: []ServiceNode{node}}, nil
	}
	nodes, err := graph.SelectTargetNodes(target)
	if err != nil {
		return TargetScope{}, err
	}
	return TargetScope{Raw: target, Nodes: graph.TopologicalOrder(nodes)}, nil
}

// FormatCycle 格式化依赖环路径，方便 CLI 直接展示。
func FormatCycle(cycle []ServiceNode) string {
	labels := make([]string, 0, len(cycle))
	for _, node := range cycle {
		labels = append(labels, node.ServiceName())
	}
	return strings.Join(labels, " -> ")
}

func addXrayDependencies(stack domain.Stack, index ReferenceIndex, dependencies map[ServiceNode]map[ServiceNode]bool) []Issue {
	if !stack.Enabled || !stack.Xray.Enabled || stack.Xray.Outbound.Type != "clash" {
		return nil
	}
	path := fmt.Sprintf("stacks.%s.xray.outbound.ref", stack.Name)
	parsed, err := ParseComponentRef(stack.Xray.Outbound.Ref, path)
	if err != nil {
		return []Issue{{Path: path, Message: errMessage(err)}}
	}
	if parsed.Component != "clash" {
		return []Issue{{Path: path, Message: "xray clash outbound ref must target clash component"}}
	}
	if parsed.Kind != "socks" {
		return []Issue{{Path: path, Message: "xray clash outbound ref must target socks listener"}}
	}
	endpoint, ok := index.ResolveClashListener(parsed.Raw)
	if !ok {
		return []Issue{{Path: path, Message: "clash listener ref does not exist: " + parsed.Raw}}
	}
	sourceNode := ServiceNode{Stack: stack.Name, Component: "xray"}
	targetNode := ServiceNode{Stack: endpoint.Stack, Component: "clash"}
	ensureDependencyNode(dependencies, sourceNode)
	ensureDependencyNode(dependencies, targetNode)
	dependencies[sourceNode][targetNode] = true
	return nil
}

func addClashDependencies(stack domain.Stack, index ReferenceIndex, dependencies map[ServiceNode]map[ServiceNode]bool) []Issue {
	if !stack.Enabled || !stack.Clash.Enabled {
		return nil
	}
	issues := make([]Issue, 0)
	sourceNode := ServiceNode{Stack: stack.Name, Component: "clash"}
	for upstreamIndex, upstream := range stack.Clash.Upstreams {
		if upstream.Type != "xray-socks5" {
			continue
		}
		path := fmt.Sprintf("stacks.%s.clash.upstreams[%d].ref", stack.Name, upstreamIndex)
		parsed, err := ParseXrayInboundRef(upstream.Ref, path)
		if err != nil {
			issues = append(issues, Issue{Path: path, Message: errMessage(err)})
			continue
		}
		endpoint, ok := index.ResolveXrayInbound(parsed.Raw)
		if !ok {
			issues = append(issues, Issue{Path: path, Message: "xray inbound ref does not exist: " + parsed.Raw})
			continue
		}
		if endpoint.Kind != "socks5" {
			issues = append(issues, Issue{Path: path, Message: fmt.Sprintf("xray-socks5 ref must target socks5 inbound, got %s: %s", endpoint.Kind, parsed.Raw)})
			continue
		}
		targetNode := ServiceNode{Stack: endpoint.Stack, Component: "xray"}
		ensureDependencyNode(dependencies, sourceNode)
		ensureDependencyNode(dependencies, targetNode)
		dependencies[sourceNode][targetNode] = true
	}
	return issues
}

func (r ReferenceGraph) visitForCycle(node ServiceNode, states map[ServiceNode]string, path *[]ServiceNode) []ServiceNode {
	states[node] = "visiting"
	*path = append(*path, node)
	for _, dependency := range sortedNodesFromSet(r.Dependencies[node]) {
		if states[dependency] == "visiting" {
			for index, item := range *path {
				if item == dependency {
					return append(append([]ServiceNode{}, (*path)[index:]...), dependency)
				}
			}
		}
		if states[dependency] == "done" {
			continue
		}
		if cycle := r.visitForCycle(dependency, states, path); len(cycle) > 0 {
			return cycle
		}
	}
	*path = (*path)[:len(*path)-1]
	states[node] = "done"
	return nil
}

func (r ReferenceGraph) appendTopological(node ServiceNode, allowedNodes map[ServiceNode]bool, visited map[ServiceNode]bool, ordered *[]ServiceNode) {
	if visited[node] {
		return
	}
	for _, dependency := range sortedNodesFromSet(r.Dependencies[node]) {
		if allowedNodes[dependency] {
			r.appendTopological(dependency, allowedNodes, visited, ordered)
		}
	}
	visited[node] = true
	*ordered = append(*ordered, node)
}

func ensureDependencyNode(dependencies map[ServiceNode]map[ServiceNode]bool, node ServiceNode) {
	if dependencies[node] == nil {
		dependencies[node] = map[ServiceNode]bool{}
	}
}

func sortedNodesFromSet(nodes map[ServiceNode]bool) []ServiceNode {
	sorted := make([]ServiceNode, 0, len(nodes))
	for node, ok := range nodes {
		if ok {
			sorted = append(sorted, node)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Stack != sorted[j].Stack {
			return sorted[i].Stack < sorted[j].Stack
		}
		return sorted[i].Component < sorted[j].Component
	})
	return sorted
}

func cloneNodeSet(nodes map[ServiceNode]bool) map[ServiceNode]bool {
	clone := make(map[ServiceNode]bool, len(nodes))
	for node, ok := range nodes {
		clone[node] = ok
	}
	return clone
}

func errMessage(err error) string {
	if refErr, ok := err.(RefFormatError); ok {
		return refErr.Message
	}
	return err.Error()
}
