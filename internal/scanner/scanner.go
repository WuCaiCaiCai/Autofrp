package scanner

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

// DetectedApp 描述在服务器上探测到的原有进程/服务（原版 Caddy、原版 frps/frpc、NPM、Nginx 等）
type DetectedApp struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"` // "caddy", "frp", "npm", "nginx", "other"
	Running        bool     `json:"running"`
	PID            int      `json:"pid"`
	Container      string   `json:"container"`      // 若为 Docker 容器则记录容器名
	BinaryOrImage  string   `json:"binaryOrImage"`  // 二进制路径或镜像名
	Cmdline        string   `json:"cmdline"`        // 启动命令行
	SystemdUnit    string   `json:"systemdUnit"`    // systemd unit 名称
	ListenPorts    []string `json:"listenPorts"`    // 监听端口列表
	ConfigFiles    []string `json:"configFiles"`    // 可编辑的配置文件路径列表
	PrimaryConfig  string   `json:"primaryConfig"`  // 当前生效的主配置文件路径
	ManagedByAutof bool     `json:"managedByAutof"` // 是否已由 Autofrp 托管
	CanImport      bool     `json:"canImport"`      // 是否支持一键导入到 Autofrp 规则表
	Note           string   `json:"note"`           // 附加说明（如端口冲突提示）
}

type procInfo struct {
	pid     int
	comm    string
	exe     string
	args    []string
	cmdline string
	ports   []string
}

// ScanServer 扫描当前服务器上的原有反代与穿透进程及配置文件
func ScanServer(autofStateDir string) []DetectedApp {
	procs := scanProcFS()
	var results []DetectedApp

	// 1. 探测 Caddy (原版或已运行实例)
	if app := detectCaddy(procs, autofStateDir); app != nil {
		results = append(results, *app)
	}

	// 2. 探测 frps (原版或已运行实例)
	if app := detectFrp(procs, "frps", "原版 / 系统 frps 服务端", autofStateDir); app != nil {
		results = append(results, *app)
	}

	// 3. 探测 frpc (原版或已运行实例)
	if app := detectFrp(procs, "frpc", "原版 / 系统 frpc 客户端", autofStateDir); app != nil {
		results = append(results, *app)
	}

	// 4. 探测 Nginx Proxy Manager (NPM) 与原版 Nginx / OpenResty
	results = append(results, detectNPMAndNginx(procs)...)

	// 5. 探测 Docker 容器中的 NPM / Caddy / frp / Lucky / Traefik
	results = append(results, detectDockerContainers(results)...)

	// 6. 探测其他常见反代/穿透进程 (Lucky / Traefik / Cloudflared)
	results = append(results, detectOtherProxies(procs, results)...)

	return results
}

func detectCaddy(procs []procInfo, autofStateDir string) *DetectedApp {
	candidateConfigs := []string{
		"/etc/caddy/Caddyfile",
		"/usr/local/etc/caddy/Caddyfile",
		"/etc/caddy/caddy.json",
	}
	if autofStateDir != "" {
		candidateConfigs = append(candidateConfigs, filepath.Join(autofStateDir, "Caddyfile"))
	}

	var matchedProc *procInfo
	for i := range procs {
		base := filepath.Base(procs[i].exe)
		if base == "caddy" || procs[i].comm == "caddy" {
			matchedProc = &procs[i]
			break
		}
	}

	var existingFiles []string
	primaryCfg := ""
	if matchedProc != nil {
		if cfgFromCmd := extractFlagValue(matchedProc.args, "--config", "-c"); cfgFromCmd != "" {
			primaryCfg = cfgFromCmd
			existingFiles = appendUniqueFile(existingFiles, cfgFromCmd)
		}
	}
	for _, p := range candidateConfigs {
		existingFiles = appendUniqueFile(existingFiles, p)
	}
	if primaryCfg == "" && len(existingFiles) > 0 {
		primaryCfg = existingFiles[0]
	}

	binPath := findExecutable("caddy", "/usr/bin/caddy", "/usr/local/bin/caddy")
	unitName := findSystemdUnit("caddy")

	if matchedProc == nil && binPath == "" && len(existingFiles) == 0 {
		return nil
	}

	app := &DetectedApp{
		ID:            "host-caddy",
		Name:          "Caddy Web / 反代服务",
		Category:      "caddy",
		BinaryOrImage: binPath,
		SystemdUnit:   unitName,
		ConfigFiles:   existingFiles,
		PrimaryConfig: primaryCfg,
		CanImport:     primaryCfg != "" && strings.HasSuffix(strings.ToLower(primaryCfg), "caddyfile"),
	}
	if matchedProc != nil {
		app.Running = true
		app.PID = matchedProc.pid
		if matchedProc.exe != "" {
			app.BinaryOrImage = matchedProc.exe
		}
		app.Cmdline = matchedProc.cmdline
		app.ListenPorts = matchedProc.ports
	}
	if autofStateDir != "" && strings.HasPrefix(primaryCfg, autofStateDir) {
		app.ManagedByAutof = true
		app.Note = "当前由 Autofrp 托管配置"
	} else if primaryCfg != "" {
		app.Note = "检测到服务器原有 Caddy 配置，可直接在线编辑或导入到 Autofrp"
	}
	return app
}

func detectFrp(procs []procInfo, binName, displayTitle, autofStateDir string) *DetectedApp {
	candidateConfigs := []string{
		fmt.Sprintf("/etc/frp/%s.toml", binName),
		fmt.Sprintf("/etc/frp/%s.ini", binName),
		fmt.Sprintf("/usr/local/etc/frp/%s.toml", binName),
		fmt.Sprintf("/usr/local/etc/frp/%s.ini", binName),
		fmt.Sprintf("/root/%s.toml", binName),
		fmt.Sprintf("/root/%s.ini", binName),
	}
	if autofStateDir != "" {
		candidateConfigs = append(candidateConfigs, filepath.Join(autofStateDir, binName+".toml"))
	}

	var matchedProc *procInfo
	for i := range procs {
		base := filepath.Base(procs[i].exe)
		if base == binName || procs[i].comm == binName {
			matchedProc = &procs[i]
			break
		}
	}

	var existingFiles []string
	primaryCfg := ""
	if matchedProc != nil {
		if cfgFromCmd := extractFlagValue(matchedProc.args, "-c", "--config"); cfgFromCmd != "" {
			primaryCfg = cfgFromCmd
			existingFiles = appendUniqueFile(existingFiles, cfgFromCmd)
		}
	}
	for _, p := range candidateConfigs {
		existingFiles = appendUniqueFile(existingFiles, p)
	}
	if primaryCfg == "" && len(existingFiles) > 0 {
		primaryCfg = existingFiles[0]
	}

	binPath := findExecutable(binName, "/usr/local/bin/"+binName, "/usr/bin/"+binName)
	unitName := findSystemdUnit(binName)

	if matchedProc == nil && binPath == "" && len(existingFiles) == 0 {
		return nil
	}

	app := &DetectedApp{
		ID:            "host-" + binName,
		Name:          displayTitle,
		Category:      "frp",
		BinaryOrImage: binPath,
		SystemdUnit:   unitName,
		ConfigFiles:   existingFiles,
		PrimaryConfig: primaryCfg,
		CanImport:     primaryCfg != "",
	}
	if matchedProc != nil {
		app.Running = true
		app.PID = matchedProc.pid
		if matchedProc.exe != "" {
			app.BinaryOrImage = matchedProc.exe
		}
		app.Cmdline = matchedProc.cmdline
		app.ListenPorts = matchedProc.ports
	}
	if autofStateDir != "" && strings.HasPrefix(primaryCfg, autofStateDir) {
		app.ManagedByAutof = true
		app.Note = "当前由 Autofrp 托管配置"
	} else if primaryCfg != "" {
		app.Note = "检测到原版 " + binName + " 配置文件，支持直接编辑或一键导入规则"
	}
	return app
}

func detectNPMAndNginx(procs []procInfo) []DetectedApp {
	var out []DetectedApp

	// 检查是否有宿主机运行的 Nginx Proxy Manager (node index.js + nginx) 或普通 Nginx
	isNPMHost := false
	for _, p := range procs {
		if strings.Contains(p.cmdline, "nginx-proxy-manager") {
			isNPMHost = true
			break
		}
	}

	var nginxProc *procInfo
	for i := range procs {
		base := filepath.Base(procs[i].exe)
		if base == "nginx" || base == "openresty" || procs[i].comm == "nginx" {
			nginxProc = &procs[i]
			break
		}
	}

	var nginxConfigs []string
	for _, p := range []string{
		"/etc/nginx/nginx.conf",
		"/usr/local/nginx/conf/nginx.conf",
		"/usr/local/openresty/nginx/conf/nginx.conf",
		"/data/nginx/proxy_host",
	} {
		nginxConfigs = appendUniqueFile(nginxConfigs, p)
	}
	if matches, err := filepath.Glob("/etc/nginx/conf.d/*.conf"); err == nil {
		for _, m := range matches {
			nginxConfigs = appendUniqueFile(nginxConfigs, m)
		}
	}
	if matches, err := filepath.Glob("/etc/nginx/sites-enabled/*"); err == nil {
		for _, m := range matches {
			nginxConfigs = appendUniqueFile(nginxConfigs, m)
		}
	}

	binPath := findExecutable("nginx", "/usr/sbin/nginx", "/usr/bin/nginx")
	if nginxProc == nil && binPath == "" && len(nginxConfigs) == 0 {
		return nil
	}

	name := "Nginx / OpenResty 反代服务"
	cat := "nginx"
	note := "系统已存在 Nginx 服务；若占用 80/443 端口，请注意与 Caddy 端口协调"
	if isNPMHost {
		name = "Nginx Proxy Manager (NPM)"
		cat = "npm"
		note = "宿主机正在运行 NPM (默认管理端口 81，反代端口 80/443)"
	}

	primary := ""
	if len(nginxConfigs) > 0 {
		primary = nginxConfigs[0]
	}

	app := DetectedApp{
		ID:            "host-nginx",
		Name:          name,
		Category:      cat,
		BinaryOrImage: binPath,
		SystemdUnit:   findSystemdUnit("nginx"),
		ConfigFiles:   nginxConfigs,
		PrimaryConfig: primary,
		CanImport:     len(nginxConfigs) > 0,
		Note:          note,
	}
	if nginxProc != nil {
		app.Running = true
		app.PID = nginxProc.pid
		if nginxProc.exe != "" {
			app.BinaryOrImage = nginxProc.exe
		}
		app.Cmdline = nginxProc.cmdline
		app.ListenPorts = nginxProc.ports
	}
	out = append(out, app)
	return out
}

func detectDockerContainers(existing []DetectedApp) []DetectedApp {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	// docker ps --format '{{.ID}}|{{.Names}}|{{.Image}}|{{.Ports}}|{{.Status}}'
	outBytes, err := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Names}}|{{.Image}}|{{.Ports}}|{{.Status}}").Output()
	if err != nil || len(outBytes) == 0 {
		return nil
	}

	var results []DetectedApp
	scanner := bufio.NewScanner(strings.NewReader(string(outBytes)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		cid, cname, image, portsStr := parts[0], parts[1], parts[2], parts[3]
		lower := strings.ToLower(cname + " " + image)

		var appName, category, note string
		switch {
		case strings.Contains(lower, "nginx-proxy-manager") || strings.Contains(lower, "npm"):
			appName = "Nginx Proxy Manager (Docker 容器)"
			category = "npm"
			note = "Docker 方式运行的 NPM 反代面板"
		case strings.Contains(lower, "caddy"):
			appName = "Caddy (Docker 容器)"
			category = "caddy"
			note = "Docker 方式运行的原版 Caddy"
		case strings.Contains(lower, "frps"):
			appName = "frps (Docker 容器)"
			category = "frp"
			note = "Docker 方式运行的 frps 服务端"
		case strings.Contains(lower, "frpc"):
			appName = "frpc (Docker 容器)"
			category = "frp"
			note = "Docker 方式运行的 frpc 客户端"
		case strings.Contains(lower, "lucky"):
			appName = "Lucky 反代/穿透工具 (Docker)"
			category = "other"
			note = "Docker 方式运行的 Lucky 服务"
		case strings.Contains(lower, "traefik"):
			appName = "Traefik 反代 (Docker)"
			category = "other"
			note = "Docker 方式运行的 Traefik 反代"
		default:
			continue
		}

		// 查找容器挂载到宿主机的配置文件
		mountedConfigs := inspectDockerMountedConfigs(cid)
		primary := ""
		if len(mountedConfigs) > 0 {
			primary = mountedConfigs[0]
		}
		var ports []string
		if portsStr != "" {
			ports = []string{portsStr}
		}

		results = append(results, DetectedApp{
			ID:            "docker-" + cname,
			Name:          appName,
			Category:      category,
			Running:       true,
			Container:     cname,
			BinaryOrImage: image,
			Cmdline:       "docker container: " + cname + " (" + cid + ")",
			ListenPorts:   ports,
			ConfigFiles:   mountedConfigs,
			PrimaryConfig: primary,
			CanImport:     category == "caddy" || category == "frp" || category == "npm" || category == "nginx",
			Note:          note,
		})
	}
	return results
}

func inspectDockerMountedConfigs(containerID string) []string {
	out, err := exec.Command("docker", "inspect", "--format", "{{json .Mounts}}", containerID).Output()
	if err != nil {
		return nil
	}
	var mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}
	if err := json.Unmarshal(out, &mounts); err != nil {
		return nil
	}
	var files []string
	for _, m := range mounts {
		st, err := os.Stat(m.Source)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			files = appendUniqueFile(files, m.Source)
			continue
		}
		// 如果是挂载目录，扫描其中常见的配置文件
		for _, rel := range []string{
			"Caddyfile",
			"frps.toml", "frps.ini", "frpc.toml", "frpc.ini",
			"nginx.conf",
		} {
			files = appendUniqueFile(files, filepath.Join(m.Source, rel))
		}
		if matches, err := filepath.Glob(filepath.Join(m.Source, "nginx", "proxy_host", "*.conf")); err == nil {
			for _, f := range matches {
				files = appendUniqueFile(files, f)
			}
		}
	}
	return files
}

func detectOtherProxies(procs []procInfo, existing []DetectedApp) []DetectedApp {
	var out []DetectedApp
	targets := []struct {
		bin   string
		title string
	}{
		{"lucky", "Lucky 反代/端口转发服务"},
		{"traefik", "Traefik 反向代理"},
		{"cloudflared", "Cloudflare Tunnel (cloudflared)"},
	}
	for _, t := range targets {
		for _, p := range procs {
			if filepath.Base(p.exe) == t.bin || p.comm == t.bin {
				var cfgs []string
				if c := extractFlagValue(p.args, "-c", "--config"); c != "" {
					cfgs = appendUniqueFile(cfgs, c)
				}
				primary := ""
				if len(cfgs) > 0 {
					primary = cfgs[0]
				}
				out = append(out, DetectedApp{
					ID:            "host-" + t.bin,
					Name:          t.title,
					Category:      "other",
					Running:       true,
					PID:           p.pid,
					BinaryOrImage: p.exe,
					Cmdline:       p.cmdline,
					ListenPorts:   p.ports,
					ConfigFiles:   cfgs,
					PrimaryConfig: primary,
					Note:          "检测到正在运行的进程",
				})
				break
			}
		}
	}
	return out
}

// ReadExternalFile 读取服务器上的原配置文件内容（供 WebUI 和 CLI 查看/编辑）
func ReadExternalFile(path string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || !filepath.IsAbs(clean) {
		return "", errors.New("请提供合法的绝对文件路径")
	}
	st, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("无法读取文件 %s: %w", clean, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s 是一个目录，请选择具体配置文件", clean)
	}
	if st.Size() > 2*1024*1024 {
		return "", errors.New("文件超过 2MB 限制，不支持在线编辑")
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveExternalFile 保存修改后的原配置文件（自动生成 .bak 备份），并可选触发对应服务重载/重启
func SaveExternalFile(path, content string, reloadService bool, appID string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || !filepath.IsAbs(clean) {
		return errors.New("请提供合法的绝对文件路径")
	}
	// 先备份原文件
	if orig, err := os.ReadFile(clean); err == nil {
		_ = os.WriteFile(clean+".autof.bak", orig, 0600)
	}
	if err := os.WriteFile(clean, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	if reloadService {
		return reloadDetectedApp(appID, clean)
	}
	return nil
}

func reloadDetectedApp(appID, configPath string) error {
	if strings.HasPrefix(appID, "docker-") {
		cname := strings.TrimPrefix(appID, "docker-")
		out, err := exec.Command("docker", "restart", cname).CombinedOutput()
		if err != nil {
			return fmt.Errorf("重启容器 %s 失败: %s", cname, strings.TrimSpace(string(out)))
		}
		return nil
	}
	switch appID {
	case "host-caddy":
		if bin := findExecutable("caddy", "/usr/local/bin/caddy", "/usr/bin/caddy"); bin != "" && configPath != "" {
			if err := exec.Command(bin, "reload", "--config", configPath, "--adapter", "caddyfile").Run(); err == nil {
				return nil
			}
		}
		_ = exec.Command("systemctl", "reload-or-restart", "caddy").Run()
	case "host-frps":
		_ = exec.Command("systemctl", "restart", "frps").Run()
	case "host-frpc":
		if bin := findExecutable("frpc", "/usr/local/bin/frpc", "/usr/bin/frpc"); bin != "" && configPath != "" {
			if err := exec.Command(bin, "reload", "-c", configPath).Run(); err == nil {
				return nil
			}
		}
		_ = exec.Command("systemctl", "restart", "frpc").Run()
	case "host-nginx":
		if bin := findExecutable("nginx", "/usr/sbin/nginx", "/usr/bin/nginx"); bin != "" {
			if out, err := exec.Command(bin, "-s", "reload").CombinedOutput(); err != nil {
				return fmt.Errorf("nginx reload 失败: %s", strings.TrimSpace(string(out)))
			}
			return nil
		}
		_ = exec.Command("systemctl", "reload", "nginx").Run()
	}
	return nil
}

// ImportExternalConfig 将服务器上已有的原版 Caddyfile 或 frps/frpc 配置解析并导入到 Autofrp
func ImportExternalConfig(store *config.Store, category, configPath string) (int, error) {
	raw, err := ReadExternalFile(configPath)
	if err != nil {
		return 0, err
	}

	switch category {
	case "caddy":
		rules := parseExistingCaddyfile(raw)
		if len(rules) == 0 {
			return 0, errors.New("未能在该 Caddyfile 中识别出包含 reverse_proxy 的反代块")
		}
		err := store.Update(func(c *config.Config) error {
			for _, r := range rules {
				exists := false
				for _, old := range c.Caddy.Rules {
					if old.Listen == r.Listen && old.Upstream == r.Upstream {
						exists = true
						break
					}
				}
				if !exists {
					c.Caddy.Rules = append(c.Caddy.Rules, r)
				}
			}
			return nil
		})
		return len(rules), err

	case "frp":
		return importExistingFrpFile(store, configPath, raw)

	default:
		return 0, fmt.Errorf("暂不支持自动导入 %s 类型的配置，请使用在线编辑功能直接修改", category)
	}
}

func parseExistingCaddyfile(raw string) []config.CaddyRule {
	var rules []config.CaddyRule
	lines := strings.Split(raw, "\n")

	var curHeader string
	var curUpstream string
	var curTLSMode = "auto"
	var curSkipVerify bool
	var curEncode bool
	depth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		openCount := strings.Count(trimmed, "{")
		closeCount := strings.Count(trimmed, "}")

		if depth == 0 && strings.HasSuffix(trimmed, "{") {
			hdr := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			// 跳过全局块 `{` 或 snippet `(name) {`
			if hdr != "" && !strings.HasPrefix(hdr, "(") {
				curHeader = hdr
				curUpstream = ""
				curTLSMode = "auto"
				if strings.HasPrefix(curHeader, "http://") {
					curTLSMode = "http"
				}
				curSkipVerify = false
				curEncode = false
			}
		} else if depth >= 1 && curHeader != "" {
			if strings.HasPrefix(trimmed, "reverse_proxy ") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "reverse_proxy "))
				fields := strings.Fields(rest)
				if len(fields) > 0 && fields[0] != "{" {
					curUpstream = fields[0]
				}
			} else if strings.HasPrefix(trimmed, "tls internal") {
				curTLSMode = "internal"
			} else if strings.HasPrefix(trimmed, "dns ") {
				curTLSMode = "dns"
			} else if strings.HasPrefix(trimmed, "encode ") {
				curEncode = true
			} else if strings.Contains(trimmed, "tls_insecure_skip_verify") {
				curSkipVerify = true
			}
		}

		depth += openCount - closeCount
		if depth <= 0 {
			depth = 0
			if curHeader != "" && curUpstream != "" {
				rules = append(rules, config.CaddyRule{
					ID:            config.GenID("caddy"),
					Name:          curHeader,
					Listen:        curHeader,
					Upstream:      curUpstream,
					TLSMode:       curTLSMode,
					SkipTLSVerify: curSkipVerify,
					EnableEncode:  curEncode,
					Enabled:       true,
					Remark:        "从原版 Caddyfile 导入",
				})
			}
			curHeader = ""
		}
	}
	return rules
}

func importExistingFrpFile(store *config.Store, path, raw string) (int, error) {
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "frps") {
		port := 0
		token := ""
		scanner := bufio.NewScanner(strings.NewReader(raw))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			switch k {
			case "bindPort", "bind_port":
				port, _ = strconv.Atoi(v)
			case "auth.token", "token":
				token = v
			}
		}
		if port == 0 && token == "" {
			return 0, errors.New("未在文件中识别出 bindPort 或 token 参数")
		}
		err := store.Update(func(c *config.Config) error {
			if port > 0 {
				c.Frps.ControlPort = port
			}
			if token != "" {
				c.Frps.Token = token
			}
			return nil
		})
		return 1, err
	}

	// 解析 frpc.toml 或 frpc.ini
	bundle, err := config.ParseImportInput(raw)
	if err != nil {
		return 0, err
	}
	err = store.Update(func(c *config.Config) error {
		if bundle.ServerAddr != "" {
			c.Frpc.ServerAddr = bundle.ServerAddr
		}
		if bundle.ServerPort > 0 {
			c.Frpc.ServerPort = bundle.ServerPort
		}
		if bundle.Token != "" {
			c.Frpc.Token = bundle.Token
		}
		for _, t := range bundle.Tunnels {
			c.Frpc.Tunnels = append(c.Frpc.Tunnels, t)
		}
		return nil
	})
	return len(bundle.Tunnels) + 1, err
}

func scanProcFS() []procInfo {
	inodeToPort := scanListeningSockets()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	selfPID := os.Getpid()
	var list []procInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == selfPID {
			continue
		}
		procDir := filepath.Join("/proc", e.Name())
		cmdBytes, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
		if err != nil || len(cmdBytes) == 0 {
			continue
		}
		rawParts := strings.Split(strings.TrimRight(string(cmdBytes), "\x00"), "\x00")
		if len(rawParts) == 0 || rawParts[0] == "" {
			continue
		}
		commBytes, _ := os.ReadFile(filepath.Join(procDir, "comm"))
		comm := strings.TrimSpace(string(commBytes))
		exe, _ := os.Readlink(filepath.Join(procDir, "exe"))
		if exe == "" {
			exe = rawParts[0]
		}

		// 关联该进程持有的监听端口
		var ports []string
		if fds, err := os.ReadDir(filepath.Join(procDir, "fd")); err == nil {
			portSet := make(map[string]bool)
			for _, fd := range fds {
				link, err := os.Readlink(filepath.Join(procDir, "fd", fd.Name()))
				if err != nil {
					continue
				}
				if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
					inode := link[8 : len(link)-1]
					if p, ok := inodeToPort[inode]; ok {
						portSet[p] = true
					}
				}
			}
			for p := range portSet {
				ports = append(ports, p)
			}
			sort.Strings(ports)
		}

		list = append(list, procInfo{
			pid:     pid,
			comm:    comm,
			exe:     exe,
			args:    rawParts,
			cmdline: strings.Join(rawParts, " "),
			ports:   ports,
		})
	}
	return list
}

func scanListeningSockets() map[string]string {
	inodeMap := make(map[string]string)
	for _, netFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(netFile)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		first := true
		for sc.Scan() {
			if first {
				first = false
				continue
			}
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 {
				continue
			}
			// state 0A 表示 TCP_LISTEN
			if fields[3] != "0A" {
				continue
			}
			local := fields[1]
			inode := fields[9]
			colonIdx := strings.LastIndex(local, ":")
			if colonIdx == -1 {
				continue
			}
			portHex := local[colonIdx+1:]
			portBytes, err := hex.DecodeString(portHex)
			if err != nil || len(portBytes) != 2 {
				continue
			}
			port := int(portBytes[0])<<8 | int(portBytes[1])
			if port > 0 {
				inodeMap[inode] = ":" + strconv.Itoa(port)
			}
		}
		f.Close()
	}
	return inodeMap
}

func extractFlagValue(args []string, flags ...string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		for _, fl := range flags {
			if arg == fl && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(arg, fl+"=") {
				return strings.TrimPrefix(arg, fl+"=")
			}
		}
	}
	return ""
}

func appendUniqueFile(list []string, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return list
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return list
	}
	for _, item := range list {
		if item == path {
			return list
		}
	}
	return append(list, path)
}

func findExecutable(name string, fallbacks ...string) string {
	for _, p := range fallbacks {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func findSystemdUnit(name string) string {
	for _, dir := range []string{"/etc/systemd/system", "/lib/systemd/system", "/usr/lib/systemd/system"} {
		p := filepath.Join(dir, name+".service")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
