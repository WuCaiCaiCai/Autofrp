package cert

import (
	"archive/tar"
	"compress/gzip"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

const acmeVersion = "3.1.6"

// Info 描述一张证书的基本信息（用于面板展示）
type Info struct {
	Domains   []string `json:"domains"`
	Issuer    string   `json:"issuer"`
	NotBefore string   `json:"notBefore"`
	NotAfter  string   `json:"notAfter"`
	DaysLeft  int      `json:"daysLeft"`
	CertPath  string   `json:"certPath"`
	KeyPath   string   `json:"keyPath"`
	Source    string   `json:"source"` // autof / caddy / panel
	ManagedID string   `json:"managedId,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func acmeRoot(store *config.Store) string   { return filepath.Join(store.StateDir(), "acme") }
func acmeScript(store *config.Store) string { return filepath.Join(acmeRoot(store), "acme.sh") }
func acmeHome(store *config.Store) string   { return filepath.Join(acmeRoot(store), "data") }

// Ensure 安装（若缺失）并返回 acme.sh 脚本路径
func Ensure(store *config.Store) (string, error) {
	script := acmeScript(store)
	if st, err := os.Stat(script); err == nil && !st.IsDir() {
		return script, nil
	}
	if os.Geteuid() != 0 {
		return "", errors.New("安装 acme.sh 需要 root 权限")
	}

	root := acmeRoot(store)
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(acmeHome(store), 0700); err != nil {
		return "", err
	}

	urls := []string{
		fmt.Sprintf("https://github.com/acmesh-official/acme.sh/archive/refs/tags/%s.tar.gz", acmeVersion),
		fmt.Sprintf("https://ghfast.top/https://github.com/acmesh-official/acme.sh/archive/refs/tags/%s.tar.gz", acmeVersion),
		"https://github.com/acmesh-official/acme.sh/archive/refs/heads/master.tar.gz",
		"https://ghfast.top/https://github.com/acmesh-official/acme.sh/archive/refs/heads/master.tar.gz",
	}

	client := &http.Client{Timeout: 120 * time.Second}
	var lastErr error
	for _, u := range urls {
		tmp, err := os.CreateTemp("", "autof-acme-*.tar.gz")
		if err != nil {
			return "", err
		}
		tmpPath := tmp.Name()
		tmp.Close()

		err = download(client, u, tmpPath)
		if err == nil {
			err = extractTarGz(tmpPath, root)
		}
		os.Remove(tmpPath)
		if err == nil {
			if st, statErr := os.Stat(script); statErr == nil && !st.IsDir() {
				_ = os.Chmod(script, 0755)
				return script, nil
			}
			err = errors.New("压缩包内未找到 acme.sh")
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("下载 acme.sh 失败")
	}
	return "", fmt.Errorf("安装 acme.sh 失败: %w", lastErr)
}

func download(client *http.Client, url, dest string) error {
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

// extractTarGz 解压并去掉压缩包的顶层目录（acme.sh-3.1.6/）
func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
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
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		parts := strings.SplitN(name, string(os.PathSeparator), 2)
		if len(parts) < 2 || parts[1] == "" {
			continue // 跳过顶层目录本身
		}
		rel := parts[1]
		target := filepath.Join(dest, rel)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				continue
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0777)
			if err != nil {
				continue
			}
			_, _ = io.Copy(out, tr)
			out.Close()
		}
	}
	return nil
}

func providerHook(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "cloudflare":
		return "dns_cf"
	case "alidns":
		return "dns_ali"
	case "dnspod":
		return "dns_dp"
	case "duckdns":
		return "dns_duckdns"
	default:
		return ""
	}
}

func providerEnv(provider string, cfg config.CaddyConfig) (map[string]string, error) {
	token := strings.TrimSpace(cfg.DNSToken)
	secret := strings.TrimSpace(cfg.DNSSecret)
	env := map[string]string{}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "cloudflare":
		if token == "" {
			return nil, errors.New("Cloudflare 需要 API Token")
		}
		env["CF_Token"] = token
		if secret != "" {
			env["CF_Account_ID"] = secret
		}
	case "alidns":
		if token == "" || secret == "" {
			return nil, errors.New("阿里云 DNS 需要 AccessKeyId 与 AccessKeySecret")
		}
		env["Ali_Key"] = token
		env["Ali_Secret"] = secret
	case "dnspod":
		if token == "" || secret == "" {
			return nil, errors.New("DNSPod 需要 ID 与 Token（分别填入 Token / Secret）")
		}
		env["DP_Id"] = token
		env["DP_Key"] = secret
	case "duckdns":
		if token == "" {
			return nil, errors.New("DuckDNS 需要 Token")
		}
		env["DuckDNS_Token"] = token
	default:
		return nil, fmt.Errorf("不支持的 DNS 服务商: %s", provider)
	}
	return env, nil
}

// NormalizeDomains 补全泛域名所需的主域名，并做基础校验
func NormalizeDomains(domains []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		d = strings.TrimSuffix(d, ".")
		if d == "" {
			continue
		}
		if strings.ContainsAny(d, " /\\") {
			return nil, fmt.Errorf("域名格式不正确: %s", d)
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
		if strings.HasPrefix(d, "*.") {
			base := strings.TrimPrefix(d, "*.")
			if !seen[base] {
				seen[base] = true
				out = append(out, base)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("请至少填写一个域名")
	}
	return out, nil
}

func primaryDomain(domains []string) string {
	for _, d := range domains {
		if !strings.HasPrefix(d, "*.") {
			return d
		}
	}
	return strings.TrimPrefix(domains[0], "*.")
}

// Issue 使用 acme.sh 通过 DNS-01 签发（或强制重签）证书，并安装到目标目录
func Issue(store *config.Store, entry config.CertEntry, dns config.CaddyConfig, force bool) error {
	script, err := Ensure(store)
	if err != nil {
		return err
	}
	hook := providerHook(entry.Provider)
	if hook == "" {
		return fmt.Errorf("不支持的 DNS 服务商: %s", entry.Provider)
	}
	envMap, err := providerEnv(entry.Provider, dns)
	if err != nil {
		return err
	}

	domains, err := NormalizeDomains(entry.Domains)
	if err != nil {
		return err
	}

	keyType := entry.KeyType
	if keyType == "" {
		keyType = "ec-256"
	}
	isEC := strings.HasPrefix(keyType, "ec")

	home := acmeHome(store)
	args := []string{"--issue", "--home", home, "--server", "letsencrypt", "--dns", hook, "--keylength", keyType}
	if email := strings.TrimSpace(dns.Email); email != "" {
		args = append(args, "--accountemail", email)
	}
	for _, d := range domains {
		args = append(args, "-d", d)
	}
	if force {
		args = append(args, "--force")
	}

	if out, err := runAcme(script, envMap, args...); err != nil {
		return fmt.Errorf("acme.sh 签发失败: %s", tail(out, err))
	}

	targetDir := entry.TargetDir
	if targetDir == "" {
		return errors.New("证书输出目录为空")
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	certPath := filepath.Join(targetDir, "fullchain.pem")
	keyPath := filepath.Join(targetDir, "privkey.pem")

	installArgs := []string{"--install-cert", "--home", home, "-d", primaryDomain(domains), "--key-file", keyPath, "--fullchain-file", certPath}
	if isEC {
		installArgs = append(installArgs, "--ecc")
	}
	if out, err := runAcme(script, envMap, installArgs...); err != nil {
		return fmt.Errorf("acme.sh 安装证书失败: %s", tail(out, err))
	}

	_ = os.Chmod(certPath, 0644)
	_ = os.Chmod(keyPath, 0600)
	return nil
}

// Renew 立即续期指定证书
func Renew(store *config.Store, entry config.CertEntry, dns config.CaddyConfig) error {
	script, err := Ensure(store)
	if err != nil {
		return err
	}
	envMap, err := providerEnv(entry.Provider, dns)
	if err != nil {
		return err
	}
	domains, err := NormalizeDomains(entry.Domains)
	if err != nil {
		return err
	}
	args := []string{"--renew", "--home", acmeHome(store), "-d", primaryDomain(domains), "--force"}
	if strings.HasPrefix(entry.KeyType, "ec") {
		args = append(args, "--ecc")
	}
	out, err := runAcme(script, envMap, args...)
	if err != nil {
		return fmt.Errorf("续期失败: %s", tail(out, err))
	}
	return nil
}

// RenewAll 由面板定时调用：续期所有 acme.sh 证书
func RenewAll(store *config.Store) (string, error) {
	script := acmeScript(store)
	if _, err := os.Stat(script); err != nil {
		return "", nil
	}
	out, err := runAcme(script, nil, "--cron", "--home", acmeHome(store))
	if err != nil {
		return out, fmt.Errorf("自动续期任务失败: %s", tail(out, err))
	}
	return out, nil
}

func runAcme(script string, envMap map[string]string, args ...string) (string, error) {
	cmd := exec.Command(script, args...)
	cmd.Env = os.Environ()
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tail(out string, err error) string {
	text := strings.TrimSpace(out)
	if text == "" {
		if err != nil {
			return err.Error()
		}
		return "未知错误"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, " | ")
}

// ReadInfo 解析证书文件，返回域名、签发者与到期时间
func ReadInfo(certPath, keyPath, source string) Info {
	info := Info{CertPath: certPath, KeyPath: keyPath, Source: source}
	data, err := os.ReadFile(certPath)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	var block *pem.Block
	rest := data
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			break
		}
	}
	if block == nil {
		info.Error = "未找到 CERTIFICATE 数据"
		return info
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Domains = c.DNSNames
	if len(info.Domains) == 0 && c.Subject.CommonName != "" {
		info.Domains = []string{c.Subject.CommonName}
	}
	if c.Issuer.CommonName != "" {
		info.Issuer = c.Issuer.CommonName
	} else {
		info.Issuer = c.Issuer.String()
	}
	info.NotBefore = c.NotBefore.Format(time.RFC3339)
	info.NotAfter = c.NotAfter.Format(time.RFC3339)
	info.DaysLeft = int(time.Until(c.NotAfter).Hours() / 24)
	return info
}

// Discover 扫描本机已有证书（Autofrp 托管 / Caddy 存储 / 面板自签）
func Discover(store *config.Store) []Info {
	out := []Info{}
	seen := map[string]bool{}

	add := func(certPath, keyPath, source string) {
		certPath = filepath.Clean(certPath)
		if seen[certPath] {
			return
		}
		if st, err := os.Stat(certPath); err != nil || st.IsDir() {
			return
		}
		seen[certPath] = true
		out = append(out, ReadInfo(certPath, keyPath, source))
	}

	// 1. Autofrp 证书目录
	if matches, err := filepath.Glob(filepath.Join(store.CertsDir(), "*", "fullchain.pem")); err == nil {
		for _, m := range matches {
			add(m, filepath.Join(filepath.Dir(m), "privkey.pem"), "autof")
		}
	}
	// 2. 面板自签证书
	add(filepath.Join(store.StateDir(), "tls", "panel.crt"), filepath.Join(store.StateDir(), "tls", "panel.key"), "panel")

	// 3. Caddy 内部存储（系统包 / root 运行 / 容器挂载）
	for _, base := range []string{
		"/var/lib/caddy/.local/share/caddy/certificates",
		"/root/.local/share/caddy/certificates",
		"/etc/autof/caddy-data/certificates",
	} {
		if matches, err := filepath.Glob(filepath.Join(base, "*", "*", "*.crt")); err == nil {
			for _, m := range matches {
				add(m, strings.TrimSuffix(m, ".crt")+".key", "caddy")
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].DaysLeft != out[j].DaysLeft {
			return out[i].DaysLeft < out[j].DaysLeft
		}
		return out[i].CertPath < out[j].CertPath
	})
	return out
}

// MatchManaged 将发现的证书关联到托管条目（按输出目录匹配）
func MatchManaged(infos []Info, entries []config.CertEntry) []Info {
	dirMap := map[string]string{}
	for _, e := range entries {
		if e.TargetDir != "" {
			dirMap[filepath.Clean(e.TargetDir)] = e.ID
		}
	}
	for i := range infos {
		if id, ok := dirMap[filepath.Clean(filepath.Dir(infos[i].CertPath))]; ok {
			infos[i].ManagedID = id
		}
	}
	return infos
}

// Installed 返回 acme.sh 是否已安装
func Installed(store *config.Store) bool {
	st, err := os.Stat(acmeScript(store))
	return err == nil && !st.IsDir()
}

// Remove 从 acme.sh 的续期列表中移除证书（不删除已安装文件）
func Remove(store *config.Store, entry config.CertEntry) error {
	script := acmeScript(store)
	if _, err := os.Stat(script); err != nil {
		return nil
	}
	domains, err := NormalizeDomains(entry.Domains)
	if err != nil {
		return err
	}
	args := []string{"--remove", "--home", acmeHome(store), "-d", primaryDomain(domains)}
	if strings.HasPrefix(entry.KeyType, "ec") {
		args = append(args, "--ecc")
	}
	_, _ = runAcme(script, nil, args...)
	return nil
}

// PurgeFiles 删除证书输出目录（仅允许 /etc/autof 下的目录，避免误删系统文件）
func PurgeFiles(store *config.Store, entry config.CertEntry) error {
	dir := filepath.Clean(entry.TargetDir)
	stateDir := filepath.Clean(store.StateDir())
	if dir == "" || dir == "/" || len(dir) < len(stateDir)+1 {
		return errors.New("输出目录不合法，拒绝删除")
	}
	if !strings.HasPrefix(dir, stateDir+string(os.PathSeparator)) {
		return errors.New("出于安全考虑，只允许删除 " + stateDir + " 下的证书目录")
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}
