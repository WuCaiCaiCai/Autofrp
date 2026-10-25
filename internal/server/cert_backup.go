package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/backup"
	"github.com/WuCaiCaiCai/Autofrp/internal/caddy"
	"github.com/WuCaiCaiCai/Autofrp/internal/cert"
	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/frp"
	"github.com/WuCaiCaiCai/Autofrp/internal/systemd"
)

// ---------------- 证书管理 ----------------

func (s *Server) handleCertIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Name      string `json:"name"`
		Domains   string `json:"domains"`
		KeyType   string `json:"keyType"`
		Provider  string `json:"provider"`
		TargetDir string `json:"targetDir"`
		AutoRenew bool   `json:"autoRenew"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}

	rawDomains := strings.FieldsFunc(req.Domains, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	domains, err := cert.NormalizeDomains(rawDomains)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	cfg := s.store.Get()
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = cfg.Caddy.DNSProvider
	}
	if provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请先在 Caddy 设置中配置 DNS 服务商与 API 凭据"})
		return
	}

	entry := config.CertEntry{
		ID:        config.GenID("cert"),
		Name:      strings.TrimSpace(req.Name),
		Domains:   domains,
		KeyType:   strings.TrimSpace(req.KeyType),
		Provider:  provider,
		TargetDir: strings.TrimSpace(req.TargetDir),
		AutoRenew: req.AutoRenew,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	if entry.Name == "" {
		entry.Name = domains[0]
	}
	if entry.TargetDir == "" {
		base := strings.TrimPrefix(strings.TrimPrefix(domains[0], "*."), ".")
		base = strings.ReplaceAll(base, ":", "_")
		entry.TargetDir = filepath.Join(s.store.CertsDir(), base)
	}
	if !filepath.IsAbs(entry.TargetDir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "证书输出目录必须是绝对路径"})
		return
	}
	if entry.KeyType == "" {
		entry.KeyType = "ec-256"
	}

	if err := cert.Issue(s.store, entry, cfg.Caddy, true); err != nil {
		entry.LastError = err.Error()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	entry.LastIssuedAt = time.Now().Format(time.RFC3339)
	entry.LastError = ""

	_ = s.store.Update(func(c *config.Config) error {
		c.Caddy.Certificates = append(c.Caddy.Certificates, entry)
		return nil
	})
	// 若已有规则引用该目录，重载 Caddy 让新证书立即生效
	_ = caddy.WriteAndApply(s.store, true)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleCertRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}

	cfg := s.store.Get()
	idx := -1
	for i := range cfg.Caddy.Certificates {
		if cfg.Caddy.Certificates[i].ID == req.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该证书"})
		return
	}
	entry := cfg.Caddy.Certificates[idx]

	if err := cert.Renew(s.store, entry, cfg.Caddy); err != nil {
		_ = s.store.Update(func(c *config.Config) error {
			for i := range c.Caddy.Certificates {
				if c.Caddy.Certificates[i].ID == req.ID {
					c.Caddy.Certificates[i].LastError = err.Error()
				}
			}
			return nil
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	_ = s.store.Update(func(c *config.Config) error {
		for i := range c.Caddy.Certificates {
			if c.Caddy.Certificates[i].ID == req.ID {
				c.Caddy.Certificates[i].LastIssuedAt = time.Now().Format(time.RFC3339)
				c.Caddy.Certificates[i].LastError = ""
			}
		}
		return nil
	})
	_ = caddy.WriteAndApply(s.store, true)
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleCertRenewAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	out, err := cert.RenewAll(s.store)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "output": out})
		return
	}
	if strings.Contains(out, "Cert success") || strings.Contains(out, "renew success") {
		_ = caddy.WriteAndApply(s.store, true)
	}
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func (s *Server) handleCertDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	purge := r.URL.Query().Get("purge") == "1"
	force := r.URL.Query().Get("force") == "1"

	cfg := s.store.Get()
	var entry *config.CertEntry
	for i := range cfg.Caddy.Certificates {
		if cfg.Caddy.Certificates[i].ID == id {
			entry = &cfg.Caddy.Certificates[i]
			break
		}
	}
	if entry == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "未找到该证书"})
		return
	}

	if purge {
		// 被 Caddy 规则引用时不允许直接删除文件
		if !force {
			dir := filepath.Clean(entry.TargetDir) + string(os.PathSeparator)
			if cfg.Web.TLSCertFile != "" && strings.HasPrefix(filepath.Clean(cfg.Web.TLSCertFile), dir) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "面板 HTTPS 正在使用该证书，请先在「设置 → HTTPS 访问」更换证书来源或使用 --force",
				})
				return
			}
			for _, rule := range cfg.Caddy.Rules {
				if rule.CertFile != "" && strings.HasPrefix(filepath.Clean(rule.CertFile), filepath.Clean(entry.TargetDir)+string(os.PathSeparator)) {
					writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": "该证书仍被 Caddy 规则 [" + rule.Listen + "] 引用，请先修改规则或使用 --force",
					})
					return
				}
			}
		}
		if err := cert.PurgeFiles(s.store, *entry); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	_ = cert.Remove(s.store, *entry)
	_ = s.store.Update(func(c *config.Config) error {
		kept := make([]config.CertEntry, 0, len(c.Caddy.Certificates))
		for _, e := range c.Caddy.Certificates {
			if e.ID != id {
				kept = append(kept, e)
			}
		}
		c.Caddy.Certificates = kept
		return nil
	})
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

// ---------------- 备份与迁移 ----------------

func (s *Server) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	opts := backup.Options{
		IncludeSecrets: r.URL.Query().Get("secrets") != "0",
		IncludeLegacy:  r.URL.Query().Get("legacy") == "1",
	}
	path, err := backup.Create(s.store, opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "打包失败: " + err.Error()})
		return
	}
	defer os.Remove(path)

	filename := "autofrp-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	http.ServeFile(w, r, path)
}

const maxBackupUpload = 128 << 20 // 128MB

func (s *Server) handleBackupInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	path, cleanup, err := receiveUpload(r, "file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer cleanup()

	manifest, err := backup.Inspect(path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	path, cleanup, err := receiveUpload(r, "file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer cleanup()

	keepCreds := r.FormValue("keepCreds") == "1"
	cfg, err := backup.Restore(s.store, path, keepCreds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// 重新生成配置并按需重载服务（不触碰 Docker / 系统服务）
	_ = frp.WriteConfigs(s.store)
	_ = caddy.WriteAndApply(s.store, true)
	if systemd.QueryStatus("frps").Running {
		_ = systemd.Control("frps", "restart", systemd.FindBinary("frps"), []string{"-c", s.store.FrpsConfPath()}, s.store.StateDir(), nil)
	}
	if systemd.QueryStatus("frpc").Running {
		_ = frp.ApplyFrpc(s.store, true)
	}

	// 恢复后凭据可能变化，重新下发当前会话 Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    computeSessionToken(cfg.Web.Username, cfg.Web.Password),
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400 * 30,
	})
	writeJSON(w, http.StatusOK, s.buildOverviewResponse())
}

func receiveUpload(r *http.Request, field string) (string, func(), error) {
	if err := r.ParseMultipartForm(maxBackupUpload); err != nil {
		return "", func() {}, errors.New("上传解析失败（文件需小于 128MB）")
	}
	file, _, err := r.FormFile(field)
	if err != nil {
		return "", func() {}, errors.New("未收到上传文件")
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "autof-upload-*.tar.gz")
	if err != nil {
		return "", func() {}, err
	}
	path := tmp.Name()
	cleanup := func() { os.Remove(path) }

	n, err := io.Copy(tmp, io.LimitReader(file, maxBackupUpload))
	tmp.Close()
	if err != nil || n == 0 {
		cleanup()
		return "", func() {}, errors.New("文件写入失败")
	}
	return path, cleanup, nil
}

// renewLoop 定时续期 acme.sh 证书（每 12 小时检查一次）
func (s *Server) renewLoop() {
	time.Sleep(3 * time.Minute)
	s.renewCertificates()
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.renewCertificates()
	}
}

func (s *Server) renewCertificates() {
	cfg := s.store.Get()
	hasAuto := false
	for _, e := range cfg.Caddy.Certificates {
		if e.AutoRenew {
			hasAuto = true
			break
		}
	}
	if !hasAuto {
		return
	}
	start := time.Now()
	if _, err := cert.RenewAll(s.store); err != nil {
		_ = s.store.Update(func(c *config.Config) error {
			for i := range c.Caddy.Certificates {
				if c.Caddy.Certificates[i].AutoRenew {
					c.Caddy.Certificates[i].LastError = err.Error()
				}
			}
			return nil
		})
		return
	}

	renewed := false
	_ = s.store.Update(func(c *config.Config) error {
		for i := range c.Caddy.Certificates {
			e := &c.Caddy.Certificates[i]
			if !e.AutoRenew {
				continue
			}
			certPath := filepath.Join(e.TargetDir, "fullchain.pem")
			if st, err := os.Stat(certPath); err == nil && st.ModTime().After(start.Add(-time.Minute)) {
				e.LastIssuedAt = time.Now().Format(time.RFC3339)
				e.LastError = ""
				renewed = true
			}
		}
		return nil
	})

	// 有规则使用自定义证书文件时，续期后重载 Caddy 使之生效
	if renewed {
		for _, rule := range cfg.Caddy.Rules {
			if rule.TLSMode == "custom" && rule.CertFile != "" {
				_ = caddy.WriteAndApply(s.store, true)
				return
			}
		}
	}
}
