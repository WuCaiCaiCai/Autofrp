package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyV1AndCodec(t *testing.T) {
	tmpDir := t.TempDir()

	stateContent := `CONTROL_PORT=30085
TOKEN=secretToken123456
FRP_VERSION=0.61.1
PUBLIC_IP=108.165.122.146
V4_STATUS='NAT IPv4'
IP_PREF=4
`
	svcContent := `mc-java|tcp|25565|30008|MC服务器
mc-bedrock|udp|19132|30009|基岩版
`
	if err := os.WriteFile(filepath.Join(tmpDir, "state.conf"), []byte(stateContent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "services.conf"), []byte(svcContent), 0600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore error: %v", err)
	}
	cfg := store.Get()
	if cfg.Frps.ControlPort != 30085 {
		t.Fatalf("expected control port 30085, got %d", cfg.Frps.ControlPort)
	}
	if cfg.Frps.Token != "secretToken123456" {
		t.Fatalf("expected token secretToken123456, got %s", cfg.Frps.Token)
	}
	if len(cfg.Frps.Services) != 2 {
		t.Fatalf("expected 2 migrated services, got %d", len(cfg.Frps.Services))
	}
	if cfg.Frps.Services[0].Name != "mc-java" || cfg.Frps.Services[0].RemotePort != 30008 {
		t.Fatalf("unexpected service[0]: %+v", cfg.Frps.Services[0])
	}

	// 测试连接码导出与导入
	code, err := cfg.ExportConnectionCode()
	if err != nil {
		t.Fatalf("ExportConnectionCode error: %v", err)
	}
	bundle, err := ParseImportInput(code)
	if err != nil {
		t.Fatalf("ParseImportInput(code) error: %v", err)
	}
	if bundle.ServerAddr != "108.165.122.146" || bundle.ServerPort != 30085 || len(bundle.Tunnels) != 2 {
		t.Fatalf("unexpected bundle: %+v", bundle)
	}

	// 测试联动 Caddy 反代自动同步
	err = store.Update(func(c *Config) error {
		c.Frps.Services[0].CaddyDomain = "mc.example.com"
		c.Frps.Services[0].CaddyTLSMode = "auto"
		return nil
	})
	if err != nil {
		t.Fatalf("store.Update error: %v", err)
	}
	cfgAfter := store.Get()
	if len(cfgAfter.Caddy.Rules) != 1 {
		t.Fatalf("expected 1 linked caddy rule, got %d", len(cfgAfter.Caddy.Rules))
	}
	if cfgAfter.Caddy.Rules[0].Listen != "mc.example.com" || cfgAfter.Caddy.Rules[0].Upstream != "127.0.0.1:30008" {
		t.Fatalf("unexpected linked caddy rule: %+v", cfgAfter.Caddy.Rules[0])
	}
}

func TestParseFrpcToml(t *testing.T) {
	sample := `# frpc 客户端配置
serverAddr = "203.0.113.10"
serverPort = 7000
auth.method = "token"
auth.token = "myTokenabc"
transport.tls.enable = true

[[proxies]]
name = "web-nas"
type = "tcp"
localIP = "192.168.1.50"
localPort = 5000
remotePort = 15000
`
	b, err := ParseImportInput(sample)
	if err != nil {
		t.Fatalf("ParseImportInput(toml) error: %v", err)
	}
	if b.ServerAddr != "203.0.113.10" || b.ServerPort != 7000 || b.Token != "myTokenabc" {
		t.Fatalf("unexpected header: %+v", b)
	}
	if len(b.Tunnels) != 1 || b.Tunnels[0].LocalIP != "192.168.1.50" || b.Tunnels[0].RemotePort != 15000 {
		t.Fatalf("unexpected proxy: %+v", b.Tunnels)
	}
}
