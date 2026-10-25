package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/caddy"
	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/frp"
	"github.com/WuCaiCaiCai/Autofrp/internal/netenv"
	"github.com/WuCaiCaiCai/Autofrp/internal/scanner"
	"github.com/WuCaiCaiCai/Autofrp/internal/systemd"
)

func (s *Server) buildOverviewResponse() map[string]any {
	cfg := s.store.Get()
	connCode, _ := cfg.ExportConnectionCode()

	return map[string]any{
		"config":       cfg,
		"stateDir":     s.store.StateDir(),
		"systemInfo":   netenv.DetectSystemInfo(),
		"detectedApps": scanner.ScanServer(s.store.StateDir()),
		"legacy":       s.store.LegacyStatus(),
		"services": map[string]any{
			"frps":  systemd.QueryStatus("frps"),
			"frpc":  systemd.QueryStatus("frpc"),
			"caddy": systemd.QueryStatus("caddy"),
			"autof": systemd.QueryStatus("autof"),
		},
		"presets": frp.BuiltinPresets(),
		"exports": map[string]string{
			"connCode":       connCode,
			"frpsToml":       frp.GenerateFrpsToml(cfg),
			"frpcExportToml": frp.GenerateFrpcTomlForExport(cfg, -1),
			"frpcClientToml": frp.GenerateFrpcTomlForClient(cfg),
			"caddyfile":      caddy.GenerateCaddyfile(cfg),
		},
	}
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleNetRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := s.store.Get()
	netCfg := cfg.Network
	netenv.DetectAndApply(&netCfg)
	_ = s.store.Update(func(c *config.Config) error {
		c.Network = netCfg
		return nil
	})
	_ = frp.WriteConfigs(s.store)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleNetPref(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		IPPref   string `json:"ipPref"`
		NodeRole string `json:"nodeRole"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	err := s.store.Update(func(c *config.Config) error {
		if req.IPPref == "4" || req.IPPref == "6" || req.IPPref == "both" {
			c.Network.IPPref = req.IPPref
		}
		if req.NodeRole == "vps" || req.NodeRole == "local" || req.NodeRole == "both" {
			c.NodeRole = req.NodeRole
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.IPPref != "" {
		_ = frp.ApplyFrps(s.store, true)
	}
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Target      string `json:"target"`      // "frps", "frpc", "caddy"
		Action      string `json:"action"`      // "install", "start", "stop", "restart", "reload"
		DNSProvider string `json:"dnsProvider"` // 可选，供 caddy 安装 DNS 插件
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}

	var err error
	switch req.Target {
	case "frps":
		switch req.Action {
		case "install":
			_, err = frp.InstallBinaries(s.store)
		case "start":
			err = frp.EnsureFrpsInstalledAndStarted(s.store)
		case "restart", "reload":
			_ = frp.WriteConfigs(s.store)
			err = systemd.Control("frps", "restart", systemd.FindBinary("frps"), []string{"-c", s.store.FrpsConfPath()}, s.store.StateDir(), nil)
		case "stop":
			err = systemd.Control("frps", "stop", systemd.FindBinary("frps"), nil, s.store.StateDir(), nil)
		default:
			err = errors.New("不支持的操作")
		}
	case "frpc":
		switch req.Action {
		case "install":
			_, err = frp.InstallBinaries(s.store)
		case "start":
			err = frp.EnsureFrpcInstalledAndStarted(s.store)
		case "reload":
			err = frp.ApplyFrpc(s.store, true)
		case "restart":
			_ = frp.WriteConfigs(s.store)
			err = systemd.Control("frpc", "restart", systemd.FindBinary("frpc"), []string{"-c", s.store.FrpcConfPath()}, s.store.StateDir(), nil)
		case "stop":
			err = systemd.Control("frpc", "stop", systemd.FindBinary("frpc"), nil, s.store.StateDir(), nil)
		default:
			err = errors.New("不支持的操作")
		}
	case "caddy":
		switch req.Action {
		case "install":
			provider := req.DNSProvider
			if provider == "" {
				provider = s.store.Get().Caddy.DNSProvider
			}
			_, err = caddy.InstallCaddy(provider)
		case "start":
			err = caddy.EnsureInstalledAndStarted(s.store)
		case "reload":
			err = caddy.WriteAndApply(s.store, true)
		case "restart":
			if err = caddy.WriteAndApply(s.store, false); err == nil {
				err = systemd.Control("caddy", "restart", systemd.FindBinary("caddy"), []string{"run", "--config", s.store.CaddyfilePath(), "--adapter", "caddyfile"}, s.store.StateDir(), nil)
			}
		case "stop":
			err = systemd.Control("caddy", "stop", systemd.FindBinary("caddy"), nil, s.store.StateDir(), nil)
		default:
			err = errors.New("不支持的操作")
		}
	case "autof":
		switch req.Action {
		case "restart", "start", "stop", "reload":
			action := req.Action
			if action == "start" {
				action = "restart"
			}
			err = systemd.Control("autof", action, systemd.FindBinary("autof"), []string{"web"}, s.store.StateDir(), nil)
		default:
			err = errors.New("不支持的操作")
		}
	default:
		err = errors.New("未知组件: " + req.Target)
	}

	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	switch target {
	case "frps", "frpc", "caddy", "autof":
	default:
		target = "frps"
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 500 {
		lines = 120
	}
	logs := systemd.ReadLogs(target, s.store.StateDir(), lines)
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleFrpsSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		ControlPort     int    `json:"controlPort"`
		Token           string `json:"token"`
		AllowPortsMode  string `json:"allowPortsMode"`
		AllowPortsRange string `json:"allowPortsRange"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if req.ControlPort < 1 || req.ControlPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "控制端口必须在 1 ~ 65535 之间"})
		return
	}
	err := s.store.Update(func(c *config.Config) error {
		c.Frps.ControlPort = req.ControlPort
		if strings.TrimSpace(req.Token) != "" {
			c.Frps.Token = strings.TrimSpace(req.Token)
		}
		if req.AllowPortsMode == "unlimited" || req.AllowPortsMode == "range" || req.AllowPortsMode == "strict" {
			c.Frps.AllowPortsMode = req.AllowPortsMode
		}
		c.Frps.AllowPortsRange = strings.TrimSpace(req.AllowPortsRange)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = frp.ApplyFrps(s.store, true)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleFrpsService(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var rule config.TunnelRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		if rule.LocalPort < 1 || rule.LocalPort > 65535 || rule.RemotePort < 1 || rule.RemotePort > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "内网端口和对外端口必须在 1 ~ 65535 之间"})
			return
		}
		err := s.store.Update(func(c *config.Config) error {
			upsertTunnel(&c.Frps.Services, rule)
			return nil
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// 仅当 allowPorts 为 strict 模式时修改服务才需重启 frps
		restartFrps := s.store.Get().Frps.AllowPortsMode == "strict"
		_ = frp.ApplyFrps(s.store, restartFrps)
		if rule.CaddyDomain != "" {
			_ = caddy.WriteAndApply(s.store, true)
		}
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		_ = s.store.Update(func(c *config.Config) error {
			removeTunnel(&c.Frps.Services, id)
			return nil
		})
		restartFrps := s.store.Get().Frps.AllowPortsMode == "strict"
		_ = frp.ApplyFrps(s.store, restartFrps)
		_ = caddy.WriteAndApply(s.store, true)
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func (s *Server) handleFrpcSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		ServerAddr string `json:"serverAddr"`
		ServerPort int    `json:"serverPort"`
		Token      string `json:"token"`
		TLSEnable  bool   `json:"tlsEnable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	if req.ServerPort < 1 || req.ServerPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "远程控制端口必须在 1 ~ 65535 之间"})
		return
	}
	_ = s.store.Update(func(c *config.Config) error {
		c.Frpc.ServerAddr = strings.TrimSpace(req.ServerAddr)
		c.Frpc.ServerPort = req.ServerPort
		c.Frpc.Token = strings.TrimSpace(req.Token)
		c.Frpc.TLSEnable = req.TLSEnable
		return nil
	})
	_ = frp.ApplyFrpc(s.store, true)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleFrpcImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Input string `json:"input"`
		Merge bool   `json:"merge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	bundle, err := config.ParseImportInput(req.Input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.store.Update(func(c *config.Config) error {
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
		if req.Merge {
			for _, t := range bundle.Tunnels {
				upsertTunnel(&c.Frpc.Tunnels, t)
			}
		} else if len(bundle.Tunnels) > 0 {
			c.Frpc.Tunnels = bundle.Tunnels
		}
		return nil
	})
	_ = frp.ApplyFrpc(s.store, true)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleFrpcTunnel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var rule config.TunnelRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		if rule.LocalPort < 1 || rule.LocalPort > 65535 || rule.RemotePort < 1 || rule.RemotePort > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "端口必须在 1 ~ 65535 之间"})
			return
		}
		_ = s.store.Update(func(c *config.Config) error {
			upsertTunnel(&c.Frpc.Tunnels, rule)
			return nil
		})
		_ = frp.ApplyFrpc(s.store, true)
		if rule.CaddyDomain != "" {
			_ = caddy.WriteAndApply(s.store, true)
		}
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		_ = s.store.Update(func(c *config.Config) error {
			removeTunnel(&c.Frpc.Tunnels, id)
			return nil
		})
		_ = frp.ApplyFrpc(s.store, true)
		_ = caddy.WriteAndApply(s.store, true)
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func (s *Server) handleCaddySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		HTTPPort     int    `json:"httpPort"`
		HTTPSPort    int    `json:"httpsPort"`
		Email        string `json:"email"`
		DNSProvider  string `json:"dnsProvider"`
		DNSToken     string `json:"dnsToken"`
		DNSSecret    string `json:"dnsSecret"`
		CustomGlobal string `json:"customGlobal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	prev := s.store.Get()
	_ = s.store.Update(func(c *config.Config) error {
		if req.HTTPPort > 0 && req.HTTPPort <= 65535 {
			c.Caddy.HTTPPort = req.HTTPPort
		}
		if req.HTTPSPort > 0 && req.HTTPSPort <= 65535 {
			c.Caddy.HTTPSPort = req.HTTPSPort
		}
		c.Caddy.Email = strings.TrimSpace(req.Email)
		c.Caddy.DNSProvider = strings.TrimSpace(req.DNSProvider)
		c.Caddy.DNSToken = strings.TrimSpace(req.DNSToken)
		c.Caddy.DNSSecret = strings.TrimSpace(req.DNSSecret)
		c.Caddy.CustomGlobal = strings.TrimSpace(req.CustomGlobal)
		return nil
	})
	if err := caddy.WriteAndApply(s.store, true); err != nil {
		// 语法校验失败时回滚
		_ = s.store.Update(func(c *config.Config) error {
			c.Caddy = prev.Caddy
			return nil
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleCaddyRule(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var rule config.CaddyRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		if strings.TrimSpace(rule.Listen) == "" || strings.TrimSpace(rule.Upstream) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "前端监听地址和后端目标地址不能为空"})
			return
		}
		prev := s.store.Get()
		_ = s.store.Update(func(c *config.Config) error {
			upsertCaddyRule(c, rule)
			return nil
		})
		if err := caddy.WriteAndApply(s.store, true); err != nil {
			_ = s.store.Update(func(c *config.Config) error {
				*c = prev
				return nil
			})
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		_ = s.store.Update(func(c *config.Config) error {
			kept := make([]config.CaddyRule, 0, len(c.Caddy.Rules))
			for _, item := range c.Caddy.Rules {
				if item.ID == id {
					// 若是联动规则，同步清除穿透条目上的 CaddyDomain
					if item.LinkedTunnelID != "" {
						clearLinkedDomain(&c.Frps.Services, item.LinkedTunnelID)
						clearLinkedDomain(&c.Frpc.Tunnels, item.LinkedTunnelID)
					}
					continue
				}
				kept = append(kept, item)
			}
			c.Caddy.Rules = kept
			return nil
		})
		_ = caddy.WriteAndApply(s.store, true)
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func (s *Server) handleSystemWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		ListenAddr  string `json:"listenAddr"`
		TLSListen   string `json:"tlsListen"`
		TLSCertFile string `json:"tlsCertFile"`
		TLSKeyFile  string `json:"tlsKeyFile"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		AuthEnabled bool   `json:"authEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}

	tlsListen := strings.TrimSpace(req.TLSListen)
	certFile := strings.TrimSpace(req.TLSCertFile)
	keyFile := strings.TrimSpace(req.TLSKeyFile)
	if tlsListen != "" {
		if certFile == "" || keyFile == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "启用 HTTPS 监听时必须填写证书与私钥路径"})
			return
		}
		for _, f := range []string{certFile, keyFile} {
			if st, err := os.Stat(f); err != nil || st.IsDir() {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "证书或私钥文件不存在: " + f})
				return
			}
		}
	}

	_ = s.store.Update(func(c *config.Config) error {
		if strings.TrimSpace(req.ListenAddr) != "" {
			c.Web.ListenAddr = strings.TrimSpace(req.ListenAddr)
		}
		c.Web.TLSListen = tlsListen
		c.Web.TLSCertFile = certFile
		c.Web.TLSKeyFile = keyFile
		if strings.TrimSpace(req.Username) != "" {
			c.Web.Username = strings.TrimSpace(req.Username)
		}
		if req.Password != "" {
			c.Web.Password = req.Password
		}
		c.Web.AuthEnabled = req.AuthEnabled
		return nil
	})
	cfg := s.store.Get()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    computeSessionToken(cfg.Web.Username, cfg.Web.Password),
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400 * 30,
	})
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

// handleSystemInit 处理首次初始化向导：写入角色、网络偏好、控制端口与 Token
func (s *Server) handleSystemInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		NodeRole    string `json:"nodeRole"`
		IPPref      string `json:"ipPref"`
		ControlPort int    `json:"controlPort"`
		Token       string `json:"token"`
		FrpcAddr    string `json:"frpcAddr"`
		FrpcPort    int    `json:"frpcPort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	err := s.store.Update(func(c *config.Config) error {
		switch req.NodeRole {
		case "vps", "local", "both":
			c.NodeRole = req.NodeRole
		}
		switch req.IPPref {
		case "4", "6", "both":
			c.Network.IPPref = req.IPPref
		}
		if req.ControlPort >= 1 && req.ControlPort <= 65535 {
			c.Frps.ControlPort = req.ControlPort
			if c.Frpc.ServerAddr == "" && c.NodeRole != "local" {
				c.Frpc.ServerPort = req.ControlPort
			}
		}
		if t := strings.TrimSpace(req.Token); t != "" {
			c.Frps.Token = t
		}
		if a := strings.TrimSpace(req.FrpcAddr); a != "" {
			c.Frpc.ServerAddr = a
		}
		if req.FrpcPort >= 1 && req.FrpcPort <= 65535 {
			c.Frpc.ServerPort = req.FrpcPort
		}
		c.SetupCompleted = true
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = frp.WriteConfigs(s.store)
	_ = caddy.WriteAndApply(s.store, false)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

// handleFrpcParse 解析连接码 / frpc.toml，供导入前预览
func (s *Server) handleFrpcParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	bundle, err := config.ParseImportInput(req.Input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"serverAddr":  bundle.ServerAddr,
		"serverPort":  bundle.ServerPort,
		"altIPv6":     bundle.ServerAltIP6,
		"hasToken":    bundle.Token != "",
		"tlsEnable":   bundle.TLSEnable,
		"tunnelCount": len(bundle.Tunnels),
		"tunnels":     bundle.Tunnels,
	})
}

func (s *Server) handleScannerFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path := strings.TrimSpace(r.URL.Query().Get("path"))
		content, err := scanner.ReadExternalFile(path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": path, "content": content})

	case http.MethodPost:
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			Reload  bool   `json:"reload"`
			AppID   string `json:"appId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		if err := scanner.SaveExternalFile(req.Path, req.Content, req.Reload, req.AppID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.buildOverviewResponse())

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func (s *Server) handleScannerImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Category   string `json:"category"`
		ConfigPath string `json:"configPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	count, err := scanner.ImportExternalConfig(s.store, req.Category, req.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = frp.WriteConfigs(s.store)
	_ = caddy.WriteAndApply(s.store, false)
	resp := s.buildOverviewResponse()
	resp["importedCount"] = count
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTakeoverPreview(w http.ResponseWriter, r *http.Request) {
	appID := strings.TrimSpace(r.URL.Query().Get("appId"))
	plan, err := scanner.PreviewTakeover(s.store, appID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleTakeoverExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req scanner.TakeoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	res, err := scanner.ExecuteTakeover(s.store, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	resp := s.buildOverviewResponse()
	resp["takeoverResult"] = res
	writeJSON(w, http.StatusOK, resp)
}

// handleGenerateSelfSignedTLS 为面板生成自签名证书，并写入 HTTPS 配置（重启面板后生效）
func (s *Server) handleGenerateSelfSignedTLS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := s.store.Get()
	tlsDir := filepath.Join(s.store.StateDir(), "tls")
	certFile := filepath.Join(tlsDir, "panel.crt")
	keyFile := filepath.Join(tlsDir, "panel.key")

	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if h := cfg.PublicAddr(); h != "" {
		hosts = append(hosts, h)
	}
	if cfg.Network.LocalIPv4 != "" {
		hosts = append(hosts, cfg.Network.LocalIPv4)
	}
	if hn, err := os.Hostname(); err == nil && hn != "" {
		hosts = append(hosts, hn)
	}

	if err := GenerateSelfSignedCert(certFile, keyFile, hosts); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成自签证书失败: " + err.Error()})
		return
	}

	listen := cfg.Web.TLSListen
	if listen == "" {
		listen = "0.0.0.0:7443"
	}
	_ = s.store.Update(func(c *config.Config) error {
		c.Web.TLSListen = listen
		c.Web.TLSCertFile = certFile
		c.Web.TLSKeyFile = keyFile
		return nil
	})

	resp := s.buildOverviewResponse()
	resp["tlsGenerated"] = map[string]string{
		"listen":   listen,
		"certFile": certFile,
		"keyFile":  keyFile,
	}
	writeJSON(w, http.StatusOK, resp)
}

func upsertTunnel(list *[]config.TunnelRule, item config.TunnelRule) {
	if item.ID != "" {
		for i := range *list {
			if (*list)[i].ID == item.ID {
				(*list)[i] = item
				return
			}
		}
	}
	// 确保名称不冲突
	base := strings.TrimSpace(item.Name)
	if base == "" {
		base = "proxy"
	}
	name := base
	seq := 1
	for hasTunnelName(*list, name, item.ID) {
		seq++
		name = base + "-" + strconv.Itoa(seq)
	}
	item.Name = name
	item.ID = config.GenID("tun")
	*list = append(*list, item)
}

func hasTunnelName(list []config.TunnelRule, name, excludeID string) bool {
	for _, t := range list {
		if t.ID != excludeID && t.Name == name {
			return true
		}
	}
	return false
}

func removeTunnel(list *[]config.TunnelRule, id string) {
	kept := make([]config.TunnelRule, 0, len(*list))
	for _, t := range *list {
		if t.ID != id && t.Name != id {
			kept = append(kept, t)
		}
	}
	*list = kept
}

func upsertCaddyRule(cfg *config.Config, rule config.CaddyRule) {
	if rule.ID != "" {
		for i := range cfg.Caddy.Rules {
			if cfg.Caddy.Rules[i].ID == rule.ID {
				rule.LinkedTunnelID = cfg.Caddy.Rules[i].LinkedTunnelID
				cfg.Caddy.Rules[i] = rule
				// 如果是穿透联动规则，同步更新穿透规则上的域名
				if rule.LinkedTunnelID != "" {
					syncDomainBackToTunnel(&cfg.Frps.Services, rule.LinkedTunnelID, rule.Listen, rule.TLSMode)
					syncDomainBackToTunnel(&cfg.Frpc.Tunnels, rule.LinkedTunnelID, rule.Listen, rule.TLSMode)
				}
				return
			}
		}
	}
	rule.ID = config.GenID("caddy")
	cfg.Caddy.Rules = append(cfg.Caddy.Rules, rule)
}

func syncDomainBackToTunnel(list *[]config.TunnelRule, tunID, domain, tlsMode string) {
	for i := range *list {
		if (*list)[i].ID == tunID {
			(*list)[i].CaddyDomain = domain
			(*list)[i].CaddyTLSMode = tlsMode
		}
	}
}

func clearLinkedDomain(list *[]config.TunnelRule, tunID string) {
	for i := range *list {
		if (*list)[i].ID == tunID {
			(*list)[i].CaddyDomain = ""
		}
	}
}
