package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/caddy"
	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/frp"
	"github.com/WuCaiCaiCai/Autofrp/internal/netenv"
	"github.com/WuCaiCaiCai/Autofrp/internal/scanner"
	"github.com/WuCaiCaiCai/Autofrp/internal/server"
	"github.com/WuCaiCaiCai/Autofrp/internal/systemd"
)

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
)

var stdinReader = bufio.NewReader(os.Stdin)

// Run 执行命令行入口逻辑
func Run(args []string) error {
	store, err := config.NewStore("")
	if err != nil {
		return err
	}

	// 若尚未探测过网络，首次自动探测
	cfg := store.Get()
	if cfg.Network.PublicIPv4 == "" && cfg.Network.PublicIPv6 == "" && cfg.Network.LocalIPv4 == "" {
		netCfg := cfg.Network
		netenv.DetectAndApply(&netCfg)
		_ = store.Update(func(c *config.Config) error {
			c.Network = netCfg
			return nil
		})
	}

	if len(args) == 0 {
		return interactiveMain(store)
	}

	cmd := args[0]
	switch cmd {
	case "menu", "manage":
		return interactiveMain(store)

	case "web":
		listenAddr := ""
		for i := 1; i < len(args); i++ {
			if (args[i] == "--listen" || args[i] == "-l") && i+1 < len(args) {
				listenAddr = args[i+1]
				i++
			}
		}
		return runWebDaemon(store, listenAddr)

	case "web-install":
		return installWebService(store)

	case "status":
		printDashboardHeader(store)
		return nil

	case "detect", "scan":
		printDetectedServices(store)
		return nil

	case "takeover":
		if len(args) < 2 {
			printDetectedServices(store)
			return fmt.Errorf("用法: autof takeover <服务ID，如 docker-npm / host-nginx / host-caddy / host-frps>")
		}
		return handleCLITakeover(store, args[1])

	case "list":
		printAllRules(store)
		return nil

	case "gen":
		target := "frps"
		if len(args) > 1 {
			target = args[1]
		}
		return handleGen(store, target)

	case "export":
		return handleExport(store)

	case "import":
		if len(args) < 2 {
			return fmt.Errorf("用法: autof import <autof://连接码 或 frpc.toml路径>")
		}
		return handleImport(store, args[1])

	case "install":
		target := "all"
		if len(args) > 1 {
			target = args[1]
		}
		return handleInstall(store, target)

	case "net":
		if len(args) < 2 {
			return fmt.Errorf("用法: autof net 4|6|both")
		}
		pref := args[1]
		if pref != "4" && pref != "6" && pref != "both" {
			return fmt.Errorf("网络偏好只能是 4、6 或 both")
		}
		_ = store.Update(func(c *config.Config) error {
			c.Network.IPPref = pref
			return nil
		})
		_ = frp.ApplyFrps(store, true)
		fmt.Printf("%s[+]%s 网络偏好已设为: %s\n", cGreen, cReset, store.Get().NetPrefLabel())
		return nil

	case "pwd":
		newPwd := ""
		if len(args) > 1 {
			newPwd = args[1]
		}
		return handlePwd(store, newPwd)

	case "start", "stop", "restart", "reload", "logs":
		target := "frps"
		if len(args) > 1 {
			target = args[1]
		}
		return handleServiceCtl(store, cmd, target)

	case "uninstall":
		return uninstallAll(store)

	case "version", "-v", "--version":
		fmt.Printf("Autofrp v%s\n", config.AppVersion)
		return nil

	case "help", "-h", "--help":
		printUsage(store.StateDir())
		return nil

	default:
		printUsage(store.StateDir())
		return fmt.Errorf("未知命令: %s", cmd)
	}
}

func runWebDaemon(store *config.Store, listenOverride string) error {
	_ = frp.WriteConfigs(store)
	_ = caddy.WriteAndApply(store, false)
	cfg := store.Get()
	// 首次启动时后台异步探测网络，避免阻塞 WebUI 首屏
	if cfg.Network.PublicIPv4 == "" && cfg.Network.PublicIPv6 == "" && cfg.Network.LocalIPv4 == "" {
		go func() {
			netCfg := cfg.Network
			netenv.DetectAndApply(&netCfg)
			_ = store.Update(func(c *config.Config) error {
				c.Network = netCfg
				return nil
			})
			_ = frp.WriteConfigs(store)
		}()
	}
	addr := cfg.Web.ListenAddr
	if listenOverride != "" {
		addr = listenOverride
	}
	displayHost := cfg.PublicAddr()
	if displayHost == "" {
		displayHost = cfg.Network.LocalIPv4
	}
	if displayHost == "" {
		displayHost = "127.0.0.1"
	}
	port := "7500"
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		port = addr[idx+1:]
	}
	fmt.Printf("%s[*]%s Autofrp WebUI 已启动:\n", cCyan, cReset)
	fmt.Printf("    访问地址: %shttp://%s:%s%s\n", cBold, displayHost, port, cReset)
	if cfg.Web.TLSListen != "" {
		tlsPort := "7443"
		if idx := strings.LastIndex(cfg.Web.TLSListen, ":"); idx != -1 {
			tlsPort = cfg.Web.TLSListen[idx+1:]
		}
		fmt.Printf("    HTTPS   : %shttps://%s:%s%s\n", cBold, displayHost, tlsPort, cReset)
	}
	fmt.Printf("    用 户 名: %s%s%s\n", cBold, cfg.Web.Username, cReset)
	fmt.Printf("    密    码: %s%s%s\n", cBold, cfg.Web.Password, cReset)
	srv := server.New(store)
	return srv.Start(addr)
}

func installWebService(store *config.Store) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("安装系统服务需要 root 权限，请用 sudo 运行")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	targetBin := "/usr/local/bin/autof"
	if exe != targetBin {
		if data, err := os.ReadFile(exe); err == nil {
			_ = os.WriteFile(targetBin, data, 0755)
			exe = targetBin
		}
	}
	if err := systemd.WriteUnit(
		"autof",
		"Autofrp WebUI & Controller Daemon",
		fmt.Sprintf("%s web", exe),
		"",
		nil,
	); err != nil {
		return err
	}
	if err := systemd.Control("autof", "restart", exe, []string{"web"}, store.StateDir(), nil); err != nil {
		return err
	}
	cfg := store.Get()
	host := cfg.PublicAddr()
	if host == "" {
		host = cfg.Network.LocalIPv4
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port := "7500"
	if idx := strings.LastIndex(cfg.Web.ListenAddr, ":"); idx != -1 {
		port = cfg.Web.ListenAddr[idx+1:]
	}
	fmt.Printf("%s[+]%s Autofrp WebUI 后台服务已启动！\n", cGreen, cReset)
	fmt.Printf("    浏览器访问: %shttp://%s:%s%s\n", cCyan+cBold, host, port, cReset)
	if cfg.Web.TLSListen != "" {
		tlsPort := "7443"
		if idx := strings.LastIndex(cfg.Web.TLSListen, ":"); idx != -1 {
			tlsPort = cfg.Web.TLSListen[idx+1:]
		}
		fmt.Printf("    HTTPS 访问: %shttps://%s:%s%s\n", cCyan+cBold, host, tlsPort, cReset)
	}
	fmt.Printf("    登录用户名: %s%s%s\n", cBold, cfg.Web.Username, cReset)
	fmt.Printf("    登录密  码: %s%s%s\n", cBold, cfg.Web.Password, cReset)
	return nil
}

func printDashboardHeader(store *config.Store) {
	cfg := store.Get()
	sysInfo := netenv.DetectSystemInfo()
	stFrps := systemd.QueryStatus("frps")
	stFrpc := systemd.QueryStatus("frpc")
	stCaddy := systemd.QueryStatus("caddy")
	stWeb := systemd.QueryStatus("autof")

	rule := strings.Repeat("═", 64)
	fmt.Printf("%s%s%s\n", cCyan, rule, cReset)
	fmt.Printf("  %sAutofrp%s · 穿透 (frps/frpc) & Caddy 反代管理  %sv%s%s\n", cBold, cReset, cDim, config.AppVersion, cReset)
	fmt.Printf("  %s系统环境%s  %s (%s / %s)\n", cCyan, cReset, sysInfo.PrettyName, sysInfo.Arch, sysInfo.InitSystem)
	fmt.Printf("%s%s%s\n", cCyan, rule, cReset)
	fmt.Printf("  %s公网 IPv4%s  %-10s %s\n", cCyan, cReset, cfg.Network.V4Status, cfg.Network.PublicIPv4)
	fmt.Printf("  %s公网 IPv6%s  %-10s %s\n", cCyan, cReset, cfg.Network.V6Status, cfg.Network.PublicIPv6)
	fmt.Printf("  %s本机内网%s   %-10s 网络偏好: %s\n", cCyan, cReset, cfg.Network.LocalIPv4, cfg.NetPrefLabel())
	fmt.Printf("  %s── 组件状态 ──%s\n", cDim, cReset)
	fmt.Printf("  frps (VPS 服务端) : %s  (控制端口 %d)\n", formatStatus(stFrps), cfg.Frps.ControlPort)
	fmt.Printf("  frpc (本地客户端) : %s  (目标 %s:%d)\n", formatStatus(stFrpc), emptyOr(cfg.Frpc.ServerAddr, "未配置"), cfg.Frpc.ServerPort)
	fmt.Printf("  Caddy (反向代理)  : %s  (规则 %d 条)\n", formatStatus(stCaddy), len(cfg.Caddy.Rules))

	host := cfg.PublicAddr()
	if host == "" {
		host = emptyOr(cfg.Network.LocalIPv4, "127.0.0.1")
	}
	port := "7500"
	if idx := strings.LastIndex(cfg.Web.ListenAddr, ":"); idx != -1 {
		port = cfg.Web.ListenAddr[idx+1:]
	}
	fmt.Printf("  WebUI 控制台      : %s  http://%s:%s  (账号 %s / 密码 %s)\n",
		formatStatus(stWeb), host, port, cfg.Web.Username, cfg.Web.Password)
	fmt.Printf("%s%s%s\n", cCyan, rule, cReset)
}

func formatStatus(st systemd.ServiceStatus) string {
	if !st.Installed {
		return cYellow + "○ 未安装" + cReset
	}
	if st.Running {
		return fmt.Sprintf("%s● 运行中%s", cGreen, cReset)
	}
	return cRed + "● 已停止" + cReset
}

func emptyOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func interactiveMain(store *config.Store) error {
	for {
		printDashboardHeader(store)
		fmt.Println()
		fmt.Printf("  %s1）%s 启动 / 开启 WebUI 网页控制台（推荐）\n", cCyan+cBold, cReset)
		fmt.Printf("  %s2）%s 远程 VPS 端：安装/启动 frps 与添加穿透服务\n", cCyan+cBold, cReset)
		fmt.Printf("  %s3）%s 本地服务器端：导入连接码并启动 frpc 客户端\n", cCyan+cBold, cReset)
		fmt.Printf("  %s4）%s Caddy 反代：快速添加域名/端口反向代理\n", cCyan+cBold, cReset)
		fmt.Printf("  %s5）%s 原服务探测与一键吞并 (NPM / Nginx / 原版 Caddy / frp)\n", cCyan+cBold, cReset)
		fmt.Printf("  %s6）%s 导出连接码 / 打印配置文件 (frps / frpc / Caddyfile)\n", cCyan+cBold, cReset)
		fmt.Printf("  %s7）%s 修改 WebUI 端口与密码 / 刷新网络探测\n", cCyan+cBold, cReset)
		fmt.Printf("  %s8）%s 完全卸载\n", cCyan+cBold, cReset)
		fmt.Printf("  %s0）%s 退出\n", cCyan+cBold, cReset)

		choice := ask("\n请选择", "0")
		fmt.Println()
		switch choice {
		case "1":
			if err := installWebService(store); err != nil {
				fmt.Printf("%s[x]%s %v\n", cRed, cReset, err)
			}
			pause()
		case "2":
			interactiveVPSMenu(store)
		case "3":
			interactiveLocalMenu(store)
		case "4":
			interactiveCaddyMenu(store)
		case "5":
			interactiveScannerMenu(store)
		case "6":
			_ = handleExport(store)
			pause()
		case "7":
			interactiveSettingsMenu(store)
		case "8":
			if err := uninstallAll(store); err == nil {
				return nil
			}
		case "0", "":
			return nil
		default:
			fmt.Printf("%s[!]%s 无效选项\n", cYellow, cReset)
		}
	}
}

func interactiveVPSMenu(store *config.Store) {
	cfg := store.Get()
	fmt.Printf("%s▌ 远程 VPS 端配置 (frps)%s\n", cCyan+cBold, cReset)
	portStr := ask("控制端口 (bindPort)", strconv.Itoa(cfg.Frps.ControlPort))
	if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
		_ = store.Update(func(c *config.Config) error {
			c.Frps.ControlPort = p
			return nil
		})
	}
	if confirm("是否现在添加一条穿透服务？") {
		name := ask("服务名称", "mc-java")
		typ := ask("协议类型 (tcp/udp)", "tcp")
		lp, _ := strconv.Atoi(ask("内网端口 (如 MC 25565 / Web 8080)", "25565"))
		rp, _ := strconv.Atoi(ask("对外端口", strconv.Itoa(lp)))
		domain := ask("联动绑定 Caddy 域名（如 app.example.com，不绑定直接回车）", "")
		if lp > 0 && rp > 0 {
			_ = store.Update(func(c *config.Config) error {
				c.Frps.Services = append(c.Frps.Services, config.TunnelRule{
					ID:           config.GenID("tun"),
					Name:         name,
					Type:         typ,
					LocalIP:      "127.0.0.1",
					LocalPort:    lp,
					RemotePort:   rp,
					CaddyDomain:  domain,
					CaddyTLSMode: "auto",
					Enabled:      true,
				})
				return nil
			})
		}
	}
	if confirm("是否安装并启动 frps 服务端？") {
		if err := frp.EnsureFrpsInstalledAndStarted(store); err != nil {
			fmt.Printf("%s[x]%s %v\n", cRed, cReset, err)
		} else {
			fmt.Printf("%s[+]%s frps 已启动！\n", cGreen, cReset)
		}
	}
	pause()
}

func interactiveLocalMenu(store *config.Store) {
	fmt.Printf("%s▌ 本地服务器端配置 (frpc)%s\n", cCyan+cBold, cReset)
	code := ask("请粘贴 VPS 导出的 autof:// 连接码（留空手动输入）", "")
	if code != "" {
		if err := handleImport(store, code); err != nil {
			fmt.Printf("%s[x]%s 导入失败: %v\n", cRed, cReset, err)
			pause()
			return
		}
	} else {
		cfg := store.Get()
		addr := ask("远程 VPS 地址", cfg.Frpc.ServerAddr)
		port, _ := strconv.Atoi(ask("远程控制端口", strconv.Itoa(cfg.Frpc.ServerPort)))
		token := ask("远程 Token", cfg.Frpc.Token)
		_ = store.Update(func(c *config.Config) error {
			c.Frpc.ServerAddr = addr
			if port > 0 {
				c.Frpc.ServerPort = port
			}
			c.Frpc.Token = token
			return nil
		})
	}
	if confirm("是否立即安装并启动本地 frpc 客户端？") {
		if err := frp.EnsureFrpcInstalledAndStarted(store); err != nil {
			fmt.Printf("%s[x]%s %v\n", cRed, cReset, err)
		} else {
			fmt.Printf("%s[+]%s frpc 已启动！\n", cGreen, cReset)
		}
	}
	pause()
}

func interactiveCaddyMenu(store *config.Store) {
	fmt.Printf("%s▌ Caddy 快速反代配置%s\n", cCyan+cBold, cReset)
	listen := ask("前端入口（如 app.example.com、app.example.com:8443 或 :8080）", "")
	if listen != "" {
		upstream := ask("后端目标地址（如 127.0.0.1:8080 或 192.168.1.50:5000）", "127.0.0.1:8080")
		tlsMode := ask("证书模式 (auto=自动公网证书 / dns=DNS证书 / internal=局域网自签 / http=纯HTTP)", "auto")
		_ = store.Update(func(c *config.Config) error {
			c.Caddy.Rules = append(c.Caddy.Rules, config.CaddyRule{
				ID:           config.GenID("caddy"),
				Name:         listen,
				Listen:       listen,
				Upstream:     upstream,
				TLSMode:      tlsMode,
				EnableEncode: true,
				Enabled:      true,
			})
			return nil
		})
	}
	if confirm("是否安装并启动/热重载 Caddy 服务？") {
		if err := caddy.EnsureInstalledAndStarted(store); err != nil {
			fmt.Printf("%s[x]%s %v\n", cRed, cReset, err)
		} else {
			fmt.Printf("%s[+]%s Caddy 已启动并加载最新反代配置！\n", cGreen, cReset)
		}
	}
	pause()
}

func interactiveSettingsMenu(store *config.Store) {
	cfg := store.Get()
	netCfg := cfg.Network
	netenv.DetectAndApply(&netCfg)
	listen := ask("WebUI 监听地址", cfg.Web.ListenAddr)
	pwd := ask("WebUI 登录密码", cfg.Web.Password)
	_ = store.Update(func(c *config.Config) error {
		c.Network = netCfg
		c.Web.ListenAddr = listen
		c.Web.Password = pwd
		return nil
	})
	fmt.Printf("%s[+]%s 设置已保存\n", cGreen, cReset)
	pause()
}

func printAllRules(store *config.Store) {
	cfg := store.Get()
	fmt.Printf("%s=== 远程 VPS 穿透服务 (frps: %d 条) ===%s\n", cBold, len(cfg.Frps.Services), cReset)
	for i, s := range cfg.Frps.Services {
		fmt.Printf("  %d. %-14s %-4s %s:%d -> :%d  %s\n", i+1, s.Name, s.Type, s.LocalIP, s.LocalPort, s.RemotePort, s.CaddyDomain)
	}
	fmt.Printf("\n%s=== 本地服务器穿透隧道 (frpc: %d 条) ===%s\n", cBold, len(cfg.Frpc.Tunnels), cReset)
	for i, s := range cfg.Frpc.Tunnels {
		fmt.Printf("  %d. %-14s %-4s %s:%d -> %s:%d\n", i+1, s.Name, s.Type, s.LocalIP, s.LocalPort, cfg.Frpc.ServerAddr, s.RemotePort)
	}
	fmt.Printf("\n%s=== Caddy 反代规则 (%d 条) ===%s\n", cBold, len(cfg.Caddy.Rules), cReset)
	for i, r := range cfg.Caddy.Rules {
		fmt.Printf("  %d. %-22s -> %-22s [%s]\n", i+1, r.Listen, r.Upstream, r.TLSMode)
	}
}

func handleGen(store *config.Store, target string) error {
	cfg := store.Get()
	switch target {
	case "frps":
		fmt.Print(frp.GenerateFrpsToml(cfg))
	case "frpc":
		fmt.Print(frp.GenerateFrpcTomlForExport(cfg, -1))
	case "frpc-client":
		fmt.Print(frp.GenerateFrpcTomlForClient(cfg))
	case "caddy", "caddyfile":
		fmt.Print(caddy.GenerateCaddyfile(cfg))
	case "code":
		code, err := cfg.ExportConnectionCode()
		if err != nil {
			return err
		}
		fmt.Println(code)
	default:
		return fmt.Errorf("未知生成类型: %s (可选: frps | frpc | frpc-client | caddy | code)", target)
	}
	return nil
}

func handleExport(store *config.Store) error {
	cfg := store.Get()
	code, err := cfg.ExportConnectionCode()
	if err != nil {
		return err
	}
	fmt.Printf("%s▌ 一键连接码（在本地服务器运行 `sudo autof import \"%s\"` 或在 WebUI 粘贴）%s\n", cCyan+cBold, code, cReset)
	fmt.Printf("%s\n\n", code)
	fmt.Printf("%s▌ 标准 frpc.toml 配置文件%s\n", cCyan+cBold, cReset)
	fmt.Println(frp.GenerateFrpcTomlForExport(cfg, -1))
	return nil
}

func handleImport(store *config.Store, arg string) error {
	raw := arg
	if !strings.HasPrefix(arg, config.ConnCodePrefix) {
		if data, err := os.ReadFile(arg); err == nil {
			raw = string(data)
		}
	}
	bundle, err := config.ParseImportInput(raw)
	if err != nil {
		return err
	}
	_ = store.Update(func(c *config.Config) error {
		if bundle.ServerAddr != "" {
			c.Frpc.ServerAddr = bundle.ServerAddr
		}
		if bundle.ServerPort > 0 {
			c.Frpc.ServerPort = bundle.ServerPort
		}
		if bundle.Token != "" {
			c.Frpc.Token = bundle.Token
		}
		c.Frpc.TLSEnable = bundle.TLSEnable
		if len(bundle.Tunnels) > 0 {
			c.Frpc.Tunnels = bundle.Tunnels
		}
		return nil
	})
	_ = frp.ApplyFrpc(store, true)
	fmt.Printf("%s[+]%s 已成功导入客户端配置（服务器 %s:%d，共 %d 条隧道）\n",
		cGreen, cReset, bundle.ServerAddr, bundle.ServerPort, len(bundle.Tunnels))
	return nil
}

func handleInstall(store *config.Store, target string) error {
	switch target {
	case "frps":
		return frp.EnsureFrpsInstalledAndStarted(store)
	case "frpc":
		return frp.EnsureFrpcInstalledAndStarted(store)
	case "caddy":
		return caddy.EnsureInstalledAndStarted(store)
	case "web":
		return installWebService(store)
	case "all":
		if _, err := frp.InstallBinaries(store); err != nil {
			return err
		}
		if _, err := caddy.InstallCaddy(store.Get().Caddy.DNSProvider); err != nil {
			return err
		}
		return installWebService(store)
	default:
		return fmt.Errorf("未知安装目标: %s (可选: frps | frpc | caddy | web | all)", target)
	}
}

func handlePwd(store *config.Store, newPwd string) error {
	if newPwd != "" {
		_ = store.Update(func(c *config.Config) error {
			c.Web.Password = newPwd
			return nil
		})
		fmt.Printf("%s[+]%s WebUI 密码已更新为: %s\n", cGreen, cReset, newPwd)
		return nil
	}
	cfg := store.Get()
	fmt.Printf("WebUI 监听地址: %s\n用户名: %s\n密  码: %s\n", cfg.Web.ListenAddr, cfg.Web.Username, cfg.Web.Password)
	return nil
}

func handleServiceCtl(store *config.Store, action, target string) error {
	if action == "logs" {
		fmt.Println(systemd.ReadLogs(target, store.StateDir(), 100))
		return nil
	}
	switch target {
	case "frps":
		if action == "start" {
			return frp.EnsureFrpsInstalledAndStarted(store)
		}
		return systemd.Control("frps", action, systemd.FindBinary("frps"), []string{"-c", store.FrpsConfPath()}, store.StateDir(), nil)
	case "frpc":
		if action == "start" {
			return frp.EnsureFrpcInstalledAndStarted(store)
		}
		if action == "reload" {
			return frp.ApplyFrpc(store, true)
		}
		return systemd.Control("frpc", action, systemd.FindBinary("frpc"), []string{"-c", store.FrpcConfPath()}, store.StateDir(), nil)
	case "caddy":
		if action == "start" {
			return caddy.EnsureInstalledAndStarted(store)
		}
		if action == "reload" {
			return caddy.WriteAndApply(store, true)
		}
		return systemd.Control("caddy", action, systemd.FindBinary("caddy"), []string{"run", "--config", store.CaddyfilePath(), "--adapter", "caddyfile"}, store.StateDir(), nil)
	case "autof", "web":
		if action == "start" {
			return installWebService(store)
		}
		return systemd.Control("autof", action, systemd.FindBinary("autof"), []string{"web"}, store.StateDir(), nil)
	default:
		return fmt.Errorf("未知服务: %s (可选: frps | frpc | caddy | autof)", target)
	}
}

func uninstallAll(store *config.Store) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("卸载需要 root 权限，请使用 sudo 运行")
	}
	fmt.Printf("%s▌ 卸载 Autofrp%s\n", cCyan+cBold, cReset)
	fmt.Printf("  将停止并移除 frps、frpc、autof 服务，删除配置目录 %s 及 /usr/local/bin/autof\n", store.StateDir())
	if !confirm("确认完全卸载？") {
		fmt.Println("已取消")
		return fmt.Errorf("已取消")
	}
	systemd.RemoveService("frps")
	systemd.RemoveService("frpc")
	systemd.RemoveService("caddy")
	systemd.RemoveService("autof")
	_ = os.Remove("/usr/local/bin/frps")
	_ = os.Remove("/usr/local/bin/frpc")
	if store.StateDir() != "" && store.StateDir() != "/" {
		_ = os.RemoveAll(store.StateDir())
	}
	_ = os.Remove(filepath.Join("/usr/local/bin", config.AppName))
	fmt.Printf("%s[+]%s 已完全卸载 Autofrp\n", cGreen, cReset)
	return nil
}

func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("%s%s%s [%s%s%s]: ", cCyan, prompt, cReset, cDim, def, cReset)
	} else {
		fmt.Printf("%s%s%s: ", cCyan, prompt, cReset)
	}
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func confirm(prompt string) bool {
	ans := ask(prompt+" (y/N)", "n")
	switch strings.ToLower(ans) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func pause() {
	fmt.Printf("%s按回车继续...%s", cDim, cReset)
	_, _ = stdinReader.ReadString('\n')
}

func printUsage(stateDir string) {
	fmt.Printf(`Autofrp v%s - 远程 VPS 与本地服务器内网穿透 (frps/frpc) & Caddy 快速反代工具

用法:
  autof                          进入终端交互主菜单
  autof web [--listen :7500]     前台运行 WebUI 控制台
  autof web-install              安装并启动 WebUI 后台服务 (systemd)
  autof status                   查看网络、组件状态及 WebUI 登录信息
  autof detect                   扫描服务器原有进程与配置 (NPM / Nginx / 原版 Caddy / frp)
  autof takeover <服务ID>        识别并迁移原服务配置与证书到 /etc/autof/，清理老服务
  autof list                     查看穿透规则与 Caddy 反代列表
  autof export                   导出客户端 autof:// 连接码与 frpc.toml
  autof import <连接码或文件>    在本地服务器一键导入连接码或 frpc.toml
  autof gen frps|frpc|caddy|code 打印生成的配置文件或连接码
  autof install frps|frpc|caddy|web|all
  autof start|stop|restart|reload|logs [frps|frpc|caddy|autof]
  autof net 4|6|both             设置双栈网络偏好
  autof pwd [新密码]             查看或重置 WebUI 登录密码
  autof uninstall                完全卸载

配置目录: %s
`, config.AppVersion, stateDir)
}

func printDetectedServices(store *config.Store) []scanner.DetectedApp {
	apps := scanner.ScanServer(store.StateDir())
	fmt.Printf("%s▌ 原服务器反代与穿透进程扫描结果 (%d 项)%s\n", cCyan+cBold, len(apps), cReset)
	if len(apps) == 0 {
		fmt.Println("  未检测到外部 NPM、Nginx、原版 Caddy 或 frp 实例。")
		return nil
	}
	for i, a := range apps {
		runLabel := cYellow + "○ 未运行" + cReset
		if a.Running {
			runLabel = cGreen + "● 运行中" + cReset
		}
		fmt.Printf("  [%d] ID: %s%-16s%s %s  %s\n", i+1, cBold, a.ID, cReset, a.Name, runLabel)
		if a.Container != "" {
			fmt.Printf("      Docker 容器 : %s (%s)\n", a.Container, a.BinaryOrImage)
		} else if a.PID > 0 {
			fmt.Printf("      进程信息    : PID %d (%s)\n", a.PID, a.Cmdline)
		}
		if len(a.ListenPorts) > 0 {
			fmt.Printf("      监听端口    : %s\n", strings.Join(a.ListenPorts, ", "))
		}
		if len(a.ConfigFiles) > 0 {
			fmt.Printf("      配置文件    : %s\n", strings.Join(a.ConfigFiles, ", "))
		}
	}
	return apps
}

func interactiveScannerMenu(store *config.Store) {
	apps := printDetectedServices(store)
	if len(apps) == 0 {
		pause()
		return
	}
	appID := ask("\n输入要吞并迁移的服务 ID（如 docker-npm / host-nginx / host-caddy，留空返回）", "")
	if appID == "" {
		return
	}
	if err := handleCLITakeover(store, appID); err != nil {
		fmt.Printf("%s[x]%s %v\n", cRed, cReset, err)
	}
	pause()
}

func handleCLITakeover(store *config.Store, appID string) error {
	plan, err := scanner.PreviewTakeover(store, appID)
	if err != nil {
		return err
	}
	fmt.Printf("\n%s▌ 吞并迁移预览确认 · %s (%s)%s\n", cCyan+cBold, plan.AppName, plan.AppID, cReset)
	fmt.Printf("  识别到反代规则 : %d 条\n", len(plan.DiscoveredRules))
	for _, r := range plan.DiscoveredRules {
		fmt.Printf("    - %s -> %s (SSL: %v)\n", r.Domain, r.Upstream, r.HasSSL)
	}
	fmt.Printf("  识别到 SSL 证书: %d 套 (将迁移至 %s)\n", len(plan.DiscoveredCerts), plan.CertsTargetDir)
	for _, c := range plan.DiscoveredCerts {
		fmt.Printf("    - %s: %s -> %s\n", c.Domain, c.SrcCertPath, c.DestCertPath)
	}
	fmt.Println("  将执行的步骤:")
	for _, s := range plan.CleanupSteps {
		fmt.Printf("    %s\n", s)
	}
	if !confirm("\n是否确认执行吞并迁移并停止原服务？") {
		fmt.Println("已取消")
		return nil
	}
	removeOld := confirm("是否同时彻底删除原 Docker 容器 / 归档原配置文件？")
	res, err := scanner.ExecuteTakeover(store, scanner.TakeoverRequest{
		AppID:             appID,
		UseImportedCerts:  true,
		StopOldService:    true,
		RemoveOldService:  removeOld,
		StartAutofService: true,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s[+]%s 吞并迁移完成！导入反代 %d 条，迁移证书 %d 套，备份目录: %s\n",
		cGreen, cReset, res.ImportedRules, res.CopiedCerts, res.BackupDir)
	for _, l := range res.Logs {
		fmt.Printf("    - %s\n", l)
	}
	return nil
}
