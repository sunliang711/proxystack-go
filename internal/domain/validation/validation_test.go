package validation_test

import (
	"testing"

	"github.com/eagle/proxystack-go/internal/config"
	"github.com/eagle/proxystack-go/internal/domain"
	"github.com/eagle/proxystack-go/internal/domain/validation"
	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestFakePortCheckerReportsOccupiedPort 验证系统端口占用可通过 fake checker 测试。
func TestFakePortCheckerReportsOccupiedPort(t *testing.T) {
	stackSet := loadExampleStackSet(t)

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.FakePortChecker{
		Occupied: map[int]bool{24001: true},
	}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "listen port 24001 is already in use")
}

// TestValidatePublicInboundAuthRejectsNoAuth 验证非回环 socks/http noauth 默认失败。
func TestValidatePublicInboundAuthRejectsNoAuth(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[0].Xrelay.Inbounds[0].Auth = &domain.InboundAuth{Type: "noauth"}

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.NoopPortChecker{}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "public socks/http inbound requires password auth")
}

// TestValidateDuplicatePort 验证跨 stack 端口重复会报错。
func TestValidateDuplicatePort(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[1].Xrelay.Inbounds[0].Port = stackSet.Stacks[0].Xrelay.Inbounds[0].Port

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.NoopPortChecker{}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate listen port")
}

// TestValidateSubscriptionProxyNamesRejectsDuplicateName 验证同一订阅用户下最终节点名不能重复。
func TestValidateSubscriptionProxyNamesRejectsDuplicateName(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[1].Xrelay.Inbounds[0].Remark = stackSet.Stacks[0].Xrelay.Inbounds[0].Remark

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.NoopPortChecker{}))

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate proxy name for user")
	require.Contains(t, err.Error(), "user=alice")
}

// TestDisabledStackDoesNotParticipateInRuntimeValidation 验证 disabled stack 不参与端口和公开鉴权校验。
func TestDisabledStackDoesNotParticipateInRuntimeValidation(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[0].Enabled = false
	stackSet.Stacks[0].Xrelay.Inbounds[0].Port = stackSet.Stacks[1].Xrelay.Inbounds[0].Port
	stackSet.Stacks[0].Xrelay.Inbounds[0].Auth = &domain.InboundAuth{Type: "noauth"}

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.NoopPortChecker{}))

	require.NoError(t, err)
}

// TestDisabledComponentsDoNotParticipateInRuntimeValidation 验证 disabled component 不参与端口校验。
func TestDisabledComponentsDoNotParticipateInRuntimeValidation(t *testing.T) {
	stackSet := loadExampleStackSet(t)
	stackSet.Stacks[0].Xrelay.Enabled = false
	stackSet.Stacks[0].Xrelay.Inbounds[0].Port = stackSet.Stacks[1].Xrelay.Inbounds[0].Port
	stackSet.Stacks[0].Clash.Enabled = false
	stackSet.Stacks[0].Clash.Listeners.Socks[0].Port = stackSet.Stacks[1].Clash.Listeners.Socks[0].Port

	err := validation.ValidateStackSet(stackSet, validation.WithPortChecker(validation.NoopPortChecker{}))

	require.NoError(t, err)
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
