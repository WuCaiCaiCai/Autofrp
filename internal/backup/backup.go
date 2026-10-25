package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WuCaiCaiCai/Autofrp/internal/config"
)

// Manifest 备份包的元信息
type Manifest struct {
	App        string    `json:"app"`
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
	Hostname   string    `json:"hostname"`
	Distro     string    `json:"distro,omitempty"`
	HasSecrets bool      `json:"hasSecrets"`
	HasLegacy  bool      `json:"hasLegacy"`
	Files      int       `json:"files"`
}

// Options 导出选项
type Options struct {
	IncludeSecrets bool // 包含 Token / 密码 / 私钥
	IncludeLegacy  bool // 包含旧版 state.conf / services.conf 与 backup 历史目录
}

var allowedTopLevel = map[string]bool{
	"config.json":   true,
	"manifest.json": true,
	"frps.toml":     true,
	"frpc.toml":     true,
	"Caddyfile":     true,
	"state.conf":    true,
	"services.conf": true,
}

// Create 生成备份压缩包，返回临时文件路径（调用方负责删除）
func Create(store *config.Store, opts Options) (string, error) {
	cfg := store.Get()
	stateDir := store.StateDir()

	tmp, err := os.CreateTemp("", "autof-backup-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()

	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)

	files := 0
	addBytes := func(name string, data []byte, mode os.FileMode) error {
		hdr := &tar.Header{
			Name:     name,
			Mode:     int64(mode.Perm()),
			Size:     int64(len(data)),
			ModTime:  time.Now(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		files++
		return nil
	}
	addFile := func(rel string, mode os.FileMode) error {
		data, err := os.ReadFile(filepath.Join(stateDir, rel))
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return addBytes(rel, data, mode)
	}

	// 1. 配置（敏感信息可选脱敏）
	if opts.IncludeSecrets {
		if err := addFile("config.json", 0600); err != nil {
			return "", err
		}
	} else {
		sanitized := cfg
		sanitized.Frps.Token = ""
		sanitized.Frpc.Token = ""
		sanitized.Web.Password = ""
		sanitized.Web.AuthEnabled = true
		sanitized.Caddy.DNSToken = ""
		sanitized.Caddy.DNSSecret = ""
		data, err := json.MarshalIndent(sanitized, "", "  ")
		if err != nil {
			return "", err
		}
		if err := addBytes("config.json", data, 0600); err != nil {
			return "", err
		}
	}

	// 2. 生成的配置文件（含 Token，仅在包含敏感信息时导出）
	if opts.IncludeSecrets {
		for _, f := range []string{"frps.toml", "frpc.toml", "Caddyfile"} {
			if err := addFile(f, 0600); err != nil {
				return "", err
			}
		}
		if err := addFile(filepath.Join("clients", "frpc.toml"), 0600); err != nil {
			return "", err
		}
	}

	// 3. 证书：私钥跟随“包含敏感信息”选项
	if err := addDir(tw, filepath.Join(stateDir, "certs"), "certs", opts.IncludeSecrets, &files); err != nil {
		return "", err
	}
	if err := addDir(tw, filepath.Join(stateDir, "tls"), "tls", opts.IncludeSecrets, &files); err != nil {
		return "", err
	}

	// 4. 旧版配置与历史备份
	if opts.IncludeLegacy {
		for _, f := range []string{"state.conf", "services.conf"} {
			if err := addFile(f, 0600); err != nil {
				return "", err
			}
		}
		if err := addDir(tw, filepath.Join(stateDir, "backup"), "backup", true, &files); err != nil {
			return "", err
		}
	}

	hostname, _ := os.Hostname()
	manifest := Manifest{
		App:        "autofrp",
		Version:    config.AppVersion,
		CreatedAt:  time.Now(),
		Hostname:   hostname,
		HasSecrets: opts.IncludeSecrets,
		HasLegacy:  opts.IncludeLegacy,
		Files:      files,
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := addBytes("manifest.json", data, 0600); err != nil {
		return "", err
	}

	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return tmpPath, nil
}

func addDir(tw *tar.Writer, srcDir, prefix string, includePrivate bool, files *int) error {
	st, err := os.Stat(srcDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !includePrivate && (strings.HasSuffix(name, ".key") || strings.HasSuffix(name, "privkey.pem")) {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(name, ".key") || strings.HasSuffix(name, "privkey.pem") {
			mode = 0600
		}
		hdr := &tar.Header{
			Name:     filepath.ToSlash(filepath.Join(prefix, rel)),
			Mode:     int64(mode),
			Size:     int64(len(data)),
			ModTime:  info.ModTime(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		*files++
		return nil
	})
}

// Inspect 读取备份包 manifest 以便导入前预览
func Inspect(archive string) (*Manifest, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("不是有效的 .tar.gz 备份包: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "manifest.json" {
			var m Manifest
			if err := json.NewDecoder(tr).Decode(&m); err != nil {
				return nil, err
			}
			if m.App != "autofrp" {
				return nil, errors.New("不是 Autofrp 的备份包")
			}
			return &m, nil
		}
	}
	return nil, errors.New("备份包缺少 manifest.json")
}

// Restore 从备份包恢复配置与证书；keepCreds 表示保留当前面板账号密码与监听地址
func Restore(store *config.Store, archive string, keepCreds bool) (*config.Config, error) {
	if _, err := Inspect(archive); err != nil {
		return nil, err
	}

	current := store.Get()
	stateDir := store.StateDir()

	// 恢复前先把当前配置整体归档，便于回滚
	preDir := filepath.Join(store.BackupDir(), "pre-restore-"+time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(preDir, 0700); err == nil {
		for _, rel := range []string{"config.json", "Caddyfile", "frps.toml", "frpc.toml"} {
			if data, err := os.ReadFile(filepath.Join(stateDir, rel)); err == nil {
				_ = os.WriteFile(filepath.Join(preDir, filepath.Base(rel)), data, 0600)
			}
		}
	}

	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	restoredConfig := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		if !safeName(name) {
			continue
		}
		target := filepath.Join(stateDir, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(stateDir)+string(os.PathSeparator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(name, "privkey.pem") || strings.HasSuffix(name, ".key") || name == "config.json" {
			mode = 0600
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return nil, err
		}
		out.Close()
		if name == "config.json" {
			restoredConfig = true
		}
	}
	if !restoredConfig {
		return nil, errors.New("备份包中没有 config.json")
	}

	// 重新从磁盘加载恢复后的配置（否则内存里仍是旧值）
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("恢复后加载配置失败: %w", err)
	}

	// 保留当前面板凭据与监听地址，避免恢复后无法登录
	if keepCreds {
		_ = store.Update(func(c *config.Config) error {
			c.Web.Username = current.Web.Username
			c.Web.Password = current.Web.Password
			c.Web.ListenAddr = current.Web.ListenAddr
			c.Web.TLSListen = current.Web.TLSListen
			c.Web.TLSCertFile = current.Web.TLSCertFile
			c.Web.TLSKeyFile = current.Web.TLSKeyFile
			return nil
		})
	}

	cfg := store.Get()
	return &cfg, nil
}

func safeName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 {
		return allowedTopLevel[parts[0]]
	}
	switch parts[0] {
	case "certs", "tls", "clients", "backup":
		return true
	default:
		return false
	}
}
