package config

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const ConnCodePrefix = "autof://"

// BuildConnectionBundle 从当前 VPS 配置构造客户端连接包
func (c Config) BuildConnectionBundle() ConnectionBundle {
	addr := c.PublicAddr()
	if addr == "" {
		addr = c.Network.LocalIPv4
	}
	tunnels := make([]TunnelRule, 0, len(c.Frps.Services))
	for _, s := range c.Frps.Services {
		if !s.Enabled {
			continue
		}
		tunnels = append(tunnels, TunnelRule{
			ID:         s.ID,
			Name:       s.Name,
			Type:       s.Type,
			LocalIP:    s.LocalIP,
			LocalPort:  s.LocalPort,
			RemotePort: s.RemotePort,
			Enabled:    true,
			Remark:     s.Remark,
		})
	}
	return ConnectionBundle{
		ServerAddr:   addr,
		ServerPort:   c.Frps.ControlPort,
		ServerAltIP6: c.PublicAddrAlt(),
		Token:        c.Frps.Token,
		TLSEnable:    true,
		Tunnels:      tunnels,
	}
}

// ExportConnectionCode 将连接包编码为 autof:// 连接码
func (c Config) ExportConnectionCode() (string, error) {
	bundle := c.BuildConnectionBundle()
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return ConnCodePrefix + encoded, nil
}

// ParseImportInput 智能解析用户输入：支持 autof:// 连接码、JSON、以及标准 frpc.toml 文本
func ParseImportInput(input string) (*ConnectionBundle, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return nil, errors.New("导入内容不能为空")
	}

	// 1. autof:// 连接码
	if strings.HasPrefix(raw, ConnCodePrefix) {
		b64 := strings.TrimSpace(strings.TrimPrefix(raw, ConnCodePrefix))
		decoded, err := base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return nil, fmt.Errorf("连接码 Base64 解码失败: %w", err)
			}
		}
		var bundle ConnectionBundle
		if err := json.Unmarshal(decoded, &bundle); err != nil {
			return nil, fmt.Errorf("连接码 JSON 解析失败: %w", err)
		}
		return &bundle, nil
	}

	// 2. 直接是 JSON
	if strings.HasPrefix(raw, "{") {
		var bundle ConnectionBundle
		if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
			return nil, fmt.Errorf("JSON 解析失败: %w", err)
		}
		return &bundle, nil
	}

	// 3. 解析 frpc.toml 配置文件
	return parseFrpcToml(raw)
}

func parseFrpcToml(tomlText string) (*ConnectionBundle, error) {
	bundle := &ConnectionBundle{
		ServerPort: 7000,
		TLSEnable:  true,
		Tunnels:    []TunnelRule{},
	}

	var curProxy *TunnelRule
	flushProxy := func() {
		if curProxy != nil && curProxy.Name != "" && curProxy.LocalPort > 0 {
			if curProxy.LocalIP == "" {
				curProxy.LocalIP = "127.0.0.1"
			}
			if curProxy.Type == "" {
				curProxy.Type = "tcp"
			}
			if curProxy.RemotePort <= 0 {
				curProxy.RemotePort = curProxy.LocalPort
			}
			curProxy.ID = GenID("tun")
			curProxy.Enabled = true
			bundle.Tunnels = append(bundle.Tunnels, *curProxy)
		}
		curProxy = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(tomlText))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去掉行尾注释
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "[[proxies]]" {
			flushProxy()
			curProxy = &TunnelRule{LocalIP: "127.0.0.1", Type: "tcp", Enabled: true}
			continue
		}
		if strings.HasPrefix(line, "[") {
			flushProxy()
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		if curProxy == nil {
			switch k {
			case "serverAddr", "server_addr":
				bundle.ServerAddr = v
			case "serverPort", "server_port":
				if p, err := strconv.Atoi(v); err == nil {
					bundle.ServerPort = p
				}
			case "auth.token", "token":
				bundle.Token = v
			case "transport.tls.enable", "tls_enable":
				bundle.TLSEnable = (v == "true" || v == "1")
			case "bindPort", "allowPorts":
				return nil, errors.New("检测到这是服务端 frps.toml 配置，请导入客户端 frpc.toml 或连接码")
			}
		} else {
			switch k {
			case "name":
				curProxy.Name = v
			case "type":
				curProxy.Type = strings.ToLower(v)
			case "localIP", "local_ip":
				curProxy.LocalIP = v
			case "localPort", "local_port":
				curProxy.LocalPort, _ = strconv.Atoi(v)
			case "remotePort", "remote_port":
				curProxy.RemotePort, _ = strconv.Atoi(v)
			}
		}
	}
	flushProxy()

	if bundle.ServerAddr == "" && len(bundle.Tunnels) == 0 {
		return nil, errors.New("未能从输入内容中识别出有效的服务器地址或穿透规则")
	}
	return bundle, nil
}
