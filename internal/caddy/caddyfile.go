package caddy

import (
	"fmt"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

// GenerateCaddyfile 根据结构化配置生成完整 Caddyfile 内容
func GenerateCaddyfile(cfg config.Config) string {
	var b strings.Builder
	b.WriteString("# Caddyfile 配置（由 Autofrp 自动生成与维护）\n")

	// 全局配置块
	hasGlobal := cfg.Caddy.Email != "" ||
		(cfg.Caddy.HTTPPort > 0 && cfg.Caddy.HTTPPort != 80) ||
		(cfg.Caddy.HTTPSPort > 0 && cfg.Caddy.HTTPSPort != 443) ||
		strings.TrimSpace(cfg.Caddy.CustomGlobal) != ""

	if hasGlobal {
		b.WriteString("{\n")
		if cfg.Caddy.Email != "" {
			b.WriteString(fmt.Sprintf("\temail %s\n", strings.TrimSpace(cfg.Caddy.Email)))
		}
		if cfg.Caddy.HTTPPort > 0 && cfg.Caddy.HTTPPort != 80 {
			b.WriteString(fmt.Sprintf("\thttp_port %d\n", cfg.Caddy.HTTPPort))
		}
		if cfg.Caddy.HTTPSPort > 0 && cfg.Caddy.HTTPSPort != 443 {
			b.WriteString(fmt.Sprintf("\thttps_port %d\n", cfg.Caddy.HTTPSPort))
		}
		if custom := strings.TrimSpace(cfg.Caddy.CustomGlobal); custom != "" {
			for _, line := range strings.Split(custom, "\n") {
				b.WriteString("\t" + strings.TrimSpace(line) + "\n")
			}
		}
		b.WriteString("}\n\n")
	}

	activeCount := 0
	for _, r := range cfg.Caddy.Rules {
		if !r.Enabled || strings.TrimSpace(r.Listen) == "" || strings.TrimSpace(r.Upstream) == "" {
			continue
		}
		activeCount++
		writeRuleBlock(&b, r, cfg.Caddy)
		b.WriteString("\n")
	}

	if activeCount == 0 {
		b.WriteString("# 暂无启用的反代规则，请在 Autofrp WebUI 或命令行中添加\n")
	}

	return b.String()
}

func writeRuleBlock(b *strings.Builder, r config.CaddyRule, caddyCfg config.CaddyConfig) {
	if r.Name != "" || r.Remark != "" {
		b.WriteString(fmt.Sprintf("# %s", r.Name))
		if r.Remark != "" && r.Remark != r.Name {
			b.WriteString(fmt.Sprintf(" (%s)", r.Remark))
		}
		b.WriteString("\n")
	}

	listenAddr := formatListenAddress(r.Listen, r.TLSMode)
	b.WriteString(fmt.Sprintf("%s {\n", listenAddr))

	// TLS 策略
	switch r.TLSMode {
	case "internal":
		b.WriteString("\ttls internal\n")
	case "dns":
		writeDNSTLSBlock(b, caddyCfg)
	case "http":
		// 已在 listenAddr 通过 http:// 声明纯 HTTP
	default:
		// "auto" 使用 Caddy 默认 ACME 自动证书
	}

	if r.EnableEncode {
		b.WriteString("\tencode zstd gzip\n")
	}

	upstream := strings.TrimSpace(r.Upstream)
	if r.SkipTLSVerify && !strings.HasPrefix(upstream, "http://") && !strings.HasPrefix(upstream, "https://") {
		upstream = "https://" + upstream
	}

	if r.SkipTLSVerify || strings.HasPrefix(upstream, "https://") {
		b.WriteString(fmt.Sprintf("\treverse_proxy %s {\n", upstream))
		b.WriteString("\t\theader_up Host {upstream_hostport}\n")
		if r.SkipTLSVerify {
			b.WriteString("\t\ttransport http {\n")
			b.WriteString("\t\t\ttls_insecure_skip_verify\n")
			b.WriteString("\t\t}\n")
		}
		b.WriteString("\t}\n")
	} else {
		b.WriteString(fmt.Sprintf("\treverse_proxy %s\n", upstream))
	}

	if extra := strings.TrimSpace(r.ExtraDirectives); extra != "" {
		for _, line := range strings.Split(extra, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				b.WriteString("\t" + line + "\n")
			}
		}
	}

	b.WriteString("}\n")
}

func formatListenAddress(listen, tlsMode string) string {
	listen = strings.TrimSpace(listen)
	if tlsMode == "http" {
		if strings.HasPrefix(listen, "https://") {
			listen = "http://" + strings.TrimPrefix(listen, "https://")
		} else if !strings.HasPrefix(listen, "http://") {
			listen = "http://" + listen
		}
		return listen
	}
	if tlsMode == "internal" || tlsMode == "auto" || tlsMode == "dns" {
		if strings.HasPrefix(listen, ":") && tlsMode != "http" {
			return "https://" + listen
		}
	}
	return listen
}

func writeDNSTLSBlock(b *strings.Builder, caddyCfg config.CaddyConfig) {
	provider := strings.ToLower(strings.TrimSpace(caddyCfg.DNSProvider))
	if provider == "" || strings.TrimSpace(caddyCfg.DNSToken) == "" {
		b.WriteString("\t# 警告: 已选择 DNS 证书模式但未配置 DNS API Token，回退为默认自动证书\n")
		return
	}
	switch provider {
	case "cloudflare":
		b.WriteString("\ttls {\n")
		b.WriteString("\t\tdns cloudflare {env.CADDY_DNS_TOKEN}\n")
		b.WriteString("\t}\n")
	case "alidns":
		b.WriteString("\ttls {\n")
		b.WriteString("\t\tdns alidns {\n")
		b.WriteString("\t\t\taccess_key_id {env.CADDY_DNS_TOKEN}\n")
		b.WriteString("\t\t\taccess_key_secret {env.CADDY_DNS_SECRET}\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
	case "dnspod":
		b.WriteString("\ttls {\n")
		b.WriteString("\t\tdns dnspod {env.CADDY_DNS_TOKEN}\n")
		b.WriteString("\t}\n")
	case "duckdns":
		b.WriteString("\ttls {\n")
		b.WriteString("\t\tdns duckdns {env.CADDY_DNS_TOKEN}\n")
		b.WriteString("\t}\n")
	default:
		b.WriteString(fmt.Sprintf("\ttls {\n\t\tdns %s {env.CADDY_DNS_TOKEN}\n\t}\n", provider))
	}
}
