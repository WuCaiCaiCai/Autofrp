package frp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

// GenerateFrpsToml 生成远程 VPS 端 frps.toml 配置文件内容
func GenerateFrpsToml(cfg config.Config) string {
	var b strings.Builder
	b.WriteString("# frps 服务端配置（由 Autofrp 自动生成，在 VPS 上运行 frps）\n")
	switch cfg.Network.IPPref {
	case "6":
		b.WriteString("bindAddr = \"::\"\n")
	case "both":
		b.WriteString("bindAddr = \"::\"  # 双栈监听（需 net.ipv6.bindv6only=0）\n")
	default:
		b.WriteString("bindAddr = \"0.0.0.0\"\n")
	}
	b.WriteString(fmt.Sprintf("bindPort = %d\n", cfg.Frps.ControlPort))
	b.WriteString("auth.method = \"token\"\n")
	if cfg.Frps.Token != "" {
		b.WriteString(fmt.Sprintf("auth.token = %q\n", cfg.Frps.Token))
	}

	// 端口白名单策略
	allowEntries := buildAllowPortsEntries(cfg)
	if len(allowEntries) > 0 {
		b.WriteString("allowPorts = [\n")
		for _, entry := range allowEntries {
			b.WriteString("  " + entry + ",\n")
		}
		b.WriteString("]\n")
	}
	return b.String()
}

func buildAllowPortsEntries(cfg config.Config) []string {
	mode := cfg.Frps.AllowPortsMode
	if mode == "unlimited" || mode == "" {
		return nil
	}

	var entries []string
	singlePorts := make(map[int]bool)

	if mode == "range" && strings.TrimSpace(cfg.Frps.AllowPortsRange) != "" {
		parts := strings.FieldsFunc(cfg.Frps.AllowPortsRange, func(r rune) bool {
			return r == ',' || r == ';' || r == ' '
		})
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.Contains(p, "-") {
				bounds := strings.SplitN(p, "-", 2)
				start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
				if err1 == nil && err2 == nil && start > 0 && end >= start && end <= 65535 {
					entries = append(entries, fmt.Sprintf("{ start = %d, end = %d }", start, end))
				}
			} else if port, err := strconv.Atoi(p); err == nil && port > 0 && port <= 65535 {
				singlePorts[port] = true
			}
		}
	}

	// 无论是 range 还是 strict，都确保已登记的穿透服务端口在白名单内
	for _, svc := range cfg.Frps.Services {
		if svc.Enabled && svc.RemotePort > 0 && svc.RemotePort <= 65535 {
			singlePorts[svc.RemotePort] = true
		}
	}

	sortedPorts := make([]int, 0, len(singlePorts))
	for p := range singlePorts {
		sortedPorts = append(sortedPorts, p)
	}
	sort.Ints(sortedPorts)
	for _, p := range sortedPorts {
		entries = append(entries, fmt.Sprintf("{ single = %d }", p))
	}
	return entries
}

// GenerateFrpcTomlForExport 根据 VPS 端配置生成供导出/复制的客户端 frpc.toml
// 若 singleIdx >= 0，则仅生成指定序号的那一条穿透规则预览
func GenerateFrpcTomlForExport(cfg config.Config, singleIdx int) string {
	var b strings.Builder
	pubAddr := cfg.PublicAddr()
	if pubAddr == "" {
		pubAddr = "请填写VPS公网IP"
	}
	b.WriteString("# frpc 客户端配置（在内网/本地服务器上运行 frpc -c frpc.toml）\n")
	b.WriteString(fmt.Sprintf("serverAddr = %q\n", pubAddr))
	b.WriteString(fmt.Sprintf("serverPort = %d\n", cfg.Frps.ControlPort))
	b.WriteString("auth.method = \"token\"\n")
	if cfg.Frps.Token != "" {
		b.WriteString(fmt.Sprintf("auth.token = %q\n", cfg.Frps.Token))
	}
	b.WriteString("transport.tls.enable = true\n")
	if alt := cfg.PublicAddrAlt(); alt != "" {
		b.WriteString(fmt.Sprintf("# 备选 IPv6 地址: %s\n", alt))
	}
	b.WriteString("\n")

	if singleIdx >= 0 && singleIdx < len(cfg.Frps.Services) {
		writeProxyBlock(&b, cfg.Frps.Services[singleIdx], pubAddr)
		return b.String()
	}

	for _, svc := range cfg.Frps.Services {
		if !svc.Enabled {
			continue
		}
		writeProxyBlock(&b, svc, pubAddr)
		b.WriteString("\n")
	}
	return b.String()
}

// GenerateFrpcTomlForClient 生成本地服务器端实际运行的 frpc.toml（包含本地热重载管理端口）
func GenerateFrpcTomlForClient(cfg config.Config) string {
	var b strings.Builder
	b.WriteString("# frpc 本地客户端运行配置（由 Autofrp 自动维护）\n")
	b.WriteString(fmt.Sprintf("serverAddr = %q\n", cfg.Frpc.ServerAddr))
	b.WriteString(fmt.Sprintf("serverPort = %d\n", cfg.Frpc.ServerPort))
	b.WriteString("auth.method = \"token\"\n")
	if cfg.Frpc.Token != "" {
		b.WriteString(fmt.Sprintf("auth.token = %q\n", cfg.Frpc.Token))
	}
	b.WriteString(fmt.Sprintf("transport.tls.enable = %v\n", cfg.Frpc.TLSEnable))

	adminPort := cfg.Frpc.AdminPort
	if adminPort <= 0 {
		adminPort = 7400
	}
	b.WriteString("\n# 本地管理端口（仅本机监听，用于热重载 frpc reload）\n")
	b.WriteString("webServer.addr = \"127.0.0.1\"\n")
	b.WriteString(fmt.Sprintf("webServer.port = %d\n\n", adminPort))

	for _, tun := range cfg.Frpc.Tunnels {
		if !tun.Enabled {
			continue
		}
		writeProxyBlock(&b, tun, cfg.Frpc.ServerAddr)
		b.WriteString("\n")
	}
	return b.String()
}

func writeProxyBlock(b *strings.Builder, svc config.TunnelRule, pubAddr string) {
	if svc.Remark != "" {
		b.WriteString(fmt.Sprintf("# %s\n", svc.Remark))
	}
	if svc.CaddyDomain != "" {
		b.WriteString(fmt.Sprintf("# 域名反代访问: %s (内部穿透端口 %d)\n", svc.CaddyDomain, svc.RemotePort))
	} else if pubAddr != "" {
		b.WriteString(fmt.Sprintf("# 外部访问地址: %s:%d\n", pubAddr, svc.RemotePort))
	}
	b.WriteString("[[proxies]]\n")
	b.WriteString(fmt.Sprintf("name = %q\n", svc.Name))
	b.WriteString(fmt.Sprintf("type = %q\n", svc.Type))
	localIP := svc.LocalIP
	if localIP == "" {
		localIP = "127.0.0.1"
	}
	b.WriteString(fmt.Sprintf("localIP = %q\n", localIP))
	b.WriteString(fmt.Sprintf("localPort = %d\n", svc.LocalPort))
	b.WriteString(fmt.Sprintf("remotePort = %d\n", svc.RemotePort))
}
