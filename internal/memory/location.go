package memory

import (
	"os"
	"path/filepath"
	"strings"

	"memory-manager/internal/config"
)

// ResolveLocation 优先使用项目内 `.memory`，缺失时才回退到共享目录，保证项目可独立持久化。
func ResolveLocation(cfg config.AppConfig, projectRoot string) (Location, error) {
	resolvedRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return Location{}, err
	}
	projectName := sanitizeProjectName(filepath.Base(resolvedRoot))
	localMemoryRoot := filepath.Join(resolvedRoot, ".memory")
	if isDir(localMemoryRoot) {
		return Location{
			ProjectRoot:     resolvedRoot,
			ProjectName:     projectName,
			SearchRoot:      resolvedRoot,
			MemoryRoot:      localMemoryRoot,
			ExternalEnabled: false,
		}, nil
	}

	sharedRoot := filepath.Join(cfg.StorageRoot, ".memory")
	if err := os.MkdirAll(sharedRoot, 0o755); err == nil {
		return Location{
			ProjectRoot:     resolvedRoot,
			ProjectName:     projectName,
			SearchRoot:      cfg.StorageRoot,
			MemoryRoot:      sharedRoot,
			ExternalEnabled: true,
		}, nil
	}

	return Location{
		ProjectRoot:     resolvedRoot,
		ProjectName:     projectName,
		SearchRoot:      resolvedRoot,
		MemoryRoot:      localMemoryRoot,
		ExternalEnabled: false,
	}, nil
}

// sanitizeProjectName 保证项目名稳定可检索，避免空值污染共享库隔离维度。
func sanitizeProjectName(name string) string {
	cleaned := strings.Trim(strings.TrimSpace(name), "./")
	if cleaned == "" {
		return "default-project"
	}
	return cleaned
}

// isDir 只在路径决策时确认目录语义，避免把普通文件误当成记忆根目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
