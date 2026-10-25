package frp

import (
	"strings"
	"testing"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

func TestGenerateFrpConfigs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Network.IPPref = "both"
	cfg.Network.PublicIPv4 = "198.51.100.10"
	cfg.Network.PublicIPv6 = "2001:db8::1"
	cfg.Frps.ControlPort = 30085
	cfg.Frps.Token = "testToken123"
	cfg.Frps.AllowPortsMode = "range"
	cfg.Frps.AllowPortsRange = "30000-30050"
	cfg.Frps.Services = []config.TunnelRule{
		{
			ID:         "tun_1",
			Name:       "mc-java",
			Type:       "tcp",
			LocalIP:    "127.0.0.1",
			LocalPort:  25565,
			RemotePort: 30008,
			Enabled:    true,
		},
	}

	frpsToml := GenerateFrpsToml(*cfg)
	if !strings.Contains(frpsToml, `bindAddr = "::"`) {
		t.Fatalf("expected dual-stack bindAddr in frps.toml:\n%s", frpsToml)
	}
	if !strings.Contains(frpsToml, `{ start = 30000, end = 30050 }`) || !strings.Contains(frpsToml, `{ single = 30008 }`) {
		t.Fatalf("expected allowPorts range and single port in frps.toml:\n%s", frpsToml)
	}

	frpcExport := GenerateFrpcTomlForExport(*cfg, -1)
	if !strings.Contains(frpcExport, `serverAddr = "198.51.100.10"`) || !strings.Contains(frpcExport, `remotePort = 30008`) {
		t.Fatalf("unexpected frpc export:\n%s", frpcExport)
	}

	cfg.Frpc.ServerAddr = "198.51.100.10"
	cfg.Frpc.ServerPort = 30085
	cfg.Frpc.Token = "testToken123"
	cfg.Frpc.Tunnels = cfg.Frps.Services
	frpcClient := GenerateFrpcTomlForClient(*cfg)
	if !strings.Contains(frpcClient, `webServer.port = 7400`) {
		t.Fatalf("expected webServer.port in client frpc.toml:\n%s", frpcClient)
	}
}
