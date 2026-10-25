package systemd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionPattern(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		// caddy version 可能先在 stderr 输出 JSON 警告，时间戳不能被误判为版本
		{`{"level":"warn","ts":1792898450.1320956,"msg":"unable to run: context canceled"}`, ""},
		{`{"level":"warn","ts":1792898450.1320956,"duration":0.224878455}`, ""},
		{"v2.10.1 h1:xxxxxxxx", "2.10.1"},
		{"0.61.2", "0.61.2"},
		{"Autofrp v1.0.3", "1.0.3"},
		{"unknown", ""},
	}
	for _, c := range cases {
		got := ""
		if m := versionPattern.FindStringSubmatch(c.line); m != nil {
			got = m[1]
		}
		if got != c.want {
			t.Fatalf("line %q: got %q, want %q", c.line, got, c.want)
		}
	}
}

func TestGetVersionIgnoresNoisyStderr(t *testing.T) {
	for _, p := range []string{"/usr/local/bin/caddy", "/usr/bin/caddy", "/bin/caddy"} {
		if _, err := os.Stat(p); err == nil {
			t.Skip("本机已安装 caddy，跳过 PATH 模拟测试")
		}
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "caddy")
	content := `#!/bin/sh
printf '%s\n' '{"level":"warn","ts":1792898450.1320956,"msg":"unable to run: context canceled"}' >&2
echo 'v2.10.1 h1:abcdef'
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if got := GetVersion("caddy"); got != "2.10.1" {
		t.Fatalf("GetVersion(caddy) = %q, want 2.10.1", got)
	}
}
