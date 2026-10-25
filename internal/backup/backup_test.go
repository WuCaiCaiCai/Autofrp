package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestCreateInspectRestore(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(c *config.Config) error {
		c.Frps.Token = "secretToken"
		c.Frps.ControlPort = 30085
		c.Web.Password = "webpass"
		c.Caddy.DNSToken = "cf-token"
		return nil
	})
	// 一张证书（含私钥）
	certDir := filepath.Join(store.CertsDir(), "example.com")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(certDir, "fullchain.pem"), []byte("CERT"), 0o644)
	_ = os.WriteFile(filepath.Join(certDir, "privkey.pem"), []byte("KEY"), 0o600)

	archive, err := Create(store, Options{IncludeSecrets: true, IncludeLegacy: false})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive)

	m, err := Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if m.App != "autofrp" || !m.HasSecrets || m.Files < 3 {
		t.Fatalf("manifest 异常: %+v", m)
	}

	// 修改当前配置后恢复
	_ = store.Update(func(c *config.Config) error {
		c.Frps.Token = "changed"
		c.Frps.ControlPort = 1234
		c.Web.Password = "newpass"
		return nil
	})
	cfg, err := Restore(store, archive, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Frps.Token != "secretToken" || cfg.Frps.ControlPort != 30085 || cfg.Web.Password != "webpass" {
		t.Fatalf("恢复结果不正确: %+v", cfg.Frps)
	}
	if cfg.Caddy.DNSToken != "cf-token" {
		t.Fatal("DNS Token 未恢复")
	}
	if data, err := os.ReadFile(filepath.Join(certDir, "privkey.pem")); err != nil || string(data) != "KEY" {
		t.Fatal("私钥未恢复")
	}

	// keepCreds 时保留当前面板密码
	current := store.Get().Web.Password
	_ = store.Update(func(c *config.Config) error {
		c.Web.Password = "keep-me"
		return nil
	})
	cfg2, err := Restore(store, archive, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Web.Password != "keep-me" || cfg2.Web.Password == current {
		t.Fatalf("keepCreds 未生效: %q", cfg2.Web.Password)
	}
}

func TestSanitizedBackupOmitsSecrets(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(c *config.Config) error {
		c.Frps.Token = "topsecret"
		c.Web.Password = "pass"
		return nil
	})
	archive, err := Create(store, Options{IncludeSecrets: false})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive)

	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	if m.HasSecrets {
		t.Fatal("脱敏备份不应标记包含密钥")
	}
}

func TestSafeName(t *testing.T) {
	allowed := []string{"config.json", "certs/a/b/fullchain.pem", "tls/panel.crt", "clients/frpc.toml"}
	for _, n := range allowed {
		if !safeName(n) {
			t.Fatalf("应允许: %s", n)
		}
	}
	blocked := []string{"../etc/passwd", "/etc/passwd", "config.json/../../x", "other/file"}
	for _, n := range blocked {
		if safeName(n) {
			t.Fatalf("应拒绝: %s", n)
		}
	}
}
