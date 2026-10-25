package caddy

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/systemd"
)

const defaultCaddyVersion = "2.9.1"

var dnsPluginRepoMap = map[string]string{
	"cloudflare": "github.com/caddy-dns/cloudflare",
	"alidns":     "github.com/caddy-dns/alidns",
	"dnspod":     "github.com/caddy-dns/dnspod",
	"duckdns":    "github.com/caddy-dns/duckdns",
}

// InstallCaddy 下载并安装 Caddy 二进制到 /usr/local/bin/caddy
// 若 dnsProvider 非空（如 cloudflare），优先从 Caddy 官方构建 API 下载内置对应 DNS 插件的二进制
func InstallCaddy(dnsProvider string) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("安装 Caddy 需要 root 权限，请使用 sudo 运行")
	}

	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64", "arm":
	default:
		return "", fmt.Errorf("不支持的 CPU 架构: %s", arch)
	}

	client := &http.Client{Timeout: 240 * time.Second}
	dest := "/usr/local/bin/caddy"

	// 1. 若指定了 DNS 插件，或使用官方 API 直接拉取单二进制
	pluginPkg := dnsPluginRepoMap[strings.ToLower(strings.TrimSpace(dnsProvider))]
	apiURL := fmt.Sprintf("https://caddyserver.com/api/download?os=linux&arch=%s", url.QueryEscape(arch))
	if pluginPkg != "" {
		apiURL += "&p=" + url.QueryEscape(pluginPkg)
	}

	if err := downloadDirectBinary(client, apiURL, dest); err == nil {
		ver := systemd.GetVersion("caddy")
		if ver == "" {
			ver = defaultCaddyVersion
		}
		return ver, nil
	}

	// 2. 回退到 GitHub Release + 国内镜像下载标准版 tar.gz
	ver := fetchLatestCaddyVersion()
	tarName := fmt.Sprintf("caddy_%s_linux_%s.tar.gz", ver, arch)
	rawURL := fmt.Sprintf("https://github.com/caddyserver/caddy/releases/download/v%s/%s", ver, tarName)
	urls := []string{
		rawURL,
		"https://ghfast.top/" + rawURL,
		"https://gh-proxy.com/" + rawURL,
	}

	tmpFile, err := os.CreateTemp("", "autof-caddy-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	downloaded := false
	for _, u := range urls {
		if err := downloadTarGz(client, u, tmpPath); err == nil {
			downloaded = true
			break
		}
	}
	if !downloaded {
		return "", errors.New("下载 Caddy 失败，请检查网络连接")
	}

	if err := extractCaddyBinary(tmpPath, dest); err != nil {
		return "", fmt.Errorf("解压 Caddy 失败: %w", err)
	}
	return ver, nil
}

func fetchLatestCaddyVersion() string {
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/caddyserver/caddy/releases/latest")
	if err != nil {
		return defaultCaddyVersion
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return defaultCaddyVersion
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return defaultCaddyVersion
	}
	ver := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if ver == "" {
		return defaultCaddyVersion
	}
	return ver
}

func downloadDirectBinary(client *http.Client, dlURL, dest string) error {
	resp, err := client.Get(dlURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmpDest := dest + ".tmp"
	f, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil || n < 1024*1024 {
		_ = os.Remove(tmpDest)
		return errors.New("下载的 Caddy 二进制大小异常")
	}
	return os.Rename(tmpDest, dest)
}

func downloadTarGz(client *http.Client, dlURL, dest string) error {
	resp, err := client.Get(dlURL)
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

func extractCaddyBinary(tarGzPath, dest string) error {
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
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "caddy" {
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
			return os.Rename(tmpDest, dest)
		}
	}
	return errors.New("未在压缩包中找到 caddy 可执行文件")
}

func buildCaddyEnv(cfg config.Config) map[string]string {
	env := make(map[string]string)
	if cfg.Caddy.DNSToken != "" {
		env["CADDY_DNS_TOKEN"] = cfg.Caddy.DNSToken
	}
	if cfg.Caddy.DNSSecret != "" {
		env["CADDY_DNS_SECRET"] = cfg.Caddy.DNSSecret
	}
	return env
}

// ValidateCaddyfile 若本地已安装 caddy，则调用 caddy validate 检查生成的配置语法
func ValidateCaddyfile(content string, env map[string]string) error {
	bin := systemd.FindBinary("caddy")
	if bin == "" {
		return nil
	}
	tmp, err := os.CreateTemp("", "autof-caddyfile-*.conf")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, _ = tmp.WriteString(content)
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command(bin, "validate", "--config", tmpPath, "--adapter", "caddyfile")
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Caddyfile 语法校验未通过:\n%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// WriteAndApply 生成 Caddyfile、校验语法并在 Caddy 运行时执行零停机热重载
func WriteAndApply(store *config.Store, reloadIfRunning bool) error {
	cfg := store.Get()
	content := GenerateCaddyfile(cfg)
	env := buildCaddyEnv(cfg)

	if err := ValidateCaddyfile(content, env); err != nil {
		return err
	}

	caddyfilePath := store.CaddyfilePath()
	if err := os.WriteFile(caddyfilePath, []byte(content), 0600); err != nil {
		return err
	}

	bin := systemd.FindBinary("caddy")
	if bin != "" && os.Geteuid() == 0 {
		_ = systemd.WriteUnit(
			"caddy",
			"Caddy reverse proxy server managed by Autofrp",
			fmt.Sprintf("%s run --environ --config %s --adapter caddyfile", bin, caddyfilePath),
			fmt.Sprintf("%s reload --config %s --adapter caddyfile --force", bin, caddyfilePath),
			env,
		)
	}

	st := systemd.QueryStatus("caddy")
	if reloadIfRunning && st.Running && bin != "" {
		cmd := exec.Command(bin, "reload", "--config", caddyfilePath, "--adapter", "caddyfile")
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if err := cmd.Run(); err == nil {
			return nil
		}
		return systemd.Control("caddy", "restart", bin, []string{"run", "--config", caddyfilePath, "--adapter", "caddyfile"}, store.StateDir(), env)
	}
	return nil
}

// EnsureInstalledAndStarted 安装（若缺失）并启动 Caddy 服务
func EnsureInstalledAndStarted(store *config.Store) error {
	cfg := store.Get()
	if systemd.FindBinary("caddy") == "" {
		if _, err := InstallCaddy(cfg.Caddy.DNSProvider); err != nil {
			return err
		}
	}
	if err := WriteAndApply(store, false); err != nil {
		return err
	}
	bin := systemd.FindBinary("caddy")
	env := buildCaddyEnv(cfg)
	return systemd.Control("caddy", "restart", bin, []string{"run", "--config", store.CaddyfilePath(), "--adapter", "caddyfile"}, store.StateDir(), env)
}
