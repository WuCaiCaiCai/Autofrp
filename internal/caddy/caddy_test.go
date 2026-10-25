package caddy

import (
	"strings"
	"testing"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestGenerateCaddyfileScenarios(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Caddy.Email = "admin@example.com"
	cfg.Caddy.DNSProvider = "cloudflare"
	cfg.Caddy.DNSToken = "cf_secret_token"
	cfg.Caddy.Rules = []config.CaddyRule{
		{
			ID:           "r1",
			Name:         "公网穿透联动反代",
			Listen:       "nas.example.com",
			Upstream:     "127.0.0.1:15666",
			TLSMode:      "auto",
			EnableEncode: true,
			Enabled:      true,
		},
		{
			ID:            "r2",
			Name:          "泛域名 DNS 证书反代",
			Listen:        "*.home.example.com:8443",
			Upstream:      "192.168.1.50:8443",
			TLSMode:       "dns",
			SkipTLSVerify: true,
			EnableEncode:  true,
			Enabled:       true,
		},
		{
			ID:           "r3",
			Name:         "局域网内网自签证书",
			Listen:       "192.168.1.100:9443",
			Upstream:     "127.0.0.1:9000",
			TLSMode:      "internal",
			EnableEncode: false,
			Enabled:      true,
		},
		{
			ID:           "r4",
			Name:         "纯 HTTP 端口转发",
			Listen:       ":8080",
			Upstream:     "192.168.1.20:3000",
			TLSMode:      "http",
			EnableEncode: false,
			Enabled:      true,
		},
	}

	out := GenerateCaddyfile(*cfg)

	expectedSnippets := []string{
		"email admin@example.com",
		"nas.example.com {",
		"encode zstd gzip",
		"reverse_proxy 127.0.0.1:15666",
		"dns cloudflare {env.CADDY_DNS_TOKEN}",
		"tls_insecure_skip_verify",
		"tls internal",
		"http://:8080 {",
	}
	for _, snip := range expectedSnippets {
		if !strings.Contains(out, snip) {
			t.Fatalf("expected snippet %q in generated Caddyfile:\n%s", snip, out)
		}
	}
}
