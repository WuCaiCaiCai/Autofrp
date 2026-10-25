package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WuCaiCaiCai/Autofrp/internal/caddy"
	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestParseNPMAndMigrateCerts(t *testing.T) {
	tmpDir := t.TempDir()
	hostDataDir := filepath.Join(tmpDir, "npm_data")
	hostLEDir := filepath.Join(tmpDir, "npm_letsencrypt")
	proxyHostDir := filepath.Join(hostDataDir, "nginx", "proxy_host")
	certLiveDir := filepath.Join(hostLEDir, "live", "npm-1")

	if err := os.MkdirAll(proxyHostDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(certLiveDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 模拟 NPM 生成的证书文件
	certFile := filepath.Join(certLiveDir, "fullchain.pem")
	keyFile := filepath.Join(certLiveDir, "privkey.pem")
	_ = os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\nFAKECERT\n-----END CERTIFICATE-----\n"), 0644)
	_ = os.WriteFile(keyFile, []byte("-----BEGIN PRIVATE KEY-----\nFAKEKEY\n-----END PRIVATE KEY-----\n"), 0600)

	// 模拟 NPM 的 /data/nginx/proxy_host/1.conf
	npmConf := `# ------------------------------------------------------------
# nas.example.com
# ------------------------------------------------------------
server {
  set $forward_scheme http;
  set $server         "192.168.1.50";
  set $port           5666;

  listen 80;
  listen 443 ssl http2;
  server_name nas.example.com;

  # Let's Encrypt SSL
  ssl_certificate /etc/letsencrypt/live/npm-1/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/npm-1/privkey.pem;

  location / {
    proxy_pass       $forward_scheme://$server:$port$request_uri;
  }
}
`
	confPath := filepath.Join(proxyHostDir, "1.conf")
	if err := os.WriteFile(confPath, []byte(npmConf), 0644); err != nil {
		t.Fatal(err)
	}

	storeDir := filepath.Join(tmpDir, "autof_etc")
	store, err := config.NewStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}

	pathMap := map[string]string{
		"/data":            hostDataDir,
		"/etc/letsencrypt": hostLEDir,
	}

	rules, certs := parseNginxAndNPMConfigs([]string{confPath}, pathMap, store.CertsDir())
	if len(rules) != 1 {
		t.Fatalf("expected 1 parsed NPM rule, got %d", len(rules))
	}
	if rules[0].Domain != "nas.example.com" || rules[0].Upstream != "192.168.1.50:5666" || !rules[0].HasSSL {
		t.Fatalf("unexpected NPM rule: %+v", rules[0])
	}
	if len(certs) != 1 || certs[0].SrcCertPath != certFile {
		t.Fatalf("unexpected certs: %+v", certs)
	}

	// 验证复制证书并写入 CaddyRule (custom TLSMode)
	cp := certs[0]
	_ = os.MkdirAll(filepath.Dir(cp.DestCertPath), 0755)
	if err := copyFile(cp.SrcCertPath, cp.DestCertPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(cp.SrcKeyPath, cp.DestKeyPath, 0600); err != nil {
		t.Fatal(err)
	}

	_ = store.Update(func(c *config.Config) error {
		c.Caddy.Rules = append(c.Caddy.Rules, config.CaddyRule{
			ID:       "r1",
			Name:     "nas.example.com",
			Listen:   "nas.example.com",
			Upstream: "192.168.1.50:5666",
			TLSMode:  "custom",
			CertFile: cp.DestCertPath,
			KeyFile:  cp.DestKeyPath,
			Enabled:  true,
		})
		return nil
	})

	caddyfile := caddy.GenerateCaddyfile(store.Get())
	if !strings.Contains(caddyfile, "tls "+cp.DestCertPath+" "+cp.DestKeyPath) {
		t.Fatalf("expected custom tls directive in Caddyfile:\n%s", caddyfile)
	}
}
