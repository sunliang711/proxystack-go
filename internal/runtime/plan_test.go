package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestBuildPlanApplyAndReuseGeneratedAt 验证 runtime plan 首次生成、apply 和无变化时间戳复用。
func TestBuildPlanApplyAndReuseGeneratedAt(t *testing.T) {
	baseDir := writeRuntimeFixture(t)
	configPath := filepath.Join(baseDir, "config.yaml")
	firstNow := func() time.Time { return time.Date(2026, 6, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)) }
	secondNow := func() time.Time { return time.Date(2026, 6, 17, 11, 0, 0, 0, time.FixedZone("CST", 8*60*60)) }

	plan, err := BuildPlan(BuildOptions{ConfigPath: configPath, SkipSystemPorts: true, Now: firstNow})

	require.NoError(t, err)
	require.Len(t, plan.GeneratedFiles, 2)
	require.Equal(t, []string{ActionCreate, ActionCreate}, changeActions(plan.Changes))
	require.Equal(t, "2026-06-17T10:00:00+08:00", plan.Manifest.GeneratedAt)

	require.NoError(t, ApplyPlan(plan))

	nextPlan, err := BuildPlan(BuildOptions{ConfigPath: configPath, SkipSystemPorts: true, Now: secondNow})

	require.NoError(t, err)
	require.Equal(t, []string{ActionNoChange, ActionNoChange}, changeActions(nextPlan.Changes))
	require.Equal(t, "2026-06-17T10:00:00+08:00", nextPlan.Manifest.GeneratedAt)
	require.FileExists(t, filepath.Join(baseDir, "runtime", "manifest.json"))

	scopedPlan, err := BuildPlan(BuildOptions{ConfigPath: configPath, Target: "xrelay/usa1", SkipSystemPorts: true, Now: secondNow})
	require.NoError(t, err)
	require.Len(t, scopedPlan.GeneratedFiles, 1)
	require.NoError(t, ApplyPlan(scopedPlan))
	manifest, err := ReadManifest(filepath.Join(baseDir, "runtime", "manifest.json"))
	require.NoError(t, err)
	require.Len(t, manifest.Files, 2)

	stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
	stackData, err := os.ReadFile(stackPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stackPath, []byte(strings.Replace(string(stackData), "enabled: true", "enabled: false", 1)), 0o640))
	disabledPlan, err := BuildPlan(BuildOptions{ConfigPath: configPath, Target: "usa1", SkipSystemPorts: true, Now: secondNow})
	require.NoError(t, err)
	require.Empty(t, disabledPlan.GeneratedFiles)
	require.Equal(t, []string{ActionDelete, ActionDelete}, changeActions(disabledPlan.Changes))
}

// TestBuildPlanDeletesDisabledComponentTarget 验证组件 disabled 后仍可通过组件 target 删除历史文件。
func TestBuildPlanDeletesDisabledComponentTarget(t *testing.T) {
	tests := []struct {
		name     string
		replaces map[string]string
		target   string
		want     string
	}{
		{
			name:     "xrelay",
			replaces: map[string]string{"xrelay:\n  enabled: true": "xrelay:\n  enabled: false"},
			target:   "xrelay/usa1",
			want:     "generated/xray/usa1.json",
		},
		{
			name: "clash",
			replaces: map[string]string{
				"clash:\n  enabled: true":                                 "clash:\n  enabled: false",
				"  outbound:\n    type: clash\n    ref: usa1.clash.socks": "  outbound:\n    type: direct",
			},
			target: "clash/usa1",
			want:   "generated/mihomo/usa1.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := writeRuntimeFixture(t)
			configPath := filepath.Join(baseDir, "config.yaml")
			now := func() time.Time { return time.Date(2026, 6, 17, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)) }
			plan, err := BuildPlan(BuildOptions{ConfigPath: configPath, SkipSystemPorts: true, Now: now})
			require.NoError(t, err)
			require.NoError(t, ApplyPlan(plan))

			stackPath := filepath.Join(baseDir, "stacks", "usa1.yaml")
			stackData, err := os.ReadFile(stackPath)
			require.NoError(t, err)
			updatedStack := string(stackData)
			for oldValue, newValue := range tt.replaces {
				updatedStack = strings.Replace(updatedStack, oldValue, newValue, 1)
			}
			require.NoError(t, os.WriteFile(stackPath, []byte(updatedStack), 0o640))

			disabledPlan, err := BuildPlan(BuildOptions{ConfigPath: configPath, Target: tt.target, SkipSystemPorts: true, Now: now})

			require.NoError(t, err)
			require.Empty(t, disabledPlan.GeneratedFiles)
			require.Len(t, disabledPlan.Changes, 1)
			require.Equal(t, ActionDelete, disabledPlan.Changes[0].Action)
			require.Equal(t, tt.want, disabledPlan.Changes[0].RelativePath)
		})
	}
}

func writeRuntimeFixture(t *testing.T) string {
	t.Helper()
	baseDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "stacks"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.yaml"), []byte(`version: 1
paths:
  bin: bin
  geo: geo
  stacks: stacks
  runtime: runtime
  generated: runtime/generated
  publish: publish
  downloads: downloads
  sub: sub
external_host: proxy.example.com
subscription:
  source: local
port_ranges:
  xrelay_inbound: 4300-4399
  clash_socks: 7001-7101
  clash_http: 7201-7301
  xray_api_range: 10001-10999
  clash_controller: 19000-19999
defaults:
  clash:
    mode: Rule
    rule_profile: default
  xrelay:
    loglevel: warning
    api:
      enabled: true
      tag: api
      listen: 127.0.0.1:10085
      services: [StatsService]
    stats:
      enabled: true
    policy:
      enabled: true
security:
  require_auth_for_public_socks_http: true
  allow_noauth_public: false
install:
  mihomo:
    version: latest
    source: auto
  xray:
    version: latest
    source: auto
  geo:
    version: latest
    source: auto
`), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "stacks", "usa1.yaml"), []byte(`name: usa1
enabled: true
role: edge
xrelay:
  enabled: true
  api:
    enabled: true
    listen: 127.0.0.1:10091
  outbound:
    type: clash
    ref: usa1.clash.socks
  inbounds:
    - name: relay
      protocol: socks5
      listen: 0.0.0.0
      port: 24001
      udp: true
      auth:
        type: password
        username: usa1
        password: relay-password
      user: alice
      sub: true
clash:
  enabled: true
  mode: Rule
  controller:
    listen: 127.0.0.1:19091
    secret: controller-secret
  listeners:
    socks:
      - name: local
        listen: 127.0.0.1
        port: 17091
  upstreams:
    - name: server-a
      type: raw
      config:
        type: vmess
        server: server-a.example.com
        port: 443
        uuid: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa
        network: ws
  groups:
    - name: AllProxy
      type: select
      proxies: [server-a, DIRECT]
  rules:
    profile: default
`), 0o640))
	return baseDir
}

func changeActions(changes []FileChange) []string {
	actions := make([]string, 0, len(changes))
	for _, change := range changes {
		actions = append(actions, change.Action)
	}
	return actions
}
