package xray_test

import (
	"encoding/json"
	"testing"

	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/generator/xray"
	"github.com/stretchr/testify/require"
)

const twoUserVmessInboundYAML = `name: vmess
protocol: vmess
listen: 0.0.0.0
port: 24100
network: raw
sub: true
users:
  - user: alice
    uuid: 11111111-1111-4111-8111-111111111111
  - user: bob
    uuid: 22222222-2222-4222-8222-222222222222`

const noAuthSocksOutboundYAML = `type: socks5
server: socks.example.com
port: 1080`

// TestRenderConfigSkipsDisabledVmessUser 验证被禁用的用户不会出现在生成的 clients 列表里。
func TestRenderConfigSkipsDisabledVmessUser(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "vm", noAuthSocksOutboundYAML, asListItem(twoUserVmessInboundYAML), ""))
	stackSet.DisabledUsers = domain.DisabledUserSet{}
	stackSet.DisabledUsers.Add("vm", "bob")

	output, err := xray.DumpsConfig(stackSet, "vm")

	require.NoError(t, err)
	require.Contains(t, output, "alice")
	require.NotContains(t, output, "bob")
	require.NotContains(t, output, "22222222-2222-4222-8222-222222222222")
}

// TestRenderConfigIgnoresDisabledUserFromOtherStack 验证禁用状态按 stack 隔离。
func TestRenderConfigIgnoresDisabledUserFromOtherStack(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "vm", noAuthSocksOutboundYAML, asListItem(twoUserVmessInboundYAML), ""))
	stackSet.DisabledUsers = domain.DisabledUserSet{}
	stackSet.DisabledUsers.Add("other", "bob")

	output, err := xray.DumpsConfig(stackSet, "vm")

	require.NoError(t, err)
	require.Contains(t, output, "bob")
}

// TestRenderConfigRejectsFullyDisabledInbound 验证禁用某个 inbound 的全部用户会报错而不是生成空 clients。
func TestRenderConfigRejectsFullyDisabledInbound(t *testing.T) {
	stackSet := makeStackSet(t, makeStack(t, "vm", noAuthSocksOutboundYAML, asListItem(twoUserVmessInboundYAML), ""))
	stackSet.DisabledUsers = domain.DisabledUserSet{}
	stackSet.DisabledUsers.Add("vm", "alice")
	stackSet.DisabledUsers.Add("vm", "bob")

	_, err := xray.DumpsConfig(stackSet, "vm")

	require.Error(t, err)
	require.Contains(t, err.Error(), "all users of inbound are disabled")
}

// TestRenderConfigKeepsSocksAccountWhenUserDisabled 验证单账号 socks inbound 不受用户启停影响。
//
// socks5/http 把账号摘掉会退化成免认证入口，因此必须完全不参与过滤。
func TestRenderConfigKeepsSocksAccountWhenUserDisabled(t *testing.T) {
	inbound := `name: relay
protocol: socks5
listen: 127.0.0.1
port: 24000
sub: true
auth:
  type: password
  username: relay-user
  password: relay-pass
user_refs: [alice]`
	stackSet := makeStackSet(t, makeStack(t, "sk", noAuthSocksOutboundYAML, asListItem(inbound), ""))
	stackSet.DisabledUsers = domain.DisabledUserSet{}
	stackSet.DisabledUsers.Add("sk", "alice")

	output, err := xray.DumpsConfig(stackSet, "sk")

	require.NoError(t, err)
	require.Contains(t, output, "relay-user")
	require.Contains(t, output, `"auth": "password"`)
}

// TestSupportsUserToggleCoversMultiUserProtocolsOnly 验证只有多用户协议参与启停。
func TestSupportsUserToggleCoversMultiUserProtocolsOnly(t *testing.T) {
	require.True(t, xray.SupportsUserToggle("vmess"))
	require.True(t, xray.SupportsUserToggle("shadowsocks"))
	require.False(t, xray.SupportsUserToggle("socks5"))
	require.False(t, xray.SupportsUserToggle("http"))
}

// TestDumpsInboundUserPatchRendersSingleClient 验证 adu 用的片段只带回被启用的那个用户。
func TestDumpsInboundUserPatchRendersSingleClient(t *testing.T) {
	stack := makeStack(t, "vm", noAuthSocksOutboundYAML, asListItem(twoUserVmessInboundYAML), "")
	inbound := stack.Xray.Inbounds[0]

	patch, err := xray.DumpsInboundUserPatch(inbound, inbound.Users[1])

	require.NoError(t, err)
	var decoded struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					ID    string `json:"id"`
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	require.NoError(t, json.Unmarshal([]byte(patch), &decoded))
	require.Len(t, decoded.Inbounds, 1)
	require.Equal(t, inbound.TagOrDefault(), decoded.Inbounds[0].Tag)
	require.Equal(t, "vmess", decoded.Inbounds[0].Protocol)
	require.Len(t, decoded.Inbounds[0].Settings.Clients, 1)
	require.Equal(t, "bob", decoded.Inbounds[0].Settings.Clients[0].Email)
	require.Equal(t, "22222222-2222-4222-8222-222222222222", decoded.Inbounds[0].Settings.Clients[0].ID)
}

// TestDumpsInboundUserPatchRejectsSingleAccountProtocol 验证 socks/http 不能走热添加路径。
func TestDumpsInboundUserPatchRejectsSingleAccountProtocol(t *testing.T) {
	_, err := xray.DumpsInboundUserPatch(domain.Inbound{Protocol: "socks5"}, domain.InboundUser{User: "alice"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support user toggle")
}
