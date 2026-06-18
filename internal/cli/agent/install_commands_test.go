package agent

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInstallProgressPrinterWritesLinesForNonTTY 验证非交互式 stderr 会逐行输出进度。
func TestInstallProgressPrinterWritesLinesForNonTTY(t *testing.T) {
	var output bytes.Buffer
	printer := newInstallProgressPrinter(&output)

	printer.Print("install: prepare mihomo")
	printer.Print("download: progress file.gz [###############---------------]  50.0% 1.0 MiB/2.0 MiB 512.0 KiB/s")
	printer.Finish()

	require.Contains(t, output.String(), "install: prepare mihomo\n")
	require.Contains(t, output.String(), "download: progress file.gz")
}
