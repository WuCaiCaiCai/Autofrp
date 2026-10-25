package systemd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ServiceStatus 表示组件（frps / frpc / caddy / autof）的安装与运行状态
type ServiceStatus struct {
	Name       string `json:"name"`
	Installed  bool   `json:"installed"`
	BinaryPath string `json:"binaryPath"`
	Version    string `json:"version"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid"`
	Enabled    bool   `json:"enabled"`
}

// HasSystemd 检测当前系统是否运行 systemd (Debian / Deepin / Fedora / Arch 等主流发行版)
func HasSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	st, err := os.Stat("/run/systemd/system")
	return err == nil && st.IsDir()
}

// HasOpenRC 检测当前系统是否运行 OpenRC (Alpine / Gentoo 等)
func HasOpenRC() bool {
	if _, err := exec.LookPath("rc-service"); err != nil {
		return false
	}
	st, err := os.Stat("/run/openrc")
	return err == nil && st.IsDir()
}

// FixSELinuxContext 修复 Fedora / RHEL / CentOS 等开启 SELinux 系统的二进制安全上下文，避免 systemd 203/EXEC 权限报错
func FixSELinuxContext(binPath string) {
	if binPath == "" {
		return
	}
	if restorecon, err := exec.LookPath("restorecon"); err == nil {
		_ = exec.Command(restorecon, "-F", binPath).Run()
		return
	}
	if chcon, err := exec.LookPath("chcon"); err == nil {
		_ = exec.Command(chcon, "-t", "bin_t", binPath).Run()
	}
}

// FindBinary 查找组件可执行文件路径
func FindBinary(name string) string {
	candidates := []string{
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
		filepath.Join("/bin", name),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return c
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// 语义化版本：必须三段（如 2.10.1），避免误匹配日志里的时间戳/浮点数
var versionPattern = regexp.MustCompile(`(?:^|[\s"'])v?(\d+\.\d+\.\d+)`)

// GetVersion 获取二进制组件的版本号
func GetVersion(name string) string {
	bin := FindBinary(name)
	if bin == "" {
		return ""
	}
	var cmd *exec.Cmd
	switch name {
	case "frps", "frpc":
		cmd = exec.Command(bin, "-v")
	case "caddy":
		cmd = exec.Command(bin, "version")
	default:
		cmd = exec.Command(bin, "--version")
	}
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	// caddy version 可能先输出一行 JSON 警告（stderr），真正的版本在后面的行里
	for _, line := range strings.Split(string(out), "\n") {
		if m := versionPattern.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// QueryStatus 查询指定服务的安装与运行状态
func QueryStatus(name string) ServiceStatus {
	bin := FindBinary(name)
	st := ServiceStatus{
		Name:       name,
		Installed:  bin != "",
		BinaryPath: bin,
	}
	if !st.Installed {
		return st
	}
	st.Version = GetVersion(name)

	if HasSystemd() {
		if err := exec.Command("systemctl", "is-active", "--quiet", name).Run(); err == nil {
			st.Running = true
		}
		if err := exec.Command("systemctl", "is-enabled", "--quiet", name).Run(); err == nil {
			st.Enabled = true
		}
		if out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", name).Output(); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
				st.PID = pid
			}
		}
	} else if HasOpenRC() {
		if err := exec.Command("rc-service", name, "status").Run(); err == nil {
			st.Running = true
		}
	}

	if !st.Running || st.PID == 0 {
		if pid := findProcessPID(name, bin); pid > 0 {
			st.Running = true
			st.PID = pid
		}
	}
	return st
}

// findProcessPID 纯 Go 扫描 /proc 获取进程 PID（不依赖外部 pgrep/procps 包，适配精简版 Debian/Arch/Fedora 容器）
func findProcessPID(name, bin string) int {
	selfPID := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid <= 1 || pid == selfPID {
				continue
			}
			cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
			if err != nil || len(cmdlineBytes) == 0 {
				continue
			}
			parts := strings.Split(string(cmdlineBytes), "\x00")
			arg0 := parts[0]
			if (bin != "" && arg0 == bin) || filepath.Base(arg0) == name {
				return pid
			}
		}
	}
	return 0
}

// WriteUnit 写入并注册系统服务（支持 systemd 与 OpenRC）
func WriteUnit(name, description, execStart, execReload string, env map[string]string) error {
	binFields := strings.Fields(execStart)
	if len(binFields) > 0 {
		FixSELinuxContext(binFields[0])
	}

	if HasSystemd() {
		var envLines strings.Builder
		for k, v := range env {
			if v != "" {
				envLines.WriteString(fmt.Sprintf("Environment=\"%s=%s\"\n", k, strings.ReplaceAll(v, `"`, `\"`)))
			}
		}
		reloadLine := ""
		if execReload != "" {
			reloadLine = fmt.Sprintf("ExecReload=%s\n", execReload)
		}
		unit := fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
%sExecStart=%s
%sRestart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`, description, envLines.String(), execStart, reloadLine)

		unitPath := filepath.Join("/etc/systemd/system", name+".service")
		if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
			return fmt.Errorf("写入 systemd 服务文件失败: %w", err)
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
		_ = exec.Command("systemctl", "enable", name).Run()
		return nil
	}

	if HasOpenRC() && len(binFields) > 0 {
		cmdBin := binFields[0]
		cmdArgs := strings.Join(binFields[1:], " ")
		var envExports strings.Builder
		for k, v := range env {
			if v != "" {
				envExports.WriteString(fmt.Sprintf("export %s=%q\n", k, v))
			}
		}
		rcScript := fmt.Sprintf(`#!/sbin/openrc-run
description=%q
command=%q
command_args=%q
command_background="yes"
pidfile="/run/${RC_SVCNAME}.pid"
%s
depend() {
	need net
}
`, description, cmdBin, cmdArgs, envExports.String())
		initPath := filepath.Join("/etc/init.d", name)
		if err := os.WriteFile(initPath, []byte(rcScript), 0755); err == nil {
			_ = exec.Command("rc-update", "add", name, "default").Run()
		}
	}

	return nil
}

// Control 控制服务启停（自动适配 systemd / OpenRC / 后台守护进程回退）
func Control(name, action, fallbackCmd string, fallbackArgs []string, logDir string, env map[string]string) error {
	if fallbackCmd != "" {
		FixSELinuxContext(fallbackCmd)
	}

	if HasSystemd() {
		unitPath := filepath.Join("/etc/systemd/system", name+".service")
		if _, err := os.Stat(unitPath); err == nil || name == "caddy" {
			switch action {
			case "start", "stop", "restart":
				out, err := exec.Command("systemctl", action, name).CombinedOutput()
				if err != nil {
					return fmt.Errorf("systemctl %s %s 失败: %s", action, name, strings.TrimSpace(string(out)))
				}
				return nil
			case "reload":
				if err := exec.Command("systemctl", "reload", name).Run(); err != nil {
					out, err2 := exec.Command("systemctl", "restart", name).CombinedOutput()
					if err2 != nil {
						return fmt.Errorf("重载/重启 %s 失败: %s", name, strings.TrimSpace(string(out)))
					}
				}
				return nil
			}
		}
	}

	if HasOpenRC() {
		initPath := filepath.Join("/etc/init.d", name)
		if _, err := os.Stat(initPath); err == nil {
			rcAction := action
			if rcAction == "reload" {
				rcAction = "restart"
			}
			if out, err := exec.Command("rc-service", name, rcAction).CombinedOutput(); err == nil {
				return nil
			} else {
				_ = out
			}
		}
	}

	// 通用后台守护回退方案（无 systemd 的精简容器 / WSL 等）
	switch action {
	case "stop":
		stopProcessFallback(name, fallbackCmd)
		return nil
	case "start":
		if st := QueryStatus(name); st.Running {
			return nil
		}
		return startProcessFallback(name, fallbackCmd, fallbackArgs, logDir, env)
	case "restart", "reload":
		stopProcessFallback(name, fallbackCmd)
		time.Sleep(300 * time.Millisecond)
		return startProcessFallback(name, fallbackCmd, fallbackArgs, logDir, env)
	default:
		return fmt.Errorf("未知操作: %s", action)
	}
}

func stopProcessFallback(name, bin string) {
	pid := findProcessPID(name, bin)
	if pid > 0 {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
}

func startProcessFallback(name, bin string, args []string, logDir string, env map[string]string) error {
	if bin == "" {
		bin = FindBinary(name)
	}
	if bin == "" {
		return fmt.Errorf("未找到可执行文件: %s", name)
	}
	FixSELinuxContext(bin)
	if logDir == "" {
		logDir = "/tmp"
	}
	_ = os.MkdirAll(logDir, 0755)
	logFile, err := os.OpenFile(filepath.Join(logDir, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	go func() {
		_ = cmd.Wait()
		logFile.Close()
	}()
	time.Sleep(400 * time.Millisecond)
	return nil
}

// ReadLogs 读取最近 N 行服务日志
func ReadLogs(name, logDir string, lines int) string {
	if lines <= 0 {
		lines = 100
	}
	if HasSystemd() {
		out, err := exec.Command("journalctl", "-u", name, "-n", strconv.Itoa(lines), "--no-pager").CombinedOutput()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			return string(out)
		}
	}
	if logDir != "" {
		logPath := filepath.Join(logDir, name+".log")
		if data, err := os.ReadFile(logPath); err == nil && len(data) > 0 {
			allLines := strings.Split(string(data), "\n")
			if len(allLines) > lines {
				allLines = allLines[len(allLines)-lines:]
			}
			return strings.Join(allLines, "\n")
		}
	}
	return fmt.Sprintf("暂无 %s 日志记录", name)
}

// RemoveService 停止并卸载指定服务
func RemoveService(name string) {
	if HasSystemd() {
		_ = exec.Command("systemctl", "stop", name).Run()
		_ = exec.Command("systemctl", "disable", name).Run()
		_ = os.Remove(filepath.Join("/etc/systemd/system", name+".service"))
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	if HasOpenRC() {
		_ = exec.Command("rc-service", name, "stop").Run()
		_ = exec.Command("rc-update", "del", name, "default").Run()
		_ = os.Remove(filepath.Join("/etc/init.d", name))
	}
	stopProcessFallback(name, FindBinary(name))
}
