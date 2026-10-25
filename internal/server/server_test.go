package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
}
