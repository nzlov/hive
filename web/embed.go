package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"path/filepath"
	"strings"
)

// distFS 持有前端构建产物，让 Go 二进制可以独立分发管理界面。
//
//go:embed dist/* dist/assets/*
var distFS embed.FS

// ReadAsset 统一读取嵌入静态资源，并根据扩展名返回浏览器可识别的内容类型。
func ReadAsset(assetPath string) ([]byte, string, error) {
	cleaned := strings.TrimPrefix(strings.TrimSpace(assetPath), "/")
	if cleaned == "" {
		cleaned = "index.html"
	}
	payload, err := fs.ReadFile(distFS, filepath.ToSlash(filepath.Join("dist", cleaned)))
	if err != nil {
		return nil, "", fmt.Errorf("读取前端资源失败: %w", err)
	}
	contentType := mime.TypeByExtension(filepath.Ext(cleaned))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if strings.HasSuffix(cleaned, ".html") {
		contentType = "text/html; charset=utf-8"
	}
	return payload, contentType, nil
}
