package frp

// Preset 定义常用的内网穿透服务预设模板
type Preset struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	DefaultName  string `json:"defaultName"`
	Type         string `json:"type"`
	LocalPort    int    `json:"localPort"`
	RemotePort   int    `json:"remotePort"`
	SuggestCaddy bool   `json:"suggestCaddy"`
	Description  string `json:"description"`
}

// BuiltinPresets 返回内置的快捷穿透预设列表
func BuiltinPresets() []Preset {
	return []Preset{
		{
			Key:         "mc-java",
			Label:       "Minecraft Java 版",
			DefaultName: "mc-java",
			Type:        "tcp",
			LocalPort:   25565,
			RemotePort:  25565,
			Description: "Java 版联机服务端（默认 TCP 25565）",
		},
		{
			Key:         "mc-bedrock",
			Label:       "Minecraft 基岩版 (Bedrock)",
			DefaultName: "mc-bedrock",
			Type:        "udp",
			LocalPort:   19132,
			RemotePort:  19132,
			Description: "基岩版 / 手机 / 主机联机服务端（默认 UDP 19132）",
		},
		{
			Key:          "web-http",
			Label:        "Web 网站 / 面板服务",
			DefaultName:  "web-app",
			Type:         "tcp",
			LocalPort:    8080,
			RemotePort:   18080,
			SuggestCaddy: true,
			Description:  "内网 Web 服务，推荐开启 Caddy 域名反代自动启用 HTTPS",
		},
		{
			Key:          "nas",
			Label:        "NAS 存储 (飞牛 / 群晖)",
			DefaultName:  "nas-web",
			Type:         "tcp",
			LocalPort:    5666,
			RemotePort:   15666,
			SuggestCaddy: true,
			Description:  "内网 NAS 管理界面穿透，可联动 Caddy 绑定域名访问",
		},
		{
			Key:         "ssh",
			Label:       "SSH 远程终端",
			DefaultName: "ssh-server",
			Type:        "tcp",
			LocalPort:   22,
			RemotePort:  2222,
			Description: "通过公网端口 SSH 登录内网 Linux 服务器",
		},
		{
			Key:         "rdp",
			Label:       "Windows 远程桌面 (RDP)",
			DefaultName: "win-rdp",
			Type:        "tcp",
			LocalPort:   3389,
			RemotePort:  13389,
			Description: "远程连接内网 Windows 电脑桌面（TCP 3389）",
		},
		{
			Key:         "custom-tcp",
			Label:       "自定义 TCP 服务",
			DefaultName: "tcp-svc",
			Type:        "tcp",
			LocalPort:   8000,
			RemotePort:  8000,
			Description: "任意基于 TCP 协议的内网服务",
		},
		{
			Key:         "custom-udp",
			Label:       "自定义 UDP 服务",
			DefaultName: "udp-svc",
			Type:        "udp",
			LocalPort:   9000,
			RemotePort:  9000,
			Description: "任意基于 UDP 协议的游戏或语音服务",
		},
	}
}
