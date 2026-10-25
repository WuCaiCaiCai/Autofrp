package server

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestWebServerAPIWorkflow(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. 测试首页返回 HTML
	resp, err := http.Get(ts.URL + "/")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /, got %v %v", resp, err)
	}
	resp.Body.Close()

	// 2. 未登录访问 /api/overview 应返回 401
	resp, err = http.Get(ts.URL + "/api/overview")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %v", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. 使用 Basic Auth 添加一条带 Caddy 联动的穿透服务
	cfg := store.Get()
	tunPayload, _ := json.Marshal(map[string]any{
		"name":         "nas-web",
		"type":         "tcp",
		"localIP":      "192.168.1.50",
		"localPort":    5666,
		"remotePort":   15666,
		"caddyDomain":  "nas.example.com",
		"caddyTlsMode": "auto",
		"enabled":      true,
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/frps/service", bytes.NewReader(tunPayload))
	req.SetBasicAuth(cfg.Web.Username, cfg.Web.Password)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/frps/service failed: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	updatedCfg := store.Get()
	if len(updatedCfg.Frps.Services) != 1 || len(updatedCfg.Caddy.Rules) != 1 {
		t.Fatalf("expected 1 frps service and 1 linked caddy rule, got %d / %d", len(updatedCfg.Frps.Services), len(updatedCfg.Caddy.Rules))
	}

	// 4. 测试导出连接码并导入到 frpc
	_ = store.Update(func(c *config.Config) error {
		c.Network.PublicIPv4 = "108.165.122.146"
		c.Network.IPPref = "4"
		return nil
	})
	updatedCfg = store.Get()
	code, err := updatedCfg.ExportConnectionCode()
	if err != nil {
		t.Fatal(err)
	}
	impPayload, _ := json.Marshal(map[string]any{
		"input": code,
		"merge": false,
	})
	reqImp, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/frpc/import", bytes.NewReader(impPayload))
	reqImp.SetBasicAuth(cfg.Web.Username, cfg.Web.Password)
	respImp, err := http.DefaultClient.Do(reqImp)
	if err != nil || respImp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/frpc/import failed: %v %d", err, respImp.StatusCode)
	}
	respImp.Body.Close()

	afterImp := store.Get()
	if len(afterImp.Frpc.Tunnels) != 1 || afterImp.Frpc.Tunnels[0].LocalPort != 5666 {
		t.Fatalf("unexpected imported tunnels: %+v", afterImp.Frpc.Tunnels)
	}

	// 5. 初始化向导写入角色 / 端口 / Token，并标记已完成初始化
	initBody, _ := json.Marshal(map[string]any{
		"nodeRole":    "vps",
		"ipPref":      "both",
		"controlPort": 30085,
		"token":       "initToken123",
	})
	reqInit, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/system/init", bytes.NewReader(initBody))
	reqInit.SetBasicAuth(cfg.Web.Username, cfg.Web.Password)
	respInit, err := http.DefaultClient.Do(reqInit)
	if err != nil || respInit.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/system/init failed: %v %d", err, respInit.StatusCode)
	}
	respInit.Body.Close()

	afterInit := store.Get()
	if afterInit.NodeRole != "vps" || afterInit.Frps.ControlPort != 30085 || !afterInit.SetupCompleted {
		t.Fatalf("unexpected init result: role=%s port=%d setup=%v", afterInit.NodeRole, afterInit.Frps.ControlPort, afterInit.SetupCompleted)
	}

	// 6. 导入前解析连接码（不落库）
	parseBody, _ := json.Marshal(map[string]any{"input": code})
	reqParse, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/frpc/parse", bytes.NewReader(parseBody))
	reqParse.SetBasicAuth(cfg.Web.Username, cfg.Web.Password)
	respParse, err := http.DefaultClient.Do(reqParse)
	if err != nil || respParse.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/frpc/parse failed: %v %d", err, respParse.StatusCode)
	}
	var parsed map[string]any
	_ = json.NewDecoder(respParse.Body).Decode(&parsed)
	respParse.Body.Close()
	if parsed["serverAddr"] != "108.165.122.146" || parsed["tunnelCount"].(float64) != 1 {
		t.Fatalf("unexpected parse result: %+v", parsed)
	}
}

func TestLegacyConfigInheritance(t *testing.T) {
	dir := t.TempDir()
	// 模拟 v1.1.x 的旧配置残留：state.conf + services.conf，且没有 config.json
	stateConf := "CONTROL_PORT=30085\nTOKEN=legacyToken\nPUBLIC_IP=203.0.113.7\nIP_PREF=6\n"
	servicesConf := "mc-java|tcp|25565|30008|MC\nnas-web|tcp|5666|15666|NAS\n"
	if err := os.WriteFile(filepath.Join(dir, "state.conf"), []byte(stateConf), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services.conf"), []byte(servicesConf), 0600); err != nil {
		t.Fatal(err)
	}

	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	if !cfg.SetupCompleted {
		t.Fatal("旧版迁移后应自动视为已完成初始化，避免重复弹出向导")
	}
	if cfg.Frps.ControlPort != 30085 || cfg.Frps.Token != "legacyToken" {
		t.Fatalf("旧版参数未继承: %+v", cfg.Frps)
	}
	if len(cfg.Frps.Services) != 2 {
		t.Fatalf("旧版服务数量应为 2，实际 %d", len(cfg.Frps.Services))
	}
	legacy := store.LegacyStatus()
	if !legacy.Detected || !legacy.Migrated || legacy.ServiceCount != 2 || legacy.LegacyPort != 30085 {
		t.Fatalf("旧版状态识别异常: %+v", legacy)
	}

	// 已存在 config.json 但缺少 setupCompleted 字段（v1.0.0 早期版本），也应视为已完成初始化
	otherDir := t.TempDir()
	store2, err := config.NewStore(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	raw := store2.Get()
	raw.SetupCompleted = false
	plain, _ := json.Marshal(raw)
	var m map[string]any
	_ = json.Unmarshal(plain, &m)
	delete(m, "setupCompleted")
	reduced, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(otherDir, "config.json"), reduced, 0600); err != nil {
		t.Fatal(err)
	}
	store3, err := config.NewStore(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	if !store3.Get().SetupCompleted {
		t.Fatal("缺少 setupCompleted 字段的旧 config.json 应视为已完成初始化")
	}
}

func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls", "panel.crt")
	keyPath := filepath.Join(dir, "tls", "panel.key")

	if err := GenerateSelfSignedCert(certPath, keyPath, []string{"localhost", "127.0.0.1", "203.0.113.9", "panel.example.com"}); err != nil {
		t.Fatalf("GenerateSelfSignedCert error: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("私钥未生成: %v", err)
	}
	pemData, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Fatal("证书 PEM 解析失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) < 2 || len(cert.DNSNames) < 2 {
		t.Fatalf("SAN 不完整: ips=%v dns=%v", cert.IPAddresses, cert.DNSNames)
	}
	if cert.NotAfter.Before(cert.NotBefore.Add(700 * 24 * time.Hour)) {
		t.Fatalf("证书有效期异常: %v ~ %v", cert.NotBefore, cert.NotAfter)
	}
}
