package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eagle/proxystack-go/internal/agentconfig"
	"github.com/eagle/proxystack-go/internal/domain"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	"github.com/eagle/proxystack-go/internal/userstate"
	"github.com/stretchr/testify/require"
)

const secondUserUUID = "22222222-2222-4222-8222-222222222222"

// fakeXrayAPI 记录热应用调用，并按需返回失败。
type fakeXrayAPI struct {
	removed []string
	added   []string
	err     error
}

func (f *fakeXrayAPI) RemoveUser(_ context.Context, inboundTag string, email string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, inboundTag+"/"+email)
	return nil
}

func (f *fakeXrayAPI) AddUser(_ context.Context, _ string, inboundPatch string) error {
	if f.err != nil {
		return f.err
	}
	f.added = append(f.added, inboundPatch)
	return nil
}

// TestAgentUserListShowsTogglableUsers 验证 list 只列出 vmess/shadowsocks 上的用户并标注状态。
func TestAgentUserListShowsTogglableUsers(t *testing.T) {
	baseDir := newTwoUserProject(t)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "list")

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	require.Contains(t, lines[0], "STACK")
	require.Contains(t, lines[0], "INBOUND")
	require.Contains(t, lines[0], "STATUS")
	require.Contains(t, output, "user1")
	require.Contains(t, output, "user2")
	require.Contains(t, output, "enabled")
	require.NotContains(t, output, "disabled")
	// socks5 是单账号 inbound，不参与启停，不应出现在表里。
	require.NotContains(t, output, "socks5:")
}

// TestAgentUserDisableWritesStateAndRegeneratesConfig 验证禁用会落盘状态并把用户从生成配置里去掉。
func TestAgentUserDisableWritesStateAndRegeneratesConfig(t *testing.T) {
	baseDir := newTwoUserProject(t)
	fake := useFakeXrayAPI(t, nil)
	useFixedToggleClock(t, "2026-08-27T12:00:00+08:00")

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	statePath := filepath.Join(baseDir, "runtime", "disabled.json")
	require.Contains(t, output, "Marked user2 as disabled in "+statePath)
	require.Contains(t, output, "Applied live: usa1/vmess:24100:vmess user2")
	require.Contains(t, output, "existing connections stay up")
	require.Equal(t, []string{"vmess:24100:vmess/user2"}, fake.removed)

	state, err := userstate.Load(statePath)
	require.NoError(t, err)
	require.Equal(t, []userstate.Entry{{User: "user2", Stack: "usa1", Since: "2026-08-27T12:00:00+08:00"}}, state.Disabled)

	generated := readGeneratedXray(t, baseDir, "usa1")
	require.NotContains(t, generated, secondUserUUID)
	require.NotContains(t, generated, "user2")
	require.Contains(t, generated, "user1")

	listOutput := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "list")
	require.Regexp(t, `user2\s+default\s+disabled \(since 2026-08-27T12:00:00\+08:00\)`, listOutput)
	require.Regexp(t, `user1\s+default\s+enabled`, listOutput)
}

// TestAgentUserEnableRestoresUser 验证启用会移除状态、写回生成配置并热添加回去。
func TestAgentUserEnableRestoresUser(t *testing.T) {
	baseDir := newTwoUserProject(t)
	useFakeXrayAPI(t, nil)
	runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")
	fake := useFakeXrayAPI(t, nil)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "enable", "user2")

	require.Contains(t, output, "Marked user2 as enabled")
	require.NotContains(t, output, "existing connections stay up")
	require.Len(t, fake.added, 1)
	require.Contains(t, fake.added[0], secondUserUUID)
	state, err := userstate.Load(filepath.Join(baseDir, "runtime", "disabled.json"))
	require.NoError(t, err)
	require.Empty(t, state.Disabled)
	require.Contains(t, readGeneratedXray(t, baseDir, "usa1"), secondUserUUID)
}

// TestAgentUserLiveApplyFailureKeepsStateAndPrintsRunnableCommand 验证热应用失败时命令仍成功，且提示的是合法命令。
//
// 生命周期命令只接受一个 TARGET，所以多 stack 时必须逐条给出而不是拼成一行。
func TestAgentUserLiveApplyFailureKeepsStateAndPrintsRunnableCommand(t *testing.T) {
	baseDir := newTwoUserProject(t)
	useFakeXrayAPI(t, fmt.Errorf("connection refused"))

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	require.Contains(t, output, "live apply failed: connection refused")
	require.Contains(t, output, "state saved but not live on: usa1")
	require.Contains(t, output, "run: psctl restart xray/usa1")
	require.NotContains(t, output, "existing connections stay up")
	state, err := userstate.Load(filepath.Join(baseDir, "runtime", "disabled.json"))
	require.NoError(t, err)
	require.Len(t, state.Disabled, 1)
	require.NotContains(t, readGeneratedXray(t, baseDir, "usa1"), secondUserUUID)
}

// TestAgentUserToggleIsIdempotent 验证重复禁用不会重复写盘。
func TestAgentUserToggleIsIdempotent(t *testing.T) {
	baseDir := newTwoUserProject(t)
	useFakeXrayAPI(t, nil)
	runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	require.Contains(t, output, "No change: user2 is already disabled")
}

// TestAgentUserDisableRejectsLastEnabledUser 验证其他用户已被禁用时的提示指向恢复路径。
func TestAgentUserDisableRejectsLastEnabledUser(t *testing.T) {
	baseDir := newTwoUserProject(t)
	useFakeXrayAPI(t, nil)
	runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "user1")

	require.Error(t, err)
	require.Contains(t, err.Error(), "user1 is the last enabled user of usa1/vmess:24100:vmess; enable one of user2 first, or disable the inbound in the stack file")
	state, stateErr := userstate.Load(filepath.Join(baseDir, "runtime", "disabled.json"))
	require.NoError(t, stateErr)
	require.Len(t, state.Disabled, 1)
}

// TestAgentUserDisableRejectsOnlyUser 验证 inbound 本来就只有一个用户时的提示。
//
// 这时说“最后一个启用的用户”会让运维去找并不存在的其他用户，必须说“唯一用户”。
func TestAgentUserDisableRejectsOnlyUser(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "user1")

	require.Error(t, err)
	require.Contains(t, err.Error(), "user1 is the only user of usa1/vmess:24100:vmess; disable the inbound in the stack file instead")
	require.NotContains(t, err.Error(), "last enabled")
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "disabled.json"))
}

// TestAgentUserDisableRejectsUnknownUser 验证不存在的用户会报错而不是静默写状态。
func TestAgentUserDisableRejectsUnknownUser(t *testing.T) {
	baseDir := newTwoUserProject(t)

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "nobody")

	require.Error(t, err)
	require.Contains(t, err.Error(), "user does not exist in any vmess or shadowsocks inbound: nobody")
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "disabled.json"))
}

// TestAgentUserRejectsClashTarget 验证 clash/NAME 会被明确拒绝而不是报“用户不存在”。
func TestAgentUserRejectsClashTarget(t *testing.T) {
	baseDir := newTwoUserProject(t)

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "user2", "clash/usa1")

	require.Error(t, err)
	require.Contains(t, err.Error(), "user toggling only applies to xray: clash/usa1")
}

// TestAgentUserScopesToTargetStack 验证 TARGET 只影响指定 stack。
func TestAgentUserScopesToTargetStack(t *testing.T) {
	baseDir := newTwoUserProject(t)
	useFakeXrayAPI(t, nil)
	require.NoError(t, agentconfig.CloneStack(agentconfig.CloneOptions{
		ConfigPath:    filepath.Join(baseDir, "config.yaml"),
		Source:        "usa1",
		Target:        "usa2",
		AllocatePorts: true,
	}))

	runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2", "usa1")

	state, err := userstate.Load(filepath.Join(baseDir, "runtime", "disabled.json"))
	require.NoError(t, err)
	require.Len(t, state.Disabled, 1)
	require.Equal(t, "usa1", state.Disabled[0].Stack)
	require.NotContains(t, readGeneratedXray(t, baseDir, "usa1"), secondUserUUID)
	// usa2 不在 target 范围内，既没被重新生成，渲染出来也仍然带着 user2。
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "generated", "xray", "usa2.json"))
	require.Contains(t, runAgentCommandForTest(t, "--base-dir", baseDir, "render", "xray", "usa2"), secondUserUUID)
}

// TestAgentUserDisableRefusesChangesBeyondToggle 验证还有未重启的其它改动时什么都不写。
//
// 状态必须和生成文件一起留在原样，否则会留下“已标记禁用但配置和运行中实例都没变”
// 的半应用状态，而重跑会被幂等判断挡掉。
func TestAgentUserDisableRefusesChangesBeyondToggle(t *testing.T) {
	baseDir := newTwoUserProject(t)
	seedGeneratedConfig(t, baseDir)
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"), "port: 24100", "port: 24180")

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "user2")

	require.Error(t, err)
	require.Contains(t, err.Error(), "changes beyond this user toggle")
	require.Contains(t, err.Error(), "nothing was written; run psctl restart xray/usa1")
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "disabled.json"))
	require.Contains(t, readGeneratedXray(t, baseDir, "usa1"), "24100")
}

// TestAgentUserDisableRefusesUnrelatedPendingUserRemoval 验证别人待吊销的用户不会被顺手写盘。
//
// 生成结果的差异全在 clients 里，但多出来的那条删除不会被热应用，写盘就会让运行中
// 实例和磁盘分叉，而且此后漂移检测再也看不出来。
func TestAgentUserDisableRefusesUnrelatedPendingUserRemoval(t *testing.T) {
	baseDir := newTwoUserProject(t)
	addThirdUser(t, baseDir)
	seedGeneratedConfig(t, baseDir)
	// 模拟运维吊销 user3：改了 stack 文件但还没重启。
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"),
		"        - user: user3\n          profile: default\n          remark: usa1 vmess user3\n", "")

	_, err := runAgentCommandForTestError("--base-dir", baseDir, "user", "disable", "user2")

	require.Error(t, err)
	require.Contains(t, err.Error(), "changes beyond this user toggle")
	require.NoFileExists(t, filepath.Join(baseDir, "runtime", "disabled.json"))
	require.Contains(t, readGeneratedXray(t, baseDir, "usa1"), "user3")
}

// TestAgentUserToggleCoversEveryInboundInStack 验证同一 stack 下多个 inbound 都会被启停覆盖。
func TestAgentUserToggleCoversEveryInboundInStack(t *testing.T) {
	baseDir := newTwoUserProject(t)
	addSecondVmessInbound(t, baseDir)
	fake := useFakeXrayAPI(t, nil)

	runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	require.Equal(t, []string{"vmess:24100:vmess/user2", "vmess:24101:vmess2/user2"}, fake.removed)
	generated := readGeneratedXray(t, baseDir, "usa1")
	require.NotContains(t, generated, secondUserUUID)
	require.Equal(t, 2, strings.Count(generated, `"protocol": "vmess"`))
}

// TestAgentUserDisableWarnsAboutShadowedEntries 验证同 scope 内不能启停的入口会被明确提示。
func TestAgentUserDisableWarnsAboutShadowedEntries(t *testing.T) {
	baseDir := newTwoUserProject(t)
	addUserToSocksInbound(t, baseDir)
	useFakeXrayAPI(t, nil)

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "disable", "user2")

	require.Contains(t, output, "user2 still has access through entries that cannot be toggled")
	require.Contains(t, output, "(socks5 inbound, shared account)")
	require.Contains(t, output, "edit the stack file to remove them")
}

// TestAgentUserEnableClearsStaleState 验证配置里已不存在的陈旧禁用条目可以用 enable 清掉。
func TestAgentUserEnableClearsStaleState(t *testing.T) {
	baseDir := newTwoUserProject(t)
	statePath := filepath.Join(baseDir, "runtime", "disabled.json")
	state := userstate.State{}
	state.Disable("usa1", "ghost", "2026-08-27T12:00:00+08:00")
	require.NoError(t, userstate.Save(statePath, state))

	listOutput := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "list")
	require.Contains(t, listOutput, "no longer match any configured user")
	require.Contains(t, listOutput, "usa1/ghost")

	output := runAgentCommandForTest(t, "--base-dir", baseDir, "user", "enable", "ghost")

	require.Contains(t, output, "Cleared stale disabled state for ghost on: usa1")
	cleared, err := userstate.Load(statePath)
	require.NoError(t, err)
	require.Empty(t, cleared.Disabled)
}

// TestAgentDoctorStatesHandlerServiceTradeoff 验证 doctor 陈述 HandlerService 的取舍但不因此判失败。
//
// 开与不开都是合法配置，默认配置不能让 doctor 以非零退出。
func TestAgentDoctorStatesHandlerServiceTradeoff(t *testing.T) {
	baseDir := newTwoUserProject(t)

	output, _ := runAgentCommandForTestError("--base-dir", baseDir, "doctor")

	require.Contains(t, output, "OK xray HandlerService enabled on usa1 127.0.0.1:10085")
	require.Contains(t, output, "the API has no authentication")
	require.NotContains(t, output, "ISSUE xray HandlerService")

	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"),
		"services: [HandlerService, StatsService]", "services: [StatsService]")

	output, _ = runAgentCommandForTestError("--base-dir", baseDir, "doctor")

	require.Contains(t, output, "OK xray HandlerService not enabled on usa1")
	require.Contains(t, output, "needs psctl restart to take effect")
}

// TestAgentDoctorReportsStaleDisabledState 验证陈旧禁用条目是真正的 issue。
func TestAgentDoctorReportsStaleDisabledState(t *testing.T) {
	baseDir := newTwoUserProject(t)
	state := userstate.State{}
	state.Disable("usa1", "ghost", "2026-08-27T12:00:00+08:00")
	require.NoError(t, userstate.Save(filepath.Join(baseDir, "runtime", "disabled.json"), state))

	output, err := runAgentCommandForTestError("--base-dir", baseDir, "doctor")

	require.Error(t, err, "doctor 有 issue 时应以非零退出")
	require.Contains(t, output, "ISSUE disabled user state references users that no longer exist")
	require.Contains(t, output, "usa1/ghost")
}

// useFakeXrayAPI 把热应用替换为 fake，并在测试结束后还原。
func useFakeXrayAPI(t *testing.T, err error) *fakeXrayAPI {
	t.Helper()
	fake := &fakeXrayAPI{err: err}
	original := newXrayAPIClient
	newXrayAPIClient = func(domain.GlobalConfig, string) xrayUserAPI { return fake }
	t.Cleanup(func() { newXrayAPIClient = original })
	return fake
}

// useFixedToggleClock 固定禁用时间戳，便于断言输出。
func useFixedToggleClock(t *testing.T, value string) {
	t.Helper()
	stamp, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	original := userToggleNow
	userToggleNow = func() time.Time { return stamp }
	t.Cleanup(func() { userToggleNow = original })
}

// seedGeneratedConfig 先生成一次运行时文件，模拟已经 start 过的机器。
func seedGeneratedConfig(t *testing.T, baseDir string) {
	t.Helper()
	plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{
		ConfigPath:      filepath.Join(baseDir, "config.yaml"),
		SkipSystemPorts: true,
	})
	require.NoError(t, err)
	require.NoError(t, agentruntime.ApplyPlan(plan))
}

// newTwoUserProject 初始化一个 vmess inbound 上带两个用户的示例项目。
//
// 模板默认只有 user1，而禁用最后一个用户是被拒绝的，所以这里补一个 user2。
func newTwoUserProject(t *testing.T) string {
	t.Helper()
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	require.NoError(t, agentconfig.InitProject(agentconfig.InitOptions{BaseDir: baseDir, ExternalHost: "proxy.example.com"}))
	require.NoError(t, agentconfig.AddStack(agentconfig.AddOptions{ConfigPath: configPath, Name: "usa1", Template: "pair", KeepTemplatePorts: true}))
	addConfigUser(t, configPath, "user2", secondUserUUID)
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"), "          remark: usa1 vmess\n",
		"          remark: usa1 vmess\n"+
			"        - user: user2\n"+
			"          profile: default\n"+
			"          remark: usa1 vmess user2\n")
	return baseDir
}

// addThirdUser 再加一个只挂在 vmess inbound 上的用户。
func addThirdUser(t *testing.T, baseDir string) {
	t.Helper()
	addConfigUser(t, filepath.Join(baseDir, "config.yaml"), "user3", "33333333-3333-4333-8333-333333333333")
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"), "          remark: usa1 vmess user2\n",
		"          remark: usa1 vmess user2\n"+
			"        - user: user3\n"+
			"          profile: default\n"+
			"          remark: usa1 vmess user3\n")
}

// addSecondVmessInbound 给 usa1 追加第二个同样带两个用户的 vmess inbound。
func addSecondVmessInbound(t *testing.T, baseDir string) {
	t.Helper()
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"), "\n  # 【一般不改】xray 出站配置",
		"\n    - name: vmess2\n"+
			"      protocol: vmess\n"+
			"      listen: 0.0.0.0\n"+
			"      port: 24101\n"+
			"      network: raw\n"+
			"      sub: true\n"+
			"      user_refs:\n"+
			"        - user: user1\n"+
			"          profile: default\n"+
			"          remark: usa1 vmess2 user1\n"+
			"        - user: user2\n"+
			"          profile: default\n"+
			"          remark: usa1 vmess2 user2\n"+
			"\n  # 【一般不改】xray 出站配置")
}

// addUserToSocksInbound 把 user2 也挂到 socks5 inbound 上，用于验证跨协议提示。
func addUserToSocksInbound(t *testing.T, baseDir string) {
	t.Helper()
	replaceInFile(t, filepath.Join(baseDir, "stacks", "usa1.yaml"), "          remark: usa1 local socks\n",
		"          remark: usa1 local socks\n"+
			"        - user: user2\n"+
			"          profile: default\n"+
			"          remark: usa1 local socks user2\n")
}

// addConfigUser 往 config.yaml 的全局用户档案里追加一个用户。
func addConfigUser(t *testing.T, configPath string, user string, uuid string) {
	t.Helper()
	replaceInFile(t, configPath, "\nport_ranges:", "\n  - user: "+user+"\n"+
		"    profile: default\n"+
		"    uuid: "+uuid+"\n"+
		"    password: change-me-"+user+"-password\n"+
		"    remark: "+user+"\n"+
		"\nport_ranges:")
}

// readGeneratedXray 读取某个 stack 生成的 Xray 配置文本。
func readGeneratedXray(t *testing.T, baseDir string, stack string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(baseDir, "runtime", "generated", "xray", stack+".json"))
	require.NoError(t, err)
	return string(data)
}

// replaceInFile 就地替换文件中的首个匹配片段。
func replaceInFile(t *testing.T, path string, old string, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, old, "replace target not found in %s", path)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(content, old, new, 1)), info.Mode().Perm()))
}
