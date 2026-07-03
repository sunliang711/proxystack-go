package graph_test

import (
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/graph"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestReferenceGraphIndexesExamples 验证示例项目能建立 endpoint 索引。
func TestReferenceGraphIndexesExamples(t *testing.T) {
	referenceGraph := loadExampleGraph(t)

	inbound, ok := referenceGraph.Index.ResolveXrayInbound("usa1.relay")
	require.True(t, ok)
	require.Equal(t, "socks5", inbound.Kind)
	require.Equal(t, 24001, inbound.Port)

	listener, ok := referenceGraph.Index.ResolveClashListener("usa1.clash.socks")
	require.True(t, ok)
	require.Equal(t, "socks", listener.Kind)
	require.Equal(t, 17091, listener.Port)
}

// TestReferenceGraphOrdersDependenciesBeforeConsumers 验证依赖服务排在消费者之前。
func TestReferenceGraphOrdersDependenciesBeforeConsumers(t *testing.T) {
	referenceGraph := loadExampleGraph(t)

	order := referenceGraph.TopologicalOrder(nil)

	require.Less(t, indexOf(order, graph.ServiceNode{Stack: "usa1", Component: "xray"}), indexOf(order, graph.ServiceNode{Stack: "auto", Component: "clash"}))
	require.Less(t, indexOf(order, graph.ServiceNode{Stack: "usa2", Component: "xray"}), indexOf(order, graph.ServiceNode{Stack: "auto", Component: "clash"}))
}

// TestTargetScopeParsesComponentTarget 验证 target scope 支持组件级目标。
func TestTargetScopeParsesComponentTarget(t *testing.T) {
	referenceGraph := loadExampleGraph(t)

	scope, err := graph.ResolveTargetScope(referenceGraph, "xray/usa1")

	require.NoError(t, err)
	require.Equal(t, []graph.ServiceNode{{Stack: "usa1", Component: "xray"}}, scope.Nodes)
}

// TestBuildPlanUsesEmptyTargetForAllServices 验证空 target 表示全部服务。
func TestBuildPlanUsesEmptyTargetForAllServices(t *testing.T) {
	referenceGraph := loadExampleGraph(t)

	plan, err := referenceGraph.BuildPlan("")

	require.NoError(t, err)
	require.NotEmpty(t, plan.OperationOrder)
}

// TestReferenceGraphReportsMissingRef 验证缺失 ref 会进入图编译问题列表。
func TestReferenceGraphReportsMissingRef(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[0].Clash.Upstreams[0].Ref = "missing.relay"

	result := graph.CompileReferenceGraph(stackSet)

	require.NotEmpty(t, result.Issues)
	require.Contains(t, result.Issues[0].Message, "xray inbound ref does not exist")
}

// TestReferenceGraphReportsProtocolMismatch 验证 xray-socks5 ref 指向非 socks5 inbound 会失败。
func TestReferenceGraphReportsProtocolMismatch(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[0].Clash.Upstreams[0].Ref = "usa1.vmess"

	result := graph.CompileReferenceGraph(stackSet)

	require.NotEmpty(t, result.Issues)
	require.Contains(t, result.Issues[0].Message, "must target socks5 inbound")
}

// TestReferenceGraphReportsCycle 验证服务依赖环会失败。
func TestReferenceGraphReportsCycle(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[1].Clash.Upstreams = append(stackSet.Stacks[1].Clash.Upstreams, domain.ClashUpstream{
		Name: "auto-relay",
		Type: "xray-socks5",
		Ref:  "auto.relay",
	})

	result := graph.CompileReferenceGraph(stackSet)

	require.NotEmpty(t, result.Issues)
	require.Contains(t, result.Issues[0].Message, "dependency cycle detected")
}

func loadExampleGraph(t *testing.T) graph.ReferenceGraph {
	t.Helper()
	stackSet := loadExampleStackSet(t)
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	require.NoError(t, err)
	return referenceGraph
}

func loadExampleStackSet(t *testing.T) domain.StackSet {
	t.Helper()
	testutil.ChdirRepo(t)
	globalConfig, err := config.LoadConfig("tests/fixtures/example-project/config.yaml")
	require.NoError(t, err)
	stackSet, err := config.LoadStacks(globalConfig, false)
	require.NoError(t, err)
	return stackSet
}

func indexOf(nodes []graph.ServiceNode, target graph.ServiceNode) int {
	for index, node := range nodes {
		if node == target {
			return index
		}
	}
	return -1
}
