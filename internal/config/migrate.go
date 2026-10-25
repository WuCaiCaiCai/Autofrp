package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// migrateLegacyV1 从旧版 v1 的 state.conf 和 services.conf 自动迁移配置
func migrateLegacyV1(stateDir string, cfg *Config) bool {
	stateFile := filepath.Join(stateDir, "state.conf")
	svcFile := filepath.Join(stateDir, "services.conf")

	migrated := false

	if data, err := os.ReadFile(stateFile); err == nil && len(data) > 0 {
		migrated = true
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := unquoteBash(strings.TrimSpace(parts[1]))
			if val == "" {
				continue
			}
			switch key {
			case "CONTROL_PORT", "CONTROL_OUT", "CONTROL_IN", "BIND_PORT":
				if p, err := strconv.Atoi(val); err == nil && p > 0 && p <= 65535 {
					cfg.Frps.ControlPort = p
				}
			case "TOKEN":
				cfg.Frps.Token = val
			case "FRP_VERSION":
				cfg.Frps.InstalledVer = val
			case "PUBLIC_IP":
				cfg.Network.PublicIPv4 = val
			case "PUBLIC_IP6":
				cfg.Network.PublicIPv6 = val
			case "V4_STATUS":
				cfg.Network.V4Status = val
			case "V6_STATUS":
				cfg.Network.V6Status = val
			case "IP_PREF":
				if val == "4" || val == "6" || val == "both" {
					cfg.Network.IPPref = val
				}
			}
		}
	}

	if data, err := os.ReadFile(svcFile); err == nil && len(data) > 0 {
		migrated = true
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "|")
			if len(fields) < 4 {
				continue
			}
			name := strings.TrimSpace(fields[0])
			typ := strings.ToLower(strings.TrimSpace(fields[1]))
			if typ != "tcp" && typ != "udp" {
				continue
			}
			var lport, rport int
			var remark string
			if len(fields) <= 5 {
				// v1.1 格式: name|type|lport|public|remark
				lport, _ = strconv.Atoi(strings.TrimSpace(fields[2]))
				rport, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
				if len(fields) == 5 {
					remark = strings.TrimSpace(fields[4])
				}
			} else {
				// 更早期格式兼容
				lport, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
				rport, _ = strconv.Atoi(strings.TrimSpace(fields[4]))
				remark = strings.TrimSpace(fields[len(fields)-1])
			}
			if lport <= 0 || rport <= 0 {
				continue
			}
			cfg.Frps.Services = append(cfg.Frps.Services, TunnelRule{
				ID:         GenID("tun"),
				Name:       name,
				Type:       typ,
				LocalIP:    "127.0.0.1",
				LocalPort:  lport,
				RemotePort: rport,
				Enabled:    true,
				Remark:     remark,
			})
		}
	}

	return migrated
}

func unquoteBash(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "$'") && strings.HasSuffix(s, "'") {
		s = s[2 : len(s)-1]
	} else if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}
