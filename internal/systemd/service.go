package systemd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// HasSystemd 检测当前系统是否运行 systemd
func HasSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	st, err := os.Stat("/run/systemd/system")
	return err == nil && st.IsDir()
}

// FindBinary 查找组件可执行文件路径
func FindBinary(name string) string {
	candidates := []string{
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
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
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	fields := strings.Fields(line)
	if len(fields) > 0 {
		return strings.TrimPrefix(fields[0], "v")
	}
	return line
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
	}

	if !st.Running {
		if pid := findProcessPID(name, bin); pid > 0 {
			st.Running = true
			st.PID = pid
		}
	}
	return st
}

func findProcessPID(name, bin string) int {
	pattern := bin
	if pattern == "" {
		pattern = name
	}
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return 0
	}
	selfPID := os.Getpid()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && pid > 0 && pid != selfPID {
			return pid
		}
	}
	return 0
}

// WriteUnit 写入并重载 systemd 服务单元文件
func WriteUnit(name, description, execStart, execReload string, env map[string]string) error {
	if !HasSystemd() {
		return nil
	}
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

// Control 控制服务启停（支持 systemd 与无 systemd 回退）
func Control(name, action, fallbackCmd string, fallbackArgs []string, logDir string, env map[string]string) error {
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

	// 无 systemd 回退方案
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

// RemoveService 停止并卸载指定 systemd 服务
func RemoveService(name string) {
	if HasSystemd() {
		_ = exec.Command("systemctl", "stop", name).Run()
		_ = exec.Command("systemctl", "disable", name).Run()
		_ = os.Remove(filepath.Join("/etc/systemd/system", name+".service"))
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	stopProcessFallback(name, FindBinary(name))
}
