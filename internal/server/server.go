package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
	"github.com/WuCaiCaiCai/Autofrp/internal/web"
)

const sessionCookieName = "autof_session"

// Server 封装 Autofrp 内置 WebUI 与 REST API 服务
type Server struct {
	store *config.Store
	mux   *http.ServeMux
}

// New 创建 WebUI HTTP Server
func New(store *config.Store) *Server {
	s := &Server{
		store: store,
		mux:   http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

// Start 启动 HTTP 监听
func (s *Server) Start(listenAddr string) error {
	if listenAddr == "" {
		listenAddr = s.store.Get().Web.ListenAddr
	}
	return http.ListenAndServe(listenAddr, s.mux)
}

func (s *Server) registerRoutes() {
	subFS, _ := fs.Sub(web.StaticFS, "static")
	fileServer := http.FileServer(http.FS(subFS))

	s.mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := web.StaticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "加载前端页面失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/overview", s.withAuth(s.handleOverview))
	s.mux.HandleFunc("/api/net/refresh", s.withAuth(s.handleNetRefresh))
	s.mux.HandleFunc("/api/net/pref", s.withAuth(s.handleNetPref))
	s.mux.HandleFunc("/api/system/init", s.withAuth(s.handleSystemInit))
	s.mux.HandleFunc("/api/frpc/parse", s.withAuth(s.handleFrpcParse))
	s.mux.HandleFunc("/api/service/action", s.withAuth(s.handleServiceAction))
	s.mux.HandleFunc("/api/service/logs", s.withAuth(s.handleServiceLogs))

	// frps (远程 VPS 服务端)
	s.mux.HandleFunc("/api/frps/settings", s.withAuth(s.handleFrpsSettings))
	s.mux.HandleFunc("/api/frps/service", s.withAuth(s.handleFrpsService))

	// frpc (本地服务器客户端)
	s.mux.HandleFunc("/api/frpc/settings", s.withAuth(s.handleFrpcSettings))
	s.mux.HandleFunc("/api/frpc/import", s.withAuth(s.handleFrpcImport))
	s.mux.HandleFunc("/api/frpc/tunnel", s.withAuth(s.handleFrpcTunnel))

	// Caddy 反向代理
	s.mux.HandleFunc("/api/caddy/settings", s.withAuth(s.handleCaddySettings))
	s.mux.HandleFunc("/api/caddy/rule", s.withAuth(s.handleCaddyRule))

	// 原服务探测、在线编辑与一键吞并迁移 (NPM / Nginx / 原版 Caddy / 原版 frp)
	s.mux.HandleFunc("/api/scanner/file", s.withAuth(s.handleScannerFile))
	s.mux.HandleFunc("/api/scanner/import", s.withAuth(s.handleScannerImport))
	s.mux.HandleFunc("/api/scanner/takeover-preview", s.withAuth(s.handleTakeoverPreview))
	s.mux.HandleFunc("/api/scanner/takeover", s.withAuth(s.handleTakeoverExecute))

	// 系统与 WebUI 设置
	s.mux.HandleFunc("/api/system/web", s.withAuth(s.handleSystemWeb))
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.store.Get()
		if !cfg.Web.AuthEnabled {
			next(w, r)
			return
		}
		expectedToken := computeSessionToken(cfg.Web.Username, cfg.Web.Password)

		// 1. 检查 Cookie
		if c, err := r.Cookie(sessionCookieName); err == nil {
			if subtle.ConstantTimeCompare([]byte(c.Value), []byte(expectedToken)) == 1 {
				next(w, r)
				return
			}
		}

		// 2. 检查 HTTP Basic Auth
		if u, p, ok := r.BasicAuth(); ok {
			uMatch := subtle.ConstantTimeCompare([]byte(u), []byte(cfg.Web.Username)) == 1
			pMatch := subtle.ConstantTimeCompare([]byte(p), []byte(cfg.Web.Password)) == 1
			if uMatch && pMatch {
				next(w, r)
				return
			}
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未授权访问，请先登录"})
	}
}

func computeSessionToken(username, password string) string {
	mac := hmac.New(sha256.New, []byte("autof-session-key:"+password))
	mac.Write([]byte(strings.TrimSpace(username)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	cfg := s.store.Get()
	uMatch := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Username)), []byte(cfg.Web.Username)) == 1
	pMatch := subtle.ConstantTimeCompare([]byte(req.Password), []byte(cfg.Web.Password)) == 1
	if !uMatch || !pMatch {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    computeSessionToken(cfg.Web.Username, cfg.Web.Password),
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400 * 30,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
