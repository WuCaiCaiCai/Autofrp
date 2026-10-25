package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// Store 管理配置文件的并发安全读写与路径定位
type Store struct {
	mu       sync.RWMutex
	stateDir string
	cfg      *Config
}

// ResolveStateDir 返回配置目录：优先环境变量 AUTOF_STATE_DIR，root 用 /etc/autof，普通用户用 ~/.config/autof
func ResolveStateDir() string {
	if env := strings.TrimSpace(os.Getenv("AUTOF_STATE_DIR")); env != "" {
		return env
	}
	if os.Geteuid() == 0 {
		return "/etc/autof"
	}
	if f, err := os.OpenFile("/etc/.autof_write_test", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err == nil {
		f.Close()
		_ = os.Remove("/etc/.autof_write_test")
		return "/etc/autof"
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "autof")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/tmp/autof"
	}
	return filepath.Join(home, ".config", "autof")
}

// NewStore 创建并加载配置存储
func NewStore(customDir string) (*Store, error) {
	dir := customDir
	if dir == "" {
		dir = ResolveStateDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败 %s: %w", dir, err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "clients"), 0755)

	s := &Store{stateDir: dir}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) StateDir() string       { return s.stateDir }
func (s *Store) ConfigPath() string     { return filepath.Join(s.stateDir, "config.json") }
func (s *Store) FrpsConfPath() string   { return filepath.Join(s.stateDir, "frps.toml") }
func (s *Store) FrpcConfPath() string   { return filepath.Join(s.stateDir, "frpc.toml") }
func (s *Store) ClientExportPath() string { return filepath.Join(s.stateDir, "clients", "frpc.toml") }
func (s *Store) CaddyfilePath() string  { return filepath.Join(s.stateDir, "Caddyfile") }

// DefaultConfig 生成带合理默认值的初始配置
func DefaultConfig() *Config {
	return &Config{
		Version:  AppVersion,
		NodeRole: "both",
		Network: NetConfig{
			IPPref:   "4",
			V4Status: "未探测",
			V6Status: "未探测",
		},
		Web: WebConfig{
			ListenAddr:  "0.0.0.0:7500",
			Username:    "admin",
			Password:    GenToken(12),
			AuthEnabled: true,
		},
		Frps: FrpsConfig{
			ControlPort:    7000,
			Token:          GenToken(24),
			AllowPortsMode: "unlimited",
			Services:       []TunnelRule{},
		},
		Frpc: FrpcConfig{
			ServerPort: 7000,
			TLSEnable:  true,
			AdminPort:  7400,
			Tunnels:    []TunnelRule{},
		},
		Caddy: CaddyConfig{
			HTTPPort:  80,
			HTTPSPort: 443,
			Rules:     []CaddyRule{},
		},
	}
}

// GenToken 生成指定长度的字母数字随机串
func GenToken(length int) string {
	if length <= 0 {
		length = 16
	}
	b := make([]byte, length)
	_, _ = rand.Read(b)
	out := make([]byte, length)
	for i := range b {
		out[i] = tokenAlphabet[int(b[i])%len(tokenAlphabet)]
	}
	return string(out)
}

// GenID 生成简短唯一 ID
func GenID(prefix string) string {
	return prefix + "_" + strings.ToLower(GenToken(8))
}

// Load 读取 config.json；若不存在则尝试从旧版 state.conf/services.conf 迁移，否则初始化默认配置
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfgPath := s.ConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err == nil && len(data) > 0 {
		cfg := DefaultConfig()
		if err := json.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("解析配置文件失败 %s: %w", cfgPath, err)
		}
		normalizeConfig(cfg)
		s.cfg = cfg
		return nil
	}

	// 尝试从 v1 (state.conf + services.conf) 迁移
	cfg := DefaultConfig()
	migrated := migrateLegacyV1(s.stateDir, cfg)
	normalizeConfig(cfg)
	s.cfg = cfg
	if migrated {
		_ = s.saveLocked()
	}
	return nil
}

// Save 将当前配置原子写入磁盘
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeConfig(s.cfg)
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.ConfigPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.ConfigPath())
}

// Get 返回当前配置的深拷贝快照
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.cfg)
	var copyCfg Config
	_ = json.Unmarshal(b, &copyCfg)
	return copyCfg
}

// Update 在写锁保护下修改配置并自动持久化
func (s *Store) Update(fn func(cfg *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.cfg); err != nil {
		return err
	}
	normalizeConfig(s.cfg)
	return s.saveLocked()
}

// PublicAddr 根据网络偏好返回首选公网地址
func (c *Config) PublicAddr() string {
	switch c.Network.IPPref {
	case "6":
		if c.Network.PublicIPv6 != "" {
			return c.Network.PublicIPv6
		}
		return c.Network.PublicIPv4
	default:
		if c.Network.PublicIPv4 != "" {
			return c.Network.PublicIPv4
		}
		return c.Network.PublicIPv6
	}
}

// PublicAddrAlt 当网络偏好为 both 且同时具备 v4/v6 时返回备选 IPv6
func (c *Config) PublicAddrAlt() string {
	if c.Network.IPPref == "both" && c.Network.PublicIPv4 != "" && c.Network.PublicIPv6 != "" {
		return c.Network.PublicIPv6
	}
	return ""
}

// NetPrefLabel 返回人类可读的网络偏好名称
func (c *Config) NetPrefLabel() string {
	switch c.Network.IPPref {
	case "4":
		return "仅 IPv4"
	case "6":
		return "仅 IPv6"
	case "both":
		return "均可（双栈）"
	default:
		return "仅 IPv4"
	}
}

func normalizeConfig(cfg *Config) {
	cfg.Version = AppVersion
	if cfg.NodeRole == "" {
		cfg.NodeRole = "both"
	}
	if cfg.Network.IPPref == "" {
		cfg.Network.IPPref = "4"
	}
	if cfg.Web.ListenAddr == "" {
		cfg.Web.ListenAddr = "0.0.0.0:7500"
	}
	if cfg.Web.Username == "" {
		cfg.Web.Username = "admin"
	}
	if cfg.Web.Password == "" {
		cfg.Web.Password = GenToken(12)
	}
	if cfg.Frps.ControlPort <= 0 || cfg.Frps.ControlPort > 65535 {
		cfg.Frps.ControlPort = 7000
	}
	if cfg.Frps.Token == "" {
		cfg.Frps.Token = GenToken(24)
	}
	if cfg.Frps.AllowPortsMode == "" {
		cfg.Frps.AllowPortsMode = "unlimited"
	}
	if cfg.Frpc.ServerPort <= 0 || cfg.Frpc.ServerPort > 65535 {
		cfg.Frpc.ServerPort = cfg.Frps.ControlPort
	}
	if cfg.Frpc.AdminPort <= 0 || cfg.Frpc.AdminPort > 65535 {
		cfg.Frpc.AdminPort = 7400
	}
	if cfg.Caddy.HTTPPort <= 0 || cfg.Caddy.HTTPPort > 65535 {
		cfg.Caddy.HTTPPort = 80
	}
	if cfg.Caddy.HTTPSPort <= 0 || cfg.Caddy.HTTPSPort > 65535 {
		cfg.Caddy.HTTPSPort = 443
	}
	if cfg.Frps.Services == nil {
		cfg.Frps.Services = []TunnelRule{}
	}
	if cfg.Frpc.Tunnels == nil {
		cfg.Frpc.Tunnels = []TunnelRule{}
	}
	if cfg.Caddy.Rules == nil {
		cfg.Caddy.Rules = []CaddyRule{}
	}

	for i := range cfg.Frps.Services {
		normalizeTunnel(&cfg.Frps.Services[i], i+1)
	}
	for i := range cfg.Frpc.Tunnels {
		normalizeTunnel(&cfg.Frpc.Tunnels[i], i+1)
	}
	SyncLinkedCaddyRules(cfg)
	for i := range cfg.Caddy.Rules {
		normalizeCaddyRule(&cfg.Caddy.Rules[i], i+1)
	}
}

func normalizeTunnel(t *TunnelRule, idx int) {
	if t.ID == "" {
		t.ID = GenID("tun")
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		t.Name = fmt.Sprintf("proxy-%d", idx)
	}
	t.Type = strings.ToLower(strings.TrimSpace(t.Type))
	if t.Type != "udp" {
		t.Type = "tcp"
	}
	t.LocalIP = strings.TrimSpace(t.LocalIP)
	if t.LocalIP == "" {
		t.LocalIP = "127.0.0.1"
	}
	if t.RemotePort <= 0 && t.LocalPort > 0 {
		t.RemotePort = t.LocalPort
	}
	t.CaddyDomain = strings.TrimSpace(t.CaddyDomain)
	if t.CaddyTLSMode == "" {
		t.CaddyTLSMode = "auto"
	}
}

func normalizeCaddyRule(r *CaddyRule, idx int) {
	if r.ID == "" {
		r.ID = GenID("caddy")
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = fmt.Sprintf("rule-%d", idx)
	}
	r.Listen = strings.TrimSpace(r.Listen)
	r.Upstream = strings.TrimSpace(r.Upstream)
	if r.TLSMode == "" {
		r.TLSMode = "auto"
	}
}

// SyncLinkedCaddyRules 将穿透服务（Frps.Services 与 Frpc.Tunnels）中配置了 CaddyDomain 的条目自动同步到 Caddy.Rules
func SyncLinkedCaddyRules(cfg *Config) {
	activeLinks := make(map[string]TunnelRule)
	for _, svc := range cfg.Frps.Services {
		if svc.CaddyDomain != "" && svc.RemotePort > 0 {
			activeLinks[svc.ID] = svc
		}
	}
	for _, tun := range cfg.Frpc.Tunnels {
		if tun.CaddyDomain != "" && tun.RemotePort > 0 {
			activeLinks[tun.ID] = tun
		}
	}

	existingByTunnel := make(map[string]int)
	keptRules := make([]CaddyRule, 0, len(cfg.Caddy.Rules))
	for _, r := range cfg.Caddy.Rules {
		if r.LinkedTunnelID != "" {
			if _, ok := activeLinks[r.LinkedTunnelID]; !ok {
				// 穿透规则已取消域名绑定或已删除，自动移除对应联动规则
				continue
			}
			existingByTunnel[r.LinkedTunnelID] = len(keptRules)
		}
		keptRules = append(keptRules, r)
	}

	for tunID, svc := range activeLinks {
		upstream := "127.0.0.1:" + strconv.Itoa(svc.RemotePort)
		tlsMode := svc.CaddyTLSMode
		if tlsMode == "" {
			tlsMode = "auto"
		}
		if idx, exists := existingByTunnel[tunID]; exists {
			keptRules[idx].Name = "穿透反代: " + svc.Name
			keptRules[idx].Listen = svc.CaddyDomain
			keptRules[idx].Upstream = upstream
			keptRules[idx].TLSMode = tlsMode
			keptRules[idx].Enabled = svc.Enabled
		} else {
			keptRules = append(keptRules, CaddyRule{
				ID:             GenID("caddy"),
				Name:           "穿透反代: " + svc.Name,
				Listen:         svc.CaddyDomain,
				Upstream:       upstream,
				TLSMode:        tlsMode,
				EnableEncode:   true,
				LinkedTunnelID: tunID,
				Enabled:        svc.Enabled,
				Remark:         "由穿透服务 [" + svc.Name + "] 自动联动生成",
			})
		}
	}

	cfg.Caddy.Rules = keptRules
}
