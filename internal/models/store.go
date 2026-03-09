package models

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/nzlov/hive/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ErrNotFound 统一收敛查询未命中的语义，避免业务层直接依赖 GORM 的错误细节。
var ErrNotFound = errors.New("record not found")

// Store 封装数据库连接与事务边界，避免业务层直接接触底层连接对象。
type Store struct {
	db          *gorm.DB
	driver      string
	sourceLabel string
}

type storeContextKey struct{}

// MemoryDBPath 统一 SQLite 数据库文件位置，避免路径拼接规则散落在业务层。
func MemoryDBPath(memoryRoot string) string {
	return filepath.Join(memoryRoot, "memory.db")
}

// Open 根据配置打开数据库并完成表结构初始化，只暴露模型方法不暴露底层连接。
func Open(cfg config.AppConfig) (*Store, error) {
	driver, dialector, sourceLabel, err := buildDialector(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, driver: driver, sourceLabel: sourceLabel}
	if err := store.migrate(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// StoreToContext 把共享 Store 注入上下文，避免业务层继续显式透传数据库依赖。
func StoreToContext(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, storeContextKey{}, store)
}

// StoreFromContext 从上下文提取共享 Store，确保服务层通过统一入口获取数据库连接。
func StoreFromContext(ctx context.Context) (*Store, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 为空")
	}
	store, ok := ctx.Value(storeContextKey{}).(*Store)
	if !ok || store == nil {
		return nil, fmt.Errorf("context 中缺少 store")
	}
	return store, nil
}

// Close 负责释放底层连接池，避免长时间运行时泄漏数据库连接。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SourceLabel 返回当前存储的可读标识，方便调试输出和结果定位。
func (s *Store) SourceLabel() string {
	if s == nil {
		return ""
	}
	return s.sourceLabel
}

// Driver 返回当前使用的数据库驱动名，便于上层做轻量调试展示。
func (s *Store) Driver() string {
	if s == nil {
		return ""
	}
	return s.driver
}

// WithTx 在不暴露底层事务对象的前提下复用一组模型方法，保证跨表写入的一致性。
func (s *Store) WithTx(fn func(*Store) error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store 未初始化")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		return fn(&Store{db: tx, driver: s.driver, sourceLabel: s.sourceLabel})
	})
}

// migrate 统一维护所有表结构，避免不同业务入口对初始化时机产生分歧。
func (s *Store) migrate() error {
	return s.db.AutoMigrate(&Memory{}, &MemoryEmbedding{}, &MemoryMetadata{}, &User{})
}

// buildDialector 根据配置构造当前数据库方言，默认回落到 SQLite 以兼容现有行为。
func buildDialector(cfg config.AppConfig) (string, gorm.Dialector, string, error) {
	if cfg.DatabaseConfig == nil || strings.TrimSpace(cfg.DatabaseConfig.Driver) == "" {
		return buildSQLiteDialector(cfg.MemoryRoot)
	}
	driver := normalizeDriver(cfg.DatabaseConfig.Driver)
	switch driver {
	case "sqlite":
		if strings.TrimSpace(cfg.DatabaseConfig.DSN) != "" {
			dsn := strings.TrimSpace(cfg.DatabaseConfig.DSN)
			return "sqlite", glebarezsqlite.Open(dsn), dsn, nil
		}
		return buildSQLiteDialector(cfg.MemoryRoot)
	case "postgres":
		dsn := strings.TrimSpace(cfg.DatabaseConfig.DSN)
		if dsn == "" {
			return "", nil, "", fmt.Errorf("postgresql 数据库缺少 dsn 配置")
		}
		return "postgres", postgres.Open(dsn), buildPostgresLabel(dsn), nil
	default:
		return "", nil, "", fmt.Errorf("不支持的数据库驱动: %s", cfg.DatabaseConfig.Driver)
	}
}

// buildSQLiteDialector 兼容现有本地文件存储模式，保证无额外配置时仍可直接运行。
func buildSQLiteDialector(memoryRoot string) (string, gorm.Dialector, string, error) {
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		return "", nil, "", err
	}
	path := MemoryDBPath(memoryRoot)
	return "sqlite", glebarezsqlite.Open(path), path, nil
}

// normalizeDriver 收敛驱动别名，避免配置层出现 postgres 和 postgresql 两套判断。
func normalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql":
		return "postgres"
	case "sqlite":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

// buildPostgresLabel 输出脱敏后的连接标识，避免调试信息直接泄露完整凭据。
func buildPostgresLabel(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err == nil && parsed.Host != "" {
		name := strings.TrimPrefix(parsed.Path, "/")
		if name == "" {
			name = "database"
		}
		return fmt.Sprintf("postgresql://%s/%s", parsed.Host, name)
	}
	return "postgresql"
}

// normalizeNotFound 统一转换底层未命中错误，避免业务层散落 GORM 专有判断。
func normalizeNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
