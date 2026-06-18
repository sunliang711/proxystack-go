package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveManagerKindAuto 验证 auto 会按平台选择服务管理后端。
func TestResolveManagerKindAuto(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		want    string
		wantErr string
	}{
		{name: "linux", goos: "linux", want: ManagerSystemd},
		{name: "darwin", goos: "darwin", want: ManagerLaunchd},
		{name: "unsupported", goos: "plan9", wantErr: "service manager auto is not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveManagerKind(ManagerAuto, tt.goos)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestNewManagerRejectsUnknownKind 验证未知服务管理器会 fail fast。
func TestNewManagerRejectsUnknownKind(t *testing.T) {
	_, err := NewManager("unknown")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported service manager")
}
