package netenv

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

var (
	ipv4Endpoints = []string{
		"https://ipv4.icanhazip.com",
		"https://api.ipify.org",
		"https://ipv4.ident.me",
	}
	ipv6Endpoints = []string{
		"https://ipv6.icanhazip.com",
		"https://api6.ipify.org",
		"https://ipv6.ident.me",
	}
)

// DetectAndApply 并发探测本机网卡地址与公网 IPv4/IPv6，并更新 NetConfig
func DetectAndApply(netCfg *config.NetConfig) {
	localV4, localV6 := localGlobalIPs()

	var pubV4, pubV6 string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pubV4 = fetchPublicIP("tcp4", ipv4Endpoints)
	}()
	go func() {
		defer wg.Done()
		pubV6 = fetchPublicIP("tcp6", ipv6Endpoints)
	}()
	wg.Wait()

	hasPublicLocalV4 := false
	firstPrivateV4 := ""
	for _, ipStr := range localV4 {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isPrivateOrCGNAT(ip) {
			if firstPrivateV4 == "" {
				firstPrivateV4 = ipStr
			}
		} else {
			hasPublicLocalV4 = true
			if pubV4 == "" {
				pubV4 = ipStr
			}
		}
	}

	if firstPrivateV4 != "" {
		netCfg.LocalIPv4 = firstPrivateV4
	} else if len(localV4) > 0 {
		netCfg.LocalIPv4 = localV4[0]
	}

	if pubV6 == "" && len(localV6) > 0 {
		pubV6 = localV6[0]
	}

	if pubV4 != "" {
		netCfg.PublicIPv4 = pubV4
	}
	if pubV6 != "" {
		netCfg.PublicIPv6 = pubV6
	}

	if hasPublicLocalV4 {
		netCfg.V4Status = "公网 IPv4"
	} else if netCfg.PublicIPv4 != "" {
		netCfg.V4Status = "NAT IPv4"
	} else {
		netCfg.V4Status = "无 IPv4"
	}

	if len(localV6) > 0 {
		netCfg.V6Status = "公网 IPv6"
	} else if netCfg.PublicIPv6 != "" {
		netCfg.V6Status = "NAT IPv6"
	} else {
		netCfg.V6Status = "无 IPv6"
	}

	if netCfg.IPPref == "" {
		if netCfg.PublicIPv4 != "" {
			netCfg.IPPref = "4"
		} else if netCfg.PublicIPv6 != "" {
			netCfg.IPPref = "6"
		} else {
			netCfg.IPPref = "both"
		}
	}
}

func fetchPublicIP(network string, urls []string) string {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   4 * time.Second,
	}

	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		ipStr := strings.TrimSpace(string(b))
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func localGlobalIPs() (v4 []string, v6 []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				v4 = append(v4, ip4.String())
			} else if ip.To16() != nil && !ip.IsPrivate() {
				v6 = append(v6, ip.String())
			}
		}
	}
	return v4, v6
}

func isPrivateOrCGNAT(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	// RFC 6598 CGNAT 100.64.0.0/10
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return false
}
