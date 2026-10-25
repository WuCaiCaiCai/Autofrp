package frp

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/systemd"
)

const defaultFrpVersion = "0.61.2"

// ArchName 将 Go GOARCH 映射为 frp 发布包架构名
func ArchName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	case "386":
		return "386"
	case "riscv64":
		return "riscv64"
	default:
		return ""
	}
}

// FetchLatestVersion 查询 GitHub Release 最新 frp 版本号，失败时回退到稳定默认版本
func FetchLatestVersion() string {
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/fatedier/frp/releases/latest")
	if err != nil {
		return defaultFrpVersion
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return defaultFrpVersion
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return defaultFrpVersion
	}
	ver := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if ver == "" {
		return defaultFrpVersion
	}
	return ver
}

// InstallBinaries 下载并安装 frps 与 frpc 到 /usr/local/bin
func InstallBinaries(store *config.Store) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("安装 frp 需要 root 权限，请使用 sudo 运行")
	}
	arch := ArchName()
	if arch == "" {
		return "", fmt.Errorf("不支持的 CPU 架构: %s", runtime.GOARCH)
	}

	ver := FetchLatestVersion()
	tarName := fmt.Sprintf("frp_%s_linux_%s.tar.gz", ver, arch)
	rawURL := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/v%s/%s", ver, tarName)
	urls := []string{
		rawURL,
		"https://ghfast.top/" + rawURL,
		"https://gh-proxy.com/" + rawURL,
	}

	tmpFile, err := os.CreateTemp("", "autof-frp-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	downloaded := false
	client := &http.Client{Timeout: 180 * time.Second}
	for _, u := range urls {
		if err := downloadFile(client, u, tmpPath); err == nil {
			downloaded = true
			break
		}
	}
	if !downloaded {
		return "", errors.New("从 GitHub 及镜像下载 frp 失败，请检查网络连接")
	}

	if err := extractBinaries(tmpPath, map[string]string{
		"frps": "/usr/local/bin/frps",
		"frpc": "/usr/local/bin/frpc",
	}); err != nil {
		return "", fmt.Errorf("解压安装 frp 失败: %w", err)
	}

	_ = store.Update(func(c *config.Config) error {
		c.Frps.InstalledVer = ver
		return nil
	})
	return ver, nil
}

func downloadFile(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil || n < 1024 {
		return errors.New("下载文件不完整")
	}
	return nil
}

func extractBinaries(tarGzPath string, targets map[string]string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	found := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		dest, ok := targets[base]
		if !ok {
			continue
		}
		tmpDest := dest + ".tmp"
		out, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			_ = os.Remove(tmpDest)
			return err
		}
		out.Close()
		if err := os.Rename(tmpDest, dest); err != nil {
			_ = os.Remove(tmpDest)
			return err
		}
		found++
	}
	if found == 0 {
		return errors.New("压缩包内未找到 frps/frpc 二进制文件")
	}
	return nil
}

// WriteConfigs 将 frps.toml、导出用 clients/frpc.toml 和本地运行用 frpc.toml 写入配置目录
func WriteConfigs(store *config.Store) error {
	cfg := store.Get()
	if err := os.WriteFile(store.FrpsConfPath(), []byte(GenerateFrpsToml(cfg)), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(store.ClientExportPath(), []byte(GenerateFrpcTomlForExport(cfg, -1)), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(store.FrpcConfPath(), []byte(GenerateFrpcTomlForClient(cfg)), 0600); err != nil {
		return err
	}
	return nil
}

// EnsureFrpsInstalledAndStarted 安装（若缺失）并启动 frps 服务端
func EnsureFrpsInstalledAndStarted(store *config.Store) error {
	if systemd.FindBinary("frps") == "" {
		if _, err := InstallBinaries(store); err != nil {
			return err
		}
	}
	if err := WriteConfigs(store); err != nil {
		return err
	}
	bin := systemd.FindBinary("frps")
	_ = systemd.WriteUnit(
		"frps",
		"frp server (frps) managed by Autofrp",
		fmt.Sprintf("%s -c %s", bin, store.FrpsConfPath()),
		"",
		nil,
	)
	return systemd.Control("frps", "restart", bin, []string{"-c", store.FrpsConfPath()}, store.StateDir(), nil)
}

// ApplyFrps 保存配置并在 frps 运行时按需重启
func ApplyFrps(store *config.Store, restartIfRunning bool) error {
	if err := WriteConfigs(store); err != nil {
		return err
	}
	st := systemd.QueryStatus("frps")
	if restartIfRunning && st.Running {
		bin := st.BinaryPath
		return systemd.Control("frps", "restart", bin, []string{"-c", store.FrpsConfPath()}, store.StateDir(), nil)
	}
	return nil
}

// EnsureFrpcInstalledAndStarted 安装（若缺失）并启动本地 frpc 客户端
func EnsureFrpcInstalledAndStarted(store *config.Store) error {
	if systemd.FindBinary("frpc") == "" {
		if _, err := InstallBinaries(store); err != nil {
			return err
		}
	}
	if err := WriteConfigs(store); err != nil {
		return err
	}
	bin := systemd.FindBinary("frpc")
	_ = systemd.WriteUnit(
		"frpc",
		"frp client (frpc) managed by Autofrp",
		fmt.Sprintf("%s -c %s", bin, store.FrpcConfPath()),
		fmt.Sprintf("%s reload -c %s", bin, store.FrpcConfPath()),
		nil,
	)
	return systemd.Control("frpc", "restart", bin, []string{"-c", store.FrpcConfPath()}, store.StateDir(), nil)
}

// ApplyFrpc 保存配置并在 frpc 运行时优先执行零中断热重载 (frpc reload)
func ApplyFrpc(store *config.Store, reloadIfRunning bool) error {
	if err := WriteConfigs(store); err != nil {
		return err
	}
	st := systemd.QueryStatus("frpc")
	if !reloadIfRunning || !st.Running {
		return nil
	}
	bin := st.BinaryPath
	if bin != "" {
		// 优先使用 frpc reload 热重载隧道，不断开现有连接
		if err := exec.Command(bin, "reload", "-c", store.FrpcConfPath()).Run(); err == nil {
			return nil
		}
	}
	return systemd.Control("frpc", "restart", bin, []string{"-c", store.FrpcConfPath()}, store.StateDir(), nil)
}
