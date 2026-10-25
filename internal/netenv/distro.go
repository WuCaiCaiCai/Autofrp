package netenv

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// SystemInfo 描述当前 Linux 发行版与运行环境信息
type SystemInfo struct {
	DistroID     string `json:"distroId"`     // debian, ubuntu, deepin, fedora, arch, alpine 等
	DistroLike   string `json:"distroLike"`   // ID_LIKE
	PrettyName   string `json:"prettyName"`   // 如 "Debian GNU/Linux 12 (bookworm)", "Deepin 23", "Fedora Linux 41", "Arch Linux"
	PkgManager   string `json:"pkgManager"`   // apt, dnf, yum, pacman, zypper, apk, opkg
	InitSystem   string `json:"initSystem"`   // systemd, openrc, daemon
	Arch         string `json:"arch"`         // amd64, arm64, arm, 386, riscv64, loong64
}

// DetectSystemInfo 识别当前 Linux 发行版（适配 Debian / Deepin / Fedora / Arch / Alpine 等）
func DetectSystemInfo() SystemInfo {
	info := SystemInfo{
		DistroID:   "linux",
		PrettyName: "Linux",
		Arch:       runtime.GOARCH,
	}

	if f, err := os.Open("/etc/os-release"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			switch k {
			case "ID":
				info.DistroID = strings.ToLower(v)
			case "ID_LIKE":
				info.DistroLike = strings.ToLower(v)
			case "PRETTY_NAME":
				if v != "" {
					info.PrettyName = v
				}
			}
		}
	}

	info.PkgManager = detectPkgManager(info.DistroID, info.DistroLike)
	info.InitSystem = detectInitSystem()
	return info
}

func detectPkgManager(id, like string) string {
	combined := id + " " + like
	switch {
	case strings.Contains(combined, "debian") || strings.Contains(combined, "ubuntu") ||
		strings.Contains(combined, "deepin") || strings.Contains(combined, "uos") ||
		strings.Contains(combined, "kali") || strings.Contains(combined, "mint"):
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "apt"
		}
	case strings.Contains(combined, "fedora") || strings.Contains(combined, "rhel") ||
		strings.Contains(combined, "centos") || strings.Contains(combined, "rocky") ||
		strings.Contains(combined, "almalinux") || strings.Contains(combined, "anolis") ||
		strings.Contains(combined, "opencloudos") || strings.Contains(combined, "openeuler"):
		if _, err := exec.LookPath("dnf"); err == nil {
			return "dnf"
		}
		if _, err := exec.LookPath("yum"); err == nil {
			return "yum"
		}
	case strings.Contains(combined, "arch") || strings.Contains(combined, "manjaro") ||
		strings.Contains(combined, "endeavouros"):
		if _, err := exec.LookPath("pacman"); err == nil {
			return "pacman"
		}
	case strings.Contains(combined, "suse"):
		if _, err := exec.LookPath("zypper"); err == nil {
			return "zypper"
		}
	case strings.Contains(combined, "alpine"):
		if _, err := exec.LookPath("apk"); err == nil {
			return "apk"
		}
	}

	// 按可执行文件兜底探测
	for _, candidate := range []struct {
		bin  string
		name string
	}{
		{"apt-get", "apt"},
		{"dnf", "dnf"},
		{"yum", "yum"},
		{"pacman", "pacman"},
		{"zypper", "zypper"},
		{"apk", "apk"},
		{"opkg", "opkg"},
	} {
		if _, err := exec.LookPath(candidate.bin); err == nil {
			return candidate.name
		}
	}
	return "unknown"
}

func detectInitSystem() string {
	if _, err := exec.LookPath("systemctl"); err == nil {
		if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
			return "systemd"
		}
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		if st, err := os.Stat("/run/openrc"); err == nil && st.IsDir() {
			return "openrc"
		}
	}
	return "daemon"
}
