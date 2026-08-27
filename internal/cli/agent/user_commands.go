package agent

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eagle/proxystack-go/internal/domain"
	xraygen "github.com/eagle/proxystack-go/internal/generator/xray"
	"github.com/eagle/proxystack-go/internal/graph"
	agentruntime "github.com/eagle/proxystack-go/internal/runtime"
	"github.com/eagle/proxystack-go/internal/userstate"
	"github.com/eagle/proxystack-go/internal/xrayapi"
	"github.com/spf13/cobra"
)

// userToggleNow 提供禁用时间戳，测试可替换为固定时钟。
var userToggleNow = time.Now

// newXrayAPIClient 构造热应用使用的 Xray API 客户端，测试可替换为 fake。
var newXrayAPIClient = func(config domain.GlobalConfig, server string) xrayUserAPI {
	return xrayapi.NewClient(config, server)
}

// xrayUserAPI 是热应用用户启停所需的最小接口，便于测试注入 fake。
type xrayUserAPI interface {
	RemoveUser(ctx context.Context, inboundTag string, email string) error
	AddUser(ctx context.Context, dir string, inboundPatch string) error
}

// userSite 表示某个 stack 的某个 inbound 上的一个用户。
type userSite struct {
	Stack    string
	Tag      string
	User     string
	Profile  string
	Email    string
	Inbound  domain.Inbound
	Endpoint xrayapi.APIEndpoint
	Disabled bool
	Since    string
}

// userScope 是一次 USER/TARGET 解析的完整结果。
type userScope struct {
	Sites    []userSite
	StackSet domain.StackSet
	State    userstate.State
	// Shadowed 是同 scope 内引用了该用户、但不参与启停的入口描述。
	Shadowed []string
	// XrayStacks 记录 scope 内实际存在的 Xray stack，用于区分“目标不含 Xray”和“用户不存在”。
	XrayStacks []string
}

// newUserCommand 创建用户临时启停命令集合。
func newUserCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "user",
		Short: "Temporarily disable or enable configured users",
		Long:  userCommandLong(),
	}
	command.AddCommand(newUserListCommand())
	command.AddCommand(newUserDisableCommand())
	command.AddCommand(newUserEnableCommand())
	return command
}

// userCommandLong 描述用户启停的适用范围和 TARGET 规则。
func userCommandLong() string {
	return "Temporarily disable or enable configured users without restarting the service.\n\n" +
		"State is recorded in runtime/disabled.json and applied to the running Xray\n" +
		"instance through HandlerService. Adding, removing and editing users is still\n" +
		"done by editing config.yaml and stacks/*.yaml.\n\n" +
		"Scope:\n" +
		"  Only vmess and shadowsocks inbounds can be toggled. socks5 and http inbounds\n" +
		"  carry a single shared account, so removing it would turn them into open\n" +
		"  proxies. Clash listeners and subscription output are never affected.\n" +
		"  State is keyed by (stack, user), so all profiles of one user toggle together.\n\n" +
		userTargetLong()
}

// userTargetLong 复述 TARGET 取值，供各子命令 help 使用。
func userTargetLong() string {
	return "TARGET:\n" +
		"  omitted     every stack\n" +
		"  NAME        one stack\n" +
		"  xray/NAME   the Xray service of one stack\n" +
		"  clash/NAME  rejected: user toggling only applies to Xray"
}

// newUserListCommand 创建 user list 子命令。
func newUserListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [TARGET]",
		Short: "List configured users and their enabled state",
		Long:  "List every togglable user in TARGET with its current state.\n\n" + userTargetLong(),
		Example: "  psctl user list\n" +
			"  psctl user list usa1\n" +
			"  psctl user list xray/usa1",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			target := optionalArg(args)
			scope, err := resolveUserScope(command, "", target)
			if err != nil {
				return err
			}
			printUserSites(command.OutOrStdout(), scope.Sites)
			if target == "" {
				printStaleEntries(command.ErrOrStderr(), scope)
			}
			return nil
		},
	}
}

// newUserDisableCommand 创建 user disable 子命令。
func newUserDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable USER [TARGET]",
		Short: "Disable a user without restarting the service",
		Long:  "Disable USER in TARGET and apply it to the running Xray instance.\n\n" + userTargetLong(),
		Example: "  psctl user disable bob\n" +
			"  psctl user disable bob usa1\n" +
			"  psctl user disable bob xray/usa1",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			return runUserToggle(command, args[0], optionalArg(args[1:]), true)
		},
	}
}

// newUserEnableCommand 创建 user enable 子命令。
func newUserEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable USER [TARGET]",
		Short: "Re-enable a previously disabled user",
		Long:  "Re-enable USER in TARGET and apply it to the running Xray instance.\n\n" + userTargetLong(),
		Example: "  psctl user enable bob\n" +
			"  psctl user enable bob usa1\n" +
			"  psctl user enable bob xray/usa1",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			return runUserToggle(command, args[0], optionalArg(args[1:]), false)
		},
	}
}

// runUserToggle 执行禁用或启用。
//
// 顺序是先在内存里算出目标状态、按它预演 plan 并校验，全部通过之后才落盘、写生成
// 文件、热应用。反过来先落盘的话，一旦校验失败就会留下“状态已禁用但配置和运行中
// 实例都没变”的半应用状态，而重跑会被幂等判断挡掉，只能靠 restart 收敛。
func runUserToggle(command *cobra.Command, user string, target string, disable bool) error {
	scope, err := resolveUserScope(command, user, target)
	if err != nil {
		return err
	}
	if len(scope.Sites) == 0 {
		if !disable {
			return clearStaleUserState(command, scope, user, target)
		}
		return noSuchUserError(scope, user, target)
	}
	if disable {
		if err := ensureInboundKeepsUser(scope, user); err != nil {
			return err
		}
	}
	state := scope.State
	since := userToggleNow().Local().Format(time.RFC3339)
	changedStacks := map[string]bool{}
	for _, site := range scope.Sites {
		var updated bool
		if disable {
			updated = state.Disable(site.Stack, site.User, since)
		} else {
			updated = state.Enable(site.Stack, site.User)
		}
		if updated {
			changedStacks[site.Stack] = true
		}
	}
	output := command.OutOrStdout()
	if len(changedStacks) == 0 {
		fmt.Fprintf(output, "No change: %s is already %s%s\n", user, stateWord(disable), targetSuffix(target))
		return nil
	}
	// 禁用状态按 (stack, user) 记录，但热应用要落到该 stack 下的每个 inbound。
	changed := make([]userSite, 0, len(scope.Sites))
	expected := agentruntime.NewUserChange()
	for _, site := range scope.Sites {
		if !changedStacks[site.Stack] {
			continue
		}
		changed = append(changed, site)
		expected.Add(disable, site.Stack, site.Tag, site.Email)
	}

	configPath, err := agentConfigPath(command)
	if err != nil {
		return err
	}
	plans, err := planUserChange(configPath, state, changedStacks, expected)
	if err != nil {
		return err
	}
	statePath := userstate.Path(scope.StackSet.Config)
	if err := userstate.Save(statePath, state); err != nil {
		return err
	}
	for _, plan := range plans {
		if err := agentruntime.ApplyPlan(plan); err != nil {
			return err
		}
	}
	fmt.Fprintf(output, "Marked %s as %s in %s\n", user, stateWord(disable), statePath)
	applyUserChangeLive(command, scope.StackSet.Config, changed, disable)
	printShadowedEntries(command.ErrOrStderr(), scope, user, disable)
	return nil
}

// planUserChange 为每个受影响 stack 单独预演 plan，并要求差异恰好等于本次启停。
//
// 逐 stack 而不是整个 scope 一次性 plan，是为了让某个无关 stack 的未重启改动不至于
// 挡住其它 stack 的启停。全部校验通过后才由调用方统一写入。
func planUserChange(configPath string, state userstate.State, stacks map[string]bool, expected agentruntime.UserChange) ([]agentruntime.Plan, error) {
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)
	loadState := func(domain.GlobalConfig) (domain.DisabledUserSet, error) { return state.UserSet(), nil }
	plans := make([]agentruntime.Plan, 0, len(names))
	for _, name := range names {
		plan, err := agentruntime.BuildPlan(agentruntime.BuildOptions{
			ConfigPath:      configPath,
			Target:          "xray/" + name,
			SkipSystemPorts: true,
			DisabledUsers:   loadState,
		})
		if err != nil {
			return nil, err
		}
		usersOnly, path, err := agentruntime.UsersOnlyPlan(plan, expected)
		if err != nil {
			return nil, err
		}
		if !usersOnly {
			return nil, fmt.Errorf("generated config has changes beyond this user toggle: %s\n"+
				"nothing was written; run psctl restart xray/%s to apply the pending changes first", path, name)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// applyUserChangeLive 把变更热应用到运行中的实例；失败只告警，因为状态已经落盘。
func applyUserChangeLive(command *cobra.Command, config domain.GlobalConfig, sites []userSite, disable bool) {
	output := command.OutOrStdout()
	errOutput := command.ErrOrStderr()
	ctx := context.Background()
	pending := map[string]bool{}
	applied := 0
	for _, site := range sites {
		if !site.Endpoint.Ready() {
			fmt.Fprintf(errOutput, "warning: %s/%s cannot apply live (%s)\n", site.Stack, site.Tag, site.Endpoint.Reason())
			pending[site.Stack] = true
			continue
		}
		client := newXrayAPIClient(config, site.Endpoint.Server)
		var err error
		if disable {
			err = client.RemoveUser(ctx, site.Tag, site.Email)
		} else {
			patch, patchErr := xraygen.DumpsInboundUserPatch(site.Inbound, inboundUserByName(site.Inbound, site.User))
			if patchErr != nil {
				err = patchErr
			} else {
				err = client.AddUser(ctx, config.ResolvePath(config.Paths.Runtime), patch)
			}
		}
		if err != nil {
			fmt.Fprintf(errOutput, "warning: %s/%s live apply failed: %v\n", site.Stack, site.Tag, err)
			pending[site.Stack] = true
			continue
		}
		applied++
		fmt.Fprintf(output, "Applied live: %s/%s %s\n", site.Stack, site.Tag, site.Email)
	}
	if len(pending) > 0 {
		stacks := make([]string, 0, len(pending))
		for stack := range pending {
			stacks = append(stacks, stack)
		}
		sort.Strings(stacks)
		fmt.Fprintf(errOutput, "state saved but not live on: %s\n", strings.Join(stacks, ", "))
		// 生命周期命令一次只接受一个 TARGET，所以逐条给出可直接执行的命令。
		for _, stack := range stacks {
			fmt.Fprintf(errOutput, "  run: psctl restart xray/%s\n", stack)
		}
	}
	if disable && applied > 0 {
		fmt.Fprintln(output, "Note: existing connections stay up until they close on their own.")
	}
}

// printShadowedEntries 提示该用户还有哪些不参与启停的入口仍然可用。
func printShadowedEntries(output io.Writer, scope userScope, user string, disable bool) {
	if !disable || len(scope.Shadowed) == 0 {
		return
	}
	fmt.Fprintf(output, "warning: %s still has access through entries that cannot be toggled:\n", user)
	for _, entry := range scope.Shadowed {
		fmt.Fprintf(output, "  %s\n", entry)
	}
	fmt.Fprintln(output, "  edit the stack file to remove them if you need a full cutoff")
}

// printStaleEntries 提示 disabled.json 里引用了已不存在的 stack/用户的条目。
func printStaleEntries(output io.Writer, scope userScope) {
	stale := staleStateEntries(scope)
	if len(stale) == 0 {
		return
	}
	fmt.Fprintln(output, "warning: runtime/disabled.json has entries that no longer match any configured user:")
	for _, entry := range stale {
		fmt.Fprintf(output, "  %s/%s\n", entry.Stack, entry.User)
	}
	fmt.Fprintln(output, "  run psctl user enable USER STACK to clear one")
}

// staleStateEntries 返回配置里已不存在、但仍留在禁用状态里的条目。
func staleStateEntries(scope userScope) []userstate.Entry {
	known := map[string]bool{}
	for _, site := range scope.Sites {
		known[site.Stack+"\x00"+site.User] = true
	}
	stale := make([]userstate.Entry, 0)
	for _, entry := range scope.State.Disabled {
		if !known[entry.Stack+"\x00"+entry.User] {
			stale = append(stale, entry)
		}
	}
	return stale
}

// clearStaleUserState 允许 enable 清掉配置里已不存在的陈旧禁用条目。
func clearStaleUserState(command *cobra.Command, scope userScope, user string, target string) error {
	state := scope.State
	stack := strings.TrimPrefix(target, "xray/")
	cleared := make([]string, 0)
	for _, entry := range append([]userstate.Entry(nil), state.Disabled...) {
		if entry.User != user || (stack != "" && entry.Stack != stack) {
			continue
		}
		if state.Enable(entry.Stack, entry.User) {
			cleared = append(cleared, entry.Stack)
		}
	}
	if len(cleared) == 0 {
		return noSuchUserError(scope, user, target)
	}
	sort.Strings(cleared)
	if err := userstate.Save(userstate.Path(scope.StackSet.Config), state); err != nil {
		return err
	}
	fmt.Fprintf(command.OutOrStdout(), "Cleared stale disabled state for %s on: %s\n", user, strings.Join(cleared, ", "))
	return nil
}

// noSuchUserError 区分“目标里没有 Xray 服务”和“用户不存在”。
func noSuchUserError(scope userScope, user string, target string) error {
	if len(scope.XrayStacks) == 0 {
		return fmt.Errorf("target has no xray service to toggle users on:%s", targetSuffix(target))
	}
	return fmt.Errorf("user does not exist in any vmess or shadowsocks inbound: %s%s", user, targetSuffix(target))
}

// ensureInboundKeepsUser 拒绝把某个 inbound 的用户全部禁用。
func ensureInboundKeepsUser(scope userScope, user string) error {
	for _, site := range scope.Sites {
		if site.Disabled {
			continue
		}
		remaining := 0
		for _, candidate := range site.Inbound.Users {
			if candidate.User == user || scope.State.IsDisabled(site.Stack, candidate.User) {
				continue
			}
			remaining++
		}
		if remaining == 0 {
			return fmt.Errorf("%s is the last enabled user of %s/%s; disable the inbound in the stack file instead", user, site.Stack, site.Tag)
		}
	}
	return nil
}

// resolveUserScope 把 USER 和 TARGET 解析为具体的 stack/inbound/用户三元组。
//
// user 为空时返回 scope 内所有可启停的用户，供 list 使用。禁用状态只在这里读一次，
// 保证 last-user 判断和后续写入用的是同一份快照。
func resolveUserScope(command *cobra.Command, user string, target string) (userScope, error) {
	if strings.HasPrefix(target, "clash/") {
		return userScope{}, fmt.Errorf("user toggling only applies to xray: %s", target)
	}
	stackSet, err := loadAgentStackSet(command, true)
	if err != nil {
		return userScope{}, err
	}
	referenceGraph, err := graph.BuildReferenceGraph(stackSet)
	if err != nil {
		return userScope{}, err
	}
	targetScope, err := graph.ResolveTargetScope(referenceGraph, target)
	if err != nil {
		return userScope{}, err
	}
	state, err := userstate.Load(userstate.Path(stackSet.Config))
	if err != nil {
		return userScope{}, err
	}
	stackSet.DisabledUsers = state.UserSet()
	stacks := stackSet.ByName()
	scope := userScope{StackSet: stackSet, State: state, Sites: make([]userSite, 0), Shadowed: make([]string, 0)}
	for _, node := range targetScope.Nodes {
		if node.Component != "xray" {
			continue
		}
		stack, ok := stacks[node.Stack]
		if !ok {
			continue
		}
		scope.XrayStacks = append(scope.XrayStacks, stack.Name)
		endpoint := xrayapi.ResolveAPIEndpoint(stackSet.Config, *stack)
		for _, inbound := range stack.Xray.Inbounds {
			if !xraygen.SupportsUserToggle(inbound.Protocol) {
				scope.Shadowed = append(scope.Shadowed, shadowedInbound(stack.Name, inbound, user)...)
				continue
			}
			for _, candidate := range inbound.Users {
				if user != "" && candidate.User != user {
					continue
				}
				entry, disabled := state.EntryFor(stack.Name, candidate.User)
				scope.Sites = append(scope.Sites, userSite{
					Stack:    stack.Name,
					Tag:      inbound.TagOrDefault(),
					User:     candidate.User,
					Profile:  candidate.ProfileOrDefault(),
					Email:    candidate.EmailOrUser(),
					Inbound:  inbound,
					Endpoint: endpoint,
					Disabled: disabled,
					Since:    entry.Since,
				})
			}
		}
		scope.Shadowed = append(scope.Shadowed, shadowedClashListeners(*stack, user)...)
	}
	sort.Slice(scope.Sites, func(i int, j int) bool {
		if scope.Sites[i].Stack != scope.Sites[j].Stack {
			return scope.Sites[i].Stack < scope.Sites[j].Stack
		}
		if scope.Sites[i].Tag != scope.Sites[j].Tag {
			return scope.Sites[i].Tag < scope.Sites[j].Tag
		}
		return scope.Sites[i].User < scope.Sites[j].User
	})
	sort.Strings(scope.Shadowed)
	return scope, nil
}

// shadowedInbound 描述引用了该用户、但协议上不能启停的 Xray inbound。
func shadowedInbound(stackName string, inbound domain.Inbound, user string) []string {
	if user == "" {
		return nil
	}
	for _, candidate := range inbound.Users {
		if candidate.User == user {
			return []string{fmt.Sprintf("%s/%s (%s inbound, shared account)", stackName, inbound.TagOrDefault(), inbound.Protocol)}
		}
	}
	return nil
}

// shadowedClashListeners 描述用同名账号的 Clash listener。
//
// Clash listener 的账号和 config.yaml 的用户档案是两套凭据，这里只能按用户名提示可能相关。
func shadowedClashListeners(stack domain.Stack, user string) []string {
	if user == "" || !stack.Clash.Enabled {
		return nil
	}
	shadowed := make([]string, 0)
	for _, listener := range stack.Clash.Listeners.Socks {
		if clashListenerHasUser(listener.Users, user) {
			shadowed = append(shadowed, fmt.Sprintf("%s/%s (clash socks listener, separate credentials)", stack.Name, listener.Name))
		}
	}
	for _, listener := range stack.Clash.Listeners.HTTP {
		if clashListenerHasUser(listener.Users, user) {
			shadowed = append(shadowed, fmt.Sprintf("%s/%s (clash http listener, separate credentials)", stack.Name, listener.Name))
		}
	}
	return shadowed
}

// clashListenerHasUser 判断 Clash listener 是否配置了同名账号。
func clashListenerHasUser(users []domain.ClashListenerUser, user string) bool {
	for _, candidate := range users {
		if candidate.Username == user {
			return true
		}
	}
	return false
}

// printUserSites 输出用户状态表。
func printUserSites(output io.Writer, sites []userSite) {
	if len(sites) == 0 {
		fmt.Fprintln(output, "No togglable users found.")
		return
	}
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "STACK\tINBOUND\tUSER\tPROFILE\tSTATUS")
	for _, site := range sites {
		status := "enabled"
		if site.Disabled {
			status = "disabled"
			if site.Since != "" {
				status += " (since " + site.Since + ")"
			}
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", site.Stack, site.Tag, site.User, site.Profile, status)
	}
	_ = writer.Flush()
}

// inboundUserByName 取回 inbound 上指定用户的完整凭据。
func inboundUserByName(inbound domain.Inbound, user string) domain.InboundUser {
	for _, candidate := range inbound.Users {
		if candidate.User == user {
			return candidate
		}
	}
	return domain.InboundUser{}
}

// stateWord 返回启停动作对应的状态词。
func stateWord(disable bool) string {
	if disable {
		return "disabled"
	}
	return "enabled"
}

// targetSuffix 把可选 TARGET 拼进消息尾部。
func targetSuffix(target string) string {
	if target == "" {
		return ""
	}
	return " " + target
}
