package config

import "testing"

// TestResolveServerListenAddr 验证监听地址可从配置读取，避免服务端继续回退到固定端口。
func TestResolveServerListenAddr(t *testing.T) {
	t.Helper()
	payload := map[string]any{
		"server": map[string]any{
			"listen_addr": ":19090",
		},
	}
	if got := resolveServerListenAddr(payload); got != ":19090" {
		t.Fatalf("resolveServerListenAddr() = %q, want %q", got, ":19090")
	}

	if got := resolveServerListenAddr(map[string]any{}); got != defaultServerListenAddr {
		t.Fatalf("默认监听地址异常: got=%q want=%q", got, defaultServerListenAddr)
	}
}
