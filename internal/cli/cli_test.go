package cli_test

import (
	"bytes"
	"testing"

	"github.com/eagle/proxystack-go/internal/cli/agent"
	"github.com/eagle/proxystack-go/internal/cli/sub"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestRootCommandsSmoke 验证两个二进制入口的最小命令树可运行。
func TestRootCommandsSmoke(t *testing.T) {
	tests := []struct {
		name    string
		command func() *cobra.Command
		args    []string
		want    string
	}{
		{name: "agent help", command: agent.NewRootCommand, args: []string{"--help"}, want: "ps-agent"},
		{name: "sub help", command: sub.NewRootCommand, args: []string{"--help"}, want: "ps-sub"},
		{name: "agent version", command: agent.NewRootCommand, args: []string{"version"}, want: "ps-agent\n  version: 0.1.0-dev\n  commit: "},
		{name: "sub version", command: sub.NewRootCommand, args: []string{"version"}, want: "ps-sub\n  version: 0.1.0-dev\n  commit: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := tt.command()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs(tt.args)

			err := command.Execute()

			require.NoError(t, err)
			require.Contains(t, output.String(), tt.want)
			if tt.args[0] == "version" {
				require.Contains(t, output.String(), "\n  build_datetime: ")
			}
		})
	}
}
