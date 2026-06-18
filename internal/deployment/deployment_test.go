package deployment_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eagle/proxystack-go/internal/testutil"
	"github.com/stretchr/testify/require"
)

const defaultReleaseRepo = "sunliang711/proxystack-go"

// TestDeploymentScriptsUseReleaseBinaryBootstrap 验证部署脚本默认使用 Release binary，并保留源码构建入口。
func TestDeploymentScriptsUseReleaseBinaryBootstrap(t *testing.T) {
	for _, scriptName := range []string{"install-agent.sh", "install-sub-local.sh"} {
		t.Run(scriptName, func(t *testing.T) {
			content := readRepoFile(t, "scripts", scriptName)

			require.Contains(t, content, "RELEASE_VERSION=\"latest\"")
			require.Contains(t, content, "DEFAULT_RELEASE_REPO=\""+defaultReleaseRepo+"\"")
			require.Contains(t, content, "RELEASE_REPO=\"\"")
			require.Contains(t, content, "--version VERSION")
			require.Contains(t, content, "--repo OWNER/REPO")
			require.Contains(t, content, "Default: "+defaultReleaseRepo)
			require.Contains(t, content, "resolve_default_release_repo")
			require.NotContains(t, content, "normalize_git_remote_repo")
			require.NotContains(t, content, "remote.origin.url")
			require.Contains(t, content, "install_release_binaries")
			require.Contains(t, content, "go build -trimpath")
			require.Contains(t, content, "go_build_ldflags")
			require.Contains(t, content, "ps-agent")
			require.Contains(t, content, "ps-sub")
			require.Contains(t, content, "install_file \"${agent_binary}\" \"${bin_dir}/ps-agent\" \"0755\"")
			require.NotContains(t, content, "scripts/lib/common.sh")
			require.NotContains(t, content, "python3 -m venv")
			require.NotContains(t, content, "pip install")
		})
	}

	common := readRepoFile(t, "scripts", "lib", "common.sh")
	require.Contains(t, common, "releases/latest/download")
	require.Contains(t, common, "SHA256SUMS")
	require.Contains(t, common, "release_asset_name")
	require.Contains(t, common, "release_binary_path")
	require.Contains(t, common, "proxystack-go_%s_%s_%s.tar.gz")
	require.Contains(t, common, "resolve_build_version")
	require.Contains(t, common, "resolve_build_commit")
	require.Contains(t, common, "resolve_build_datetime")

	workflow := readRepoFile(t, ".github", "workflows", "release.yml")
	require.Contains(t, workflow, `tar -C "${work_dir}" -czf "dist/${archive}" ps-agent ps-sub`)
}

// TestDockerSubDeploymentUsesSecureDefaults 验证 Docker 部署文件保留 sub-only 和安全运行参数。
func TestDockerSubDeploymentUsesSecureDefaults(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile.sub")
	compose := readRepoFile(t, "docker-compose.sub.yml")
	deployScript := readRepoFile(t, "scripts", "deploy-sub-docker.sh")

	require.Contains(t, dockerfile, "./cmd/ps-sub")
	require.Contains(t, dockerfile, "BUILD_VERSION")
	require.Contains(t, dockerfile, "BUILD_COMMIT")
	require.Contains(t, dockerfile, "BUILD_DATETIME")
	require.Contains(t, dockerfile, "USER 10001:10001")
	require.Contains(t, dockerfile, "VOLUME [\"/data\"]")
	require.NotContains(t, dockerfile, "xray")
	require.NotContains(t, dockerfile, "mihomo")

	require.Contains(t, compose, "read_only: true")
	require.Contains(t, compose, "cap_drop:")
	require.Contains(t, compose, "- ALL")
	require.Contains(t, compose, "no-new-privileges:true")
	require.Contains(t, compose, "/opt/proxystack:/data")
	require.Contains(t, compose, "user: \"10001:10001\"")
	require.Contains(t, compose, "- ps-sub")
	require.NotContains(t, compose, "- proxystack-sub")

	require.Contains(t, deployScript, "--read-only")
	require.Contains(t, deployScript, "--cap-drop ALL")
	require.Contains(t, deployScript, "--security-opt no-new-privileges:true")
	require.Contains(t, deployScript, "--build")
	require.Contains(t, deployScript, "--volume \"${BASE_DIR}:/data\"")
	require.Contains(t, deployScript, "ps-sub --base-dir /data serve")
}

// TestDockerSubDeployDryRunUsesBaseDir 验证 Docker dry-run 使用 host base dir 映射到容器 /data。
func TestDockerSubDeployDryRunUsesBaseDir(t *testing.T) {
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	baseDir := filepath.Join(t.TempDir(), "sub-only")

	output, err := runScript(t, "scripts/deploy-sub-docker.sh", "--dry-run", "--base-dir", baseDir)

	require.NoError(t, err, output)
	require.Contains(t, output, "install -d -m 0750 "+baseDir)
	require.Contains(t, output, "install -d -m 0750 "+filepath.Join(baseDir, "sub"))
	require.Contains(t, output, "install -d -m 0750 "+filepath.Join(baseDir, "sub", "inputs"))
	require.Contains(t, output, "--volume "+baseDir+":/data")
	require.Contains(t, output, "ps-sub --base-dir /data serve")
}

// TestInstallScriptsDryRunDownloadRelease 验证安装脚本 dry-run 会从默认 GitHub Release 下载二进制。
func TestInstallScriptsDryRunDownloadRelease(t *testing.T) {
	for _, scriptName := range []string{"install-agent.sh", "install-sub-local.sh"} {
		t.Run(scriptName, func(t *testing.T) {
			baseDir := filepath.Join(t.TempDir(), "proxystack")
			binDir := filepath.Join(t.TempDir(), "bin")

			output, err := runScript(t, filepath.Join("scripts", scriptName), "--dry-run", "--base-dir", baseDir, "--bin-dir", binDir, "--version", "1.2.3")

			require.NoError(t, err, output)
			require.Contains(t, output, "https://github.com/"+defaultReleaseRepo+"/releases/download/v1.2.3/proxystack-go_v1.2.3_")
			require.Contains(t, output, "https://github.com/"+defaultReleaseRepo+"/releases/download/v1.2.3/SHA256SUMS")
			require.Contains(t, output, "tar -xzf")
			require.Contains(t, output, "proxystack-release-dry-run")
			require.Contains(t, output, filepath.Join(binDir, "ps-agent"))
			require.Contains(t, output, filepath.Join(binDir, "ps-sub"))
			require.NotContains(t, output, filepath.Join(baseDir, "bin", "ps-agent"))
		})
	}
}

// TestInstallScriptsDryRunLatestKeepsStableAssetAlias 验证 latest 下载仍使用不带版本的兼容资产名。
func TestInstallScriptsDryRunLatestKeepsStableAssetAlias(t *testing.T) {
	for _, scriptName := range []string{"install-agent.sh", "install-sub-local.sh"} {
		t.Run(scriptName, func(t *testing.T) {
			baseDir := filepath.Join(t.TempDir(), "proxystack")
			binDir := filepath.Join(t.TempDir(), "bin")

			output, err := runScript(t, filepath.Join("scripts", scriptName), "--dry-run", "--base-dir", baseDir, "--bin-dir", binDir)

			require.NoError(t, err, output)
			require.Contains(t, output, "https://github.com/"+defaultReleaseRepo+"/releases/latest/download/proxystack-go_")
			require.NotContains(t, output, "releases/latest/download/proxystack-go_latest_")
			require.Contains(t, output, "https://github.com/"+defaultReleaseRepo+"/releases/latest/download/SHA256SUMS")
		})
	}
}

// TestInstallScriptsRejectUnsafeManagedPaths 验证 root bootstrap 脚本拒绝宽泛托管目录。
func TestInstallScriptsRejectUnsafeManagedPaths(t *testing.T) {
	for _, scriptName := range []string{"install-agent.sh", "install-sub-local.sh"} {
		t.Run(scriptName, func(t *testing.T) {
			output, err := runScript(t, filepath.Join("scripts", scriptName), "--dry-run", "--base-dir", "/opt")

			require.Error(t, err)
			require.Contains(t, output, "base directory is too broad")
		})
	}
}

// TestInstallScriptsRejectCustomSystemdIdentity 验证 systemd 模式拒绝和固定 unit 不一致的用户组。
func TestInstallScriptsRejectCustomSystemdIdentity(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "agent", args: []string{"scripts/install-agent.sh", "--dry-run", "--install-systemd", "--user", "custom"}},
		{name: "sub", args: []string{"scripts/install-sub-local.sh", "--dry-run", "--start", "--user", "custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runScript(t, tt.args[0], tt.args[1:]...)

			require.Error(t, err)
			require.Contains(t, output, "requires --user proxystack --group proxystack --bin-dir /usr/local/bin")
		})
	}
}

// readRepoFile 读取仓库内文件内容。
func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := testutil.RepoPath(t, parts...)
	content, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	return string(content)
}

// runScript 执行仓库内脚本并返回合并输出。
func runScript(t *testing.T, scriptPath string, args ...string) (string, error) {
	t.Helper()
	repoRoot := testutil.RepoPath(t)
	command := exec.Command("bash", append([]string{filepath.Join(repoRoot, scriptPath)}, args...)...)
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	return string(output), err
}
