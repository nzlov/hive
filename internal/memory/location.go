package memory

import "github.com/nzlov/hive/internal/config"

// ResolveLocation 固定使用服务端自己的记忆库，保证所有项目共享同一份数据库。
func ResolveLocation(cfg config.AppConfig) Location {
	return Location{MemoryRoot: cfg.MemoryRoot}
}
