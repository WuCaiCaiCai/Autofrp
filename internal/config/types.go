package config

const (
	AppName    = "autof"
	AppVersion = "1.0.1"
)

// Config 保存 Autofrp 的完整配置与状态（远程 VPS 与本地服务器通用）
type Config struct {
	Version        string      `json:"version"`
	NodeRole       string      `json:"nodeRole"`       // "vps", "local", "both"
	SetupCompleted bool        `json:"setupCompleted"` // 是否已完成初始化向导（旧版迁移用户自动视为已完成）
	Network        NetConfig   `json:"network"`
	Web            WebConfig   `json:"web"`
	Frps           FrpsConfig  `json:"frps"`
	Frpc           FrpcConfig  `json:"frpc"`
	Caddy          CaddyConfig `json:"caddy"`
}

// NetConfig 保存网络探测状态与双栈偏好
type NetConfig struct {
	IPPref     string `json:"ipPref"`     // "4", "6", "both"
	PublicIPv4 string `json:"publicIPv4"` // 公网 IPv4
	PublicIPv6 string `json:"publicIPv6"` // 公网 IPv6
	LocalIPv4  string `json:"localIPv4"`  // 本机内网/网卡 IPv4
	V4Status   string `json:"v4Status"`   // "公网 IPv4", "NAT IPv4", "无 IPv4"
	V6Status   string `json:"v6Status"`   // "公网 IPv6", "NAT IPv6", "无 IPv6"
}

// WebConfig 保存内置 WebUI 面板设置
type WebConfig struct {
	ListenAddr  string `json:"listenAddr"`  // 默认 "0.0.0.0:7500"
	Username    string `json:"username"`    // 默认 "admin"
	Password    string `json:"password"`    // 登录密码
	AuthEnabled bool   `json:"authEnabled"` // 是否启用登录鉴权
}

// FrpsConfig 保存远程 VPS 端的 frps 服务端设置
type FrpsConfig struct {
	ControlPort     int          `json:"controlPort"`     // 控制端口，默认 7000
	Token           string       `json:"token"`           // 鉴权 Token
	AllowPortsMode  string       `json:"allowPortsMode"`  // "unlimited" (不限制), "range" (端口段), "strict" (仅列表内端口)
	AllowPortsRange string       `json:"allowPortsRange"` // 如 "30000-30100"
	InstalledVer    string       `json:"installedVer"`    // 已安装的 frp 版本
	Services        []TunnelRule `json:"services"`        // VPS 端登记的穿透服务（用于生成客户端配置、strict 白名单及联动 Caddy）
}

// FrpcConfig 保存本地服务器端的 frpc 客户端设置
type FrpcConfig struct {
	ServerAddr string       `json:"serverAddr"` // 远程 VPS 地址
	ServerPort int          `json:"serverPort"` // 远程 VPS 控制端口
	Token      string       `json:"token"`      // 远程 VPS Token
	TLSEnable  bool         `json:"tlsEnable"`  // 默认启用 TLS
	AdminPort  int          `json:"adminPort"`  // 本地热重载管理端口，默认 7400 (127.0.0.1)
	Tunnels    []TunnelRule `json:"tunnels"`    // 本地客户端实际开启的穿透隧道
}

// TunnelRule 表示一条内网穿透规则（VPS 端预设与本地端隧道共用结构）
type TunnelRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`          // 隧道名称，如 mc-java, web-nas
	Type          string `json:"type"`          // "tcp" 或 "udp"
	LocalIP       string `json:"localIP"`       // 内网目标地址，默认 "127.0.0.1"，可填局域网 IP 如 "192.168.1.50"
	LocalPort     int    `json:"localPort"`     // 内网服务实际端口
	RemotePort    int    `json:"remotePort"`    // 远程 VPS 暴露/监听端口
	BindLocalOnly bool   `json:"bindLocalOnly"` // 仅在 VPS 本地监听 (配合 Caddy 反代时不对外直接暴露明文端口)
	CaddyDomain   string `json:"caddyDomain"`   // 联动 Caddy 反代入口（如 "nas.example.com" 或 "nas.example.com:8443"）
	CaddyTLSMode  string `json:"caddyTlsMode"`  // 联动 Caddy 的证书模式: "auto", "dns", "internal", "http"
	Enabled       bool   `json:"enabled"`       // 是否启用
	Remark        string `json:"remark"`        // 备注
}

// CaddyConfig 保存 Caddy 反向代理全局配置与规则列表
type CaddyConfig struct {
	HTTPPort     int         `json:"httpPort"`     // 默认 80
	HTTPSPort    int         `json:"httpsPort"`    // 默认 443
	Email        string      `json:"email"`        // ACME 证书邮箱（可选）
	DNSProvider  string      `json:"dnsProvider"`  // "" | "cloudflare" | "alidns" | "dnspod" | "duckdns"
	DNSToken     string      `json:"dnsToken"`     // DNS API Token / AccessKeyId
	DNSSecret    string      `json:"dnsSecret"`    // DNS Secret（如阿里云 AccessKeySecret）
	CustomGlobal string      `json:"customGlobal"` // 自定义全局选项片段
	Rules        []CaddyRule `json:"rules"`        // 反代规则列表
}

// CaddyRule 表示一条 Caddy 反向代理规则
type CaddyRule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`            // 规则名称/备注
	Listen          string `json:"listen"`          // 前端监听：域名(example.com)、域名:端口(example.com:8443)、纯端口(:8080)、局域网IP:端口
	Upstream        string `json:"upstream"`        // 后端目标：127.0.0.1:8080、192.168.1.50:5000、https://127.0.0.1:8443
	TLSMode         string `json:"tlsMode"`         // "auto" (自动公网证书), "dns" (DNS API 证书), "internal" (局域网自签证书), "custom" (已迁移的原证书), "http" (纯 HTTP)
	CertFile        string `json:"certFile"`        // 迁移或自定义的公钥证书路径 (/etc/autof/certs/.../fullchain.pem)
	KeyFile         string `json:"keyFile"`         // 迁移或自定义的私钥路径 (/etc/autof/certs/.../privkey.pem)
	SkipTLSVerify   bool   `json:"skipTlsVerify"`   // 后端为 HTTPS 时跳过自签证书校验
	EnableEncode    bool   `json:"enableEncode"`    // 开启 zstd/gzip 压缩
	ExtraDirectives string `json:"extraDirectives"` // 额外自定义 Caddyfile 指令
	LinkedTunnelID  string `json:"linkedTunnelId"`  // 关联的穿透规则 ID（由穿透服务自动联动创建时非空）
	Enabled         bool   `json:"enabled"`         // 是否启用
	Remark          string `json:"remark"`          // 备注
}

// ConnectionBundle 是远程 VPS 导出给本地服务器的一键连接数据包（支持编码为 autof://...）
type ConnectionBundle struct {
	ServerAddr   string       `json:"serverAddr"`
	ServerPort   int          `json:"serverPort"`
	ServerAltIP6 string       `json:"serverAltIp6,omitempty"`
	Token        string       `json:"token"`
	TLSEnable    bool         `json:"tlsEnable"`
	Tunnels      []TunnelRule `json:"tunnels,omitempty"`
}
