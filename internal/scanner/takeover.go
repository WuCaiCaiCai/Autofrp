package scanner

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/caddy"
	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/frp"
)

// MigratedRulePreview 表示从 NPM / Nginx / 原版 Caddy 识别出的待迁移反代规则预览
type MigratedRulePreview struct {
	Domain     string `json:"domain"`
	Upstream   string `json:"upstream"`
	HasSSL     bool   `json:"hasSsl"`
	CertPath   string `json:"certPath,omitempty"`
	KeyPath    string `json:"keyPath,omitempty"`
	SourceFile string `json:"sourceFile"`
}

// CertPair 表示从原服务识别出的一对 SSL 证书（含宿主机源路径与迁移目标路径）
type CertPair struct {
	Domain       string `json:"domain"`
	SrcCertPath  string `json:"srcCertPath"`
	SrcKeyPath   string `json:"srcKeyPath"`
	DestCertPath string `json:"destCertPath"`
	DestKeyPath  string `json:"destKeyPath"`
}

// TakeoverPlan 是「一键吞并迁移老服务」确认窗口展示的完整预览计划
type TakeoverPlan struct {
	AppID              string                `json:"appId"`
	AppName            string                `json:"appName"`
	Category           string                `json:"category"`
	Container          string                `json:"container,omitempty"`
	ComposeProject     string                `json:"composeProject,omitempty"`
	ComposeService     string                `json:"composeService,omitempty"`
	ComposeFile        string                `json:"composeFile,omitempty"`
	SystemdUnit        string                `json:"systemdUnit,omitempty"`
	PID                int                   `json:"pid,omitempty"`
	TargetStateDir     string                `json:"targetStateDir"`
	CertsTargetDir     string                `json:"certsTargetDir"`
	BackupTargetDir    string                `json:"backupTargetDir"`
	DiscoveredRules    []MigratedRulePreview `json:"discoveredRules"`
	DiscoveredTunnels  []config.TunnelRule   `json:"discoveredTunnels"`
	DiscoveredCerts    []CertPair            `json:"discoveredCerts"`
	DiscoveredFrpsPort int                   `json:"discoveredFrpsPort,omitempty"`
	CleanupSteps       []string              `json:"cleanupSteps"`
}

// TakeoverRequest 是用户在确认窗口点击「确认吞并迁移」后提交的参数
type TakeoverRequest struct {
	AppID             string `json:"appId"`
	UseImportedCerts  bool   `json:"useImportedCerts"`  // 是否直接使用迁移过来的原 SSL 证书文件（否则交由 Caddy 自动申请）
	StopOldService    bool   `json:"stopOldService"`    // 停止并禁用老服务/容器（释放 80/443/7000 端口）
	RemoveOldService  bool   `json:"removeOldService"`  // 彻底移除老 Docker 容器或原 systemd 单元
	StartAutofService bool   `json:"startAutofService"` // 迁移完成后立即启动 Autofrp 的 Caddy / frp 接管
}

// TakeoverResult 返回吞并迁移的执行结果摘要
type TakeoverResult struct {
	ImportedRules   int      `json:"importedRules"`
	ImportedTunnels int      `json:"importedTunnels"`
	CopiedCerts     int      `json:"copiedCerts"`
	BackupDir       string   `json:"backupDir"`
	Logs            []string `json:"logs"`
}

// PreviewTakeover 扫描目标老服务（NPM / Nginx / 原版 Caddy / 原版 frp），生成迁移与清理预览计划供确认弹窗展示
func PreviewTakeover(store *config.Store, appID string) (*TakeoverPlan, error) {
	apps := ScanServer(store.StateDir())
	var target *DetectedApp
	for i := range apps {
		if apps[i].ID == appID {
			target = &apps[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("未找到目标服务: %s", appID)
	}

	ts := time.Now().Format("20060102_150405")
	plan := &TakeoverPlan{
		AppID:             target.ID,
		AppName:           target.Name,
		Category:          target.Category,
		Container:         target.Container,
		ComposeProject:    target.ComposeProject,
		ComposeService:    target.ComposeService,
		ComposeFile:       target.ComposeFile,
		SystemdUnit:       target.SystemdUnit,
		PID:               target.PID,
		TargetStateDir:    store.StateDir(),
		CertsTargetDir:    store.CertsDir(),
		BackupTargetDir:   filepath.Join(store.BackupDir(), fmt.Sprintf("%s_%s", target.ID, ts)),
		DiscoveredRules:   []MigratedRulePreview{},
		DiscoveredTunnels: []config.TunnelRule{},
		DiscoveredCerts:   []CertPair{},
		CleanupSteps:      []string{},
	}

	pathMap := buildContainerMountMap(target.Container)

	switch target.Category {
	case "npm", "nginx":
		confFiles := collectNginxAndNPMConfFiles(target, pathMap)
		rules, certs := parseNginxAndNPMConfigs(confFiles, pathMap, store.CertsDir())
		plan.DiscoveredRules = rules
		plan.DiscoveredCerts = certs

	case "caddy":
		for _, f := range target.ConfigFiles {
			if strings.HasSuffix(strings.ToLower(f), "caddyfile") {
				raw, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				cRules, cCerts := parseCaddyfileWithCerts(string(raw), f, pathMap, store.CertsDir())
				plan.DiscoveredRules = append(plan.DiscoveredRules, cRules...)
				plan.DiscoveredCerts = append(plan.DiscoveredCerts, cCerts...)
			}
		}
		// 同时扫描原版 Caddy 证书目录 (/var/lib/caddy/.local/share/caddy/certificates 或容器挂载目录)
		extraCerts := scanCaddyStorageCerts(pathMap, store.CertsDir(), plan.DiscoveredRules)
		plan.DiscoveredCerts = mergeCertPairs(plan.DiscoveredCerts, extraCerts)

	case "frp":
		for _, f := range target.ConfigFiles {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			base := strings.ToLower(filepath.Base(f))
			if strings.Contains(base, "frps") {
				port, _ := parseFrpsPortAndToken(string(raw))
				if port > 0 {
					plan.DiscoveredFrpsPort = port
				}
			} else {
				if bundle, err := config.ParseImportInput(string(raw)); err == nil {
					plan.DiscoveredTunnels = append(plan.DiscoveredTunnels, bundle.Tunnels...)
				}
			}
		}
	}

	// 生成人类可读的吞并步骤说明
	plan.CleanupSteps = append(plan.CleanupSteps,
		fmt.Sprintf("1. 备份原服务配置文件到 %s", plan.BackupTargetDir),
	)
	if len(plan.DiscoveredCerts) > 0 {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("2. 提取并复制 %d 套 SSL 证书 (fullchain + privkey) 到 %s", len(plan.DiscoveredCerts), plan.CertsTargetDir),
		)
	}
	if len(plan.DiscoveredRules) > 0 {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("3. 将识别出的 %d 条反向代理站点规则转换并写入 Autofrp Caddy 配置 (%s)", len(plan.DiscoveredRules), store.CaddyfilePath()),
		)
	}
	if len(plan.DiscoveredTunnels) > 0 || plan.DiscoveredFrpsPort > 0 {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("3. 将识别出的 FRP 端口与 %d 条穿透隧道迁移到 Autofrp 配置 (%s)", len(plan.DiscoveredTunnels), store.ConfigPath()),
		)
	}
	if target.Container != "" && target.ComposeProject != "" {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("4. 仅停止 Docker Compose 容器 [%s]（项目 %s / 服务 %s），不删除容器、不修改 compose 文件；如需彻底移除请手动编辑 %s 后执行 docker compose up -d",
				target.Container, target.ComposeProject, target.ComposeService, target.ComposeFile),
		)
	} else if target.Container != "" {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("4. 停止原 Docker 容器 [%s] 并关闭其开机自启 (--restart=no)，释放占用的 80/443 等端口", target.Container),
		)
	} else if target.SystemdUnit != "" || target.Running {
		plan.CleanupSteps = append(plan.CleanupSteps,
			fmt.Sprintf("4. 停止并禁用原服务 [%s] (PID %d)，彻底释放占用的监听端口", target.Name, target.PID),
		)
	}
	plan.CleanupSteps = append(plan.CleanupSteps,
		"5. 启动/热重载 Autofrp 管理的 Caddy / FRP 服务，无缝接管全部流量",
	)

	return plan, nil
}

// ExecuteTakeover 执行「吞并迁移与老服务清理」
func ExecuteTakeover(store *config.Store, req TakeoverRequest) (*TakeoverResult, error) {
	plan, err := PreviewTakeover(store, req.AppID)
	if err != nil {
		return nil, err
	}

	apps := ScanServer(store.StateDir())
	var target *DetectedApp
	for i := range apps {
		if apps[i].ID == req.AppID {
			target = &apps[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("未找到目标服务: %s", req.AppID)
	}

	res := &TakeoverResult{
		BackupDir: plan.BackupTargetDir,
		Logs:      []string{},
	}

	// 1. 备份原配置文件到 /etc/autof/backup/<appID>_<timestamp>/
	if err := os.MkdirAll(plan.BackupTargetDir, 0700); err == nil {
		for _, cfgFile := range target.ConfigFiles {
			if data, err := os.ReadFile(cfgFile); err == nil {
				dest := filepath.Join(plan.BackupTargetDir, filepath.Base(cfgFile))
				_ = os.WriteFile(dest, data, 0600)
			}
		}
		res.Logs = append(res.Logs, fmt.Sprintf("已备份原配置文件至 %s", plan.BackupTargetDir))
	}

	// 2. 复制识别到的 SSL 证书到 /etc/autof/certs/<domain>/
	certMapByDomain := make(map[string]CertPair)
	for _, cp := range plan.DiscoveredCerts {
		certDir := filepath.Dir(cp.DestCertPath)
		if err := os.MkdirAll(certDir, 0755); err != nil {
			continue
		}
		if err1 := copyFile(cp.SrcCertPath, cp.DestCertPath, 0644); err1 == nil {
			if err2 := copyFile(cp.SrcKeyPath, cp.DestKeyPath, 0600); err2 == nil {
				res.CopiedCerts++
				certMapByDomain[cp.Domain] = cp
				res.Logs = append(res.Logs, fmt.Sprintf("已迁移域名 [%s] 的 SSL 证书至 %s", cp.Domain, certDir))
			}
		}
	}

	// 3. 将反代规则或穿透配置合入 Autofrp Store
	err = store.Update(func(c *config.Config) error {
		for _, mr := range plan.DiscoveredRules {
			tlsMode := "auto"
			certFile := ""
			keyFile := ""
			if !mr.HasSSL && strings.HasPrefix(mr.Domain, "http://") {
				tlsMode = "http"
			} else if req.UseImportedCerts {
				if cp, ok := certMapByDomain[mr.Domain]; ok {
					tlsMode = "custom"
					certFile = cp.DestCertPath
					keyFile = cp.DestKeyPath
				}
			}
			skipVerify := strings.HasPrefix(mr.Upstream, "https://")

			// 避免重复添加相同 Listen 的规则
			updated := false
			for i := range c.Caddy.Rules {
				if c.Caddy.Rules[i].Listen == mr.Domain {
					c.Caddy.Rules[i].Upstream = mr.Upstream
					c.Caddy.Rules[i].TLSMode = tlsMode
					c.Caddy.Rules[i].CertFile = certFile
					c.Caddy.Rules[i].KeyFile = keyFile
					c.Caddy.Rules[i].SkipTLSVerify = skipVerify
					c.Caddy.Rules[i].Enabled = true
					updated = true
					break
				}
			}
			if !updated {
				c.Caddy.Rules = append(c.Caddy.Rules, config.CaddyRule{
					ID:            config.GenID("caddy"),
					Name:          mr.Domain,
					Listen:        mr.Domain,
					Upstream:      mr.Upstream,
					TLSMode:       tlsMode,
					CertFile:      certFile,
					KeyFile:       keyFile,
					SkipTLSVerify: skipVerify,
					EnableEncode:  true,
					Enabled:       true,
					Remark:        fmt.Sprintf("从 %s 吞并迁移", target.Name),
				})
			}
			res.ImportedRules++
		}

		if plan.DiscoveredFrpsPort > 0 {
			c.Frps.ControlPort = plan.DiscoveredFrpsPort
		}
		for _, tun := range plan.DiscoveredTunnels {
			c.Frpc.Tunnels = append(c.Frpc.Tunnels, tun)
			res.ImportedTunnels++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 4. 停止并清理老服务（释放 80/443/7000 等端口）
	if req.StopOldService {
		cleanLogs := stopAndCleanOldApp(*target, req.RemoveOldService)
		res.Logs = append(res.Logs, cleanLogs...)
	}

	// 5. 启动/重载 Autofrp 接管服务
	if req.StartAutofService {
		if len(plan.DiscoveredRules) > 0 || target.Category == "npm" || target.Category == "nginx" || target.Category == "caddy" {
			if err := caddy.EnsureInstalledAndStarted(store); err != nil {
				res.Logs = append(res.Logs, fmt.Sprintf("提示: Caddy 自动启动返回: %v（已写入 Caddyfile 配置）", err))
				_ = caddy.WriteAndApply(store, false)
			} else {
				res.Logs = append(res.Logs, "Autofrp Caddy 已成功启动并接管所有迁移站点！")
			}
		}
		if target.Category == "frp" {
			_ = frp.WriteConfigs(store)
			if strings.Contains(target.ID, "frps") {
				_ = frp.EnsureFrpsInstalledAndStarted(store)
				res.Logs = append(res.Logs, "Autofrp frps 已启动接管！")
			} else {
				_ = frp.EnsureFrpcInstalledAndStarted(store)
				res.Logs = append(res.Logs, "Autofrp frpc 已启动接管！")
			}
		}
	} else {
		_ = caddy.WriteAndApply(store, false)
		_ = frp.WriteConfigs(store)
	}

	return res, nil
}

func stopAndCleanOldApp(app DetectedApp, removeCompletely bool) []string {
	var logs []string

	// A. Docker 容器处理
	if app.Container != "" {
		// compose 管理的容器：只停止，绝不删除、不改 restart、不碰 compose 文件
		if app.ComposeProject != "" {
			if err := exec.Command("docker", "stop", app.Container).Run(); err == nil {
				logs = append(logs, fmt.Sprintf("已停止 Compose 容器 [%s]（项目 %s / 服务 %s）",
					app.Container, app.ComposeProject, app.ComposeService))
			}
			logs = append(logs, fmt.Sprintf("提示：该容器由 compose 管理（%s），未删除容器、未修改 compose；如需彻底移除请手动编辑该文件后执行 docker compose up -d",
				app.ComposeFile))
			return logs
		}

		_ = exec.Command("docker", "update", "--restart=no", app.Container).Run()
		if err := exec.Command("docker", "stop", app.Container).Run(); err == nil {
			logs = append(logs, fmt.Sprintf("已停止原 Docker 容器 [%s] 并禁用自动重启", app.Container))
		}
		if removeCompletely {
			if err := exec.Command("docker", "rm", "-f", app.Container).Run(); err == nil {
				logs = append(logs, fmt.Sprintf("已彻底移除原 Docker 容器 [%s]", app.Container))
			}
		}
		return logs
	}

	// B. 宿主机服务处理
	var svcNames []string
	switch app.Category {
	case "npm":
		svcNames = []string{"npm", "nginx-proxy-manager", "nginx", "openresty"}
	case "nginx":
		svcNames = []string{"nginx", "openresty"}
	case "caddy":
		// 如果目标是原版 Caddy，先停掉原进程（后续会以 /etc/autof/Caddyfile 重新注册并启动）
		svcNames = []string{"caddy"}
	case "frp":
		if strings.Contains(app.ID, "frps") {
			svcNames = []string{"frps"}
		} else {
			svcNames = []string{"frpc"}
		}
	}

	for _, s := range svcNames {
		_ = exec.Command("systemctl", "stop", s).Run()
		if app.Category == "npm" || app.Category == "nginx" {
			_ = exec.Command("systemctl", "disable", s).Run()
			logs = append(logs, fmt.Sprintf("已停止并禁用宿主机服务 [%s]", s))
		}
	}

	if app.PID > 1 {
		if p, err := os.FindProcess(app.PID); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			logs = append(logs, fmt.Sprintf("已向原进程 (PID %d) 发送停止信号", app.PID))
		}
	}

	// 若原版 /etc/caddy/Caddyfile 或 /etc/frp/ 存在且选择了清理，将其重命名为 .migrated_by_autof
	if removeCompletely {
		for _, f := range app.ConfigFiles {
			if !strings.HasPrefix(f, "/etc/autof") {
				_ = os.Rename(f, f+".migrated_by_autof")
				logs = append(logs, fmt.Sprintf("已归档原配置文件 %s -> %s.migrated_by_autof", f, f))
			}
		}
	}
	time.Sleep(300 * time.Millisecond)
	return logs
}

func buildContainerMountMap(containerName string) map[string]string {
	m := make(map[string]string)
	if containerName == "" {
		return m
	}
	out, err := exec.Command("docker", "inspect", "--format", "{{json .Mounts}}", containerName).Output()
	if err != nil {
		return m
	}
	var mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}
	if err := json.Unmarshal(out, &mounts); err != nil {
		return m
	}
	for _, item := range mounts {
		if item.Destination != "" && item.Source != "" {
			m[item.Destination] = item.Source
		}
	}
	return m
}

func collectNginxAndNPMConfFiles(app *DetectedApp, pathMap map[string]string) []string {
	var files []string
	for _, f := range app.ConfigFiles {
		files = appendUniqueFile(files, f)
	}
	// 检查容器挂载的 /data/nginx/proxy_host/*.conf
	if hostData, ok := pathMap["/data"]; ok {
		if matches, err := filepath.Glob(filepath.Join(hostData, "nginx", "proxy_host", "*.conf")); err == nil {
			for _, m := range matches {
				files = appendUniqueFile(files, m)
			}
		}
	}
	// 检查宿主机常见 NPM 数据目录
	for _, dir := range []string{
		"/data/nginx/proxy_host",
		"/opt/npm/data/nginx/proxy_host",
		"/root/npm/data/nginx/proxy_host",
	} {
		if matches, err := filepath.Glob(filepath.Join(dir, "*.conf")); err == nil {
			for _, m := range matches {
				files = appendUniqueFile(files, m)
			}
		}
	}
	return files
}

func parseNginxAndNPMConfigs(confFiles []string, pathMap map[string]string, autofCertsDir string) ([]MigratedRulePreview, []CertPair) {
	var rules []MigratedRulePreview
	var certs []CertPair
	seenCertDomains := make(map[string]bool)

	for _, file := range confFiles {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		fileRules, fileCerts := parseSingleNginxConf(string(raw), file, pathMap, autofCertsDir)
		rules = append(rules, fileRules...)
		for _, c := range fileCerts {
			if !seenCertDomains[c.Domain] {
				seenCertDomains[c.Domain] = true
				certs = append(certs, c)
			}
		}
	}
	return rules, certs
}

func parseSingleNginxConf(content, sourceFile string, pathMap map[string]string, autofCertsDir string) ([]MigratedRulePreview, []CertPair) {
	var rules []MigratedRulePreview
	var certs []CertPair

	var serverNames []string
	var forwardScheme = "http"
	var forwardServer string
	var forwardPort string
	var proxyPass string
	var sslCert string
	var sslKey string
	var hasSSLListen bool

	flushServer := func() {
		upstream := ""
		if forwardServer != "" && forwardPort != "" {
			if forwardScheme == "https" {
				upstream = fmt.Sprintf("https://%s:%s", forwardServer, forwardPort)
			} else {
				upstream = fmt.Sprintf("%s:%s", forwardServer, forwardPort)
			}
		} else if proxyPass != "" {
			upstream = strings.TrimSuffix(proxyPass, "/")
			upstream = strings.TrimPrefix(upstream, "http://")
		}

		if len(serverNames) > 0 && upstream != "" && !strings.Contains(upstream, "$") {
			hostCert := resolveHostPath(sslCert, pathMap)
			hostKey := resolveHostPath(sslKey, pathMap)
			hasValidCert := hostCert != "" && hostKey != "" && fileExists(hostCert) && fileExists(hostKey)

			for _, domain := range serverNames {
				if domain == "_" || domain == "localhost" {
					continue
				}
				rules = append(rules, MigratedRulePreview{
					Domain:     domain,
					Upstream:   upstream,
					HasSSL:     hasSSLListen || hasValidCert,
					CertPath:   hostCert,
					KeyPath:    hostKey,
					SourceFile: sourceFile,
				})
				if hasValidCert {
					safeName := sanitizeDomainDir(domain)
					certs = append(certs, CertPair{
						Domain:       domain,
						SrcCertPath:  hostCert,
						SrcKeyPath:   hostKey,
						DestCertPath: filepath.Join(autofCertsDir, safeName, "fullchain.pem"),
						DestKeyPath:  filepath.Join(autofCertsDir, safeName, "privkey.pem"),
					})
				}
			}
		}

		serverNames = nil
		forwardScheme = "http"
		forwardServer = ""
		forwardPort = ""
		proxyPass = ""
		sslCert = ""
		sslKey = ""
		hasSSLListen = false
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	depth := 0
	inServer := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}

		if strings.HasPrefix(line, "server") && strings.Contains(line, "{") && depth == 0 {
			inServer = true
		}

		if inServer {
			cleanLine := strings.TrimSuffix(line, ";")
			fields := strings.Fields(cleanLine)
			if len(fields) >= 2 {
				switch fields[0] {
				case "server_name":
					for _, d := range fields[1:] {
						d = strings.Trim(d, `";'`)
						if d != "" {
							serverNames = append(serverNames, d)
						}
					}
				case "listen":
					if strings.Contains(cleanLine, "ssl") || strings.Contains(cleanLine, "443") {
						hasSSLListen = true
					}
				case "set":
					if len(fields) >= 3 {
						varName := fields[1]
						varVal := strings.Trim(fields[2], `";'`)
						switch varName {
						case "$forward_scheme":
							forwardScheme = varVal
						case "$server":
							forwardServer = varVal
						case "$port":
							forwardPort = varVal
						}
					}
				case "proxy_pass":
					val := strings.Trim(fields[1], `";'`)
					if !strings.Contains(val, "$forward_scheme") && !strings.Contains(val, "$server") {
						proxyPass = val
					}
				case "ssl_certificate":
					sslCert = strings.Trim(fields[1], `";'`)
				case "ssl_certificate_key":
					sslKey = strings.Trim(fields[1], `";'`)
				}
			}
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if inServer && depth <= 0 {
			depth = 0
			inServer = false
			flushServer()
		}
	}
	if inServer {
		flushServer()
	}

	return rules, certs
}

func parseCaddyfileWithCerts(raw, sourceFile string, pathMap map[string]string, autofCertsDir string) ([]MigratedRulePreview, []CertPair) {
	parsedRules := parseExistingCaddyfile(raw)
	var previews []MigratedRulePreview
	var certs []CertPair

	for _, r := range parsedRules {
		previews = append(previews, MigratedRulePreview{
			Domain:     r.Listen,
			Upstream:   r.Upstream,
			HasSSL:     r.TLSMode != "http",
			SourceFile: sourceFile,
		})
	}
	return previews, certs
}

func scanCaddyStorageCerts(pathMap map[string]string, autofCertsDir string, rules []MigratedRulePreview) []CertPair {
	var certs []CertPair
	storageDirs := []string{
		"/var/lib/caddy/.local/share/caddy/certificates",
		"/root/.local/share/caddy/certificates",
		"/data/caddy/certificates",
	}
	if hostData, ok := pathMap["/data"]; ok {
		storageDirs = append(storageDirs, filepath.Join(hostData, "caddy", "certificates"))
	}

	for _, r := range rules {
		domain := strings.Split(strings.TrimPrefix(strings.TrimPrefix(r.Domain, "https://"), "http://"), ":")[0]
		if domain == "" {
			continue
		}
		for _, baseDir := range storageDirs {
			matches, _ := filepath.Glob(filepath.Join(baseDir, "*", domain, domain+".crt"))
			for _, crt := range matches {
				key := strings.TrimSuffix(crt, ".crt") + ".key"
				if fileExists(crt) && fileExists(key) {
					safe := sanitizeDomainDir(domain)
					certs = append(certs, CertPair{
						Domain:       r.Domain,
						SrcCertPath:  crt,
						SrcKeyPath:   key,
						DestCertPath: filepath.Join(autofCertsDir, safe, "fullchain.pem"),
						DestKeyPath:  filepath.Join(autofCertsDir, safe, "privkey.pem"),
					})
				}
			}
		}
	}
	return certs
}

func resolveHostPath(containerOrHostPath string, pathMap map[string]string) string {
	p := strings.TrimSpace(containerOrHostPath)
	if p == "" {
		return ""
	}
	mapped := p
	for containerPrefix, hostPrefix := range pathMap {
		if strings.HasPrefix(p, containerPrefix+"/") || p == containerPrefix {
			rel := strings.TrimPrefix(p, containerPrefix)
			mapped = filepath.Join(hostPrefix, rel)
			break
		}
	}

	// 处理 Let's Encrypt live/npm-1/fullchain.pem 软链接在容器内指向 ../../archive/... 的情况
	info, err := os.Lstat(mapped)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(mapped)
		if err == nil {
			if !filepath.IsAbs(linkTarget) {
				resolved := filepath.Clean(filepath.Join(filepath.Dir(mapped), linkTarget))
				if fileExists(resolved) {
					return resolved
				}
			} else {
				for cPrefix, hPrefix := range pathMap {
					if strings.HasPrefix(linkTarget, cPrefix+"/") {
						resolved := filepath.Join(hPrefix, strings.TrimPrefix(linkTarget, cPrefix))
						if fileExists(resolved) {
							return resolved
						}
					}
				}
			}
		}
	}
	return mapped
}

func parseFrpsPortAndToken(raw string) (int, string) {
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
	return port, token
}

func sanitizeDomainDir(d string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	s = strings.ReplaceAll(s, "*", "_wildcard_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func mergeCertPairs(a, b []CertPair) []CertPair {
	seen := make(map[string]bool)
	var out []CertPair
	for _, item := range append(a, b...) {
		if !seen[item.Domain] {
			seen[item.Domain] = true
			out = append(out, item)
		}
	}
	return out
}

var _ = errors.New
