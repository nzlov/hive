package user

import (
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
)

// Service 封装用户与鉴权相关能力，避免 HTTP 层直接拼接安全逻辑和 SQL。
type Service struct {
	config config.AppConfig
}

// NewService 创建用户服务，确保登录、用户管理和 token 校验共享同一配置。
func NewService(cfg config.AppConfig) *Service {
	return &Service{config: cfg}
}

// JWTSecret 统一暴露 JWT 密钥给中间件，避免路由层直接访问用户服务内部配置字段。
func (s *Service) JWTSecret() string {
	return s.config.JWTSecret
}

// EnsureDefaultAdmin 在首次启动时补齐管理员账号，降低空库下的初始化门槛。
func (s *Service) EnsureDefaultAdmin() (User, string, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, "", err
	}
	defer db.Close()

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return User{}, "", err
	}
	if total > 0 {
		user, err := s.findByUsernameWithDB(db, "admin")
		if err == nil {
			return user, "", nil
		}
		return User{}, "", nil
	}
	password, err := randomString(12)
	if err != nil {
		return User{}, "", err
	}
	created, err := s.createUserWithDB(db, CreateInput{
		Username: "admin",
		RealName: "系统管理员",
		Password: password,
		IsAdmin:  true,
	})
	if err != nil {
		return User{}, "", err
	}
	return created, password, nil
}

// AuthenticateLogin 校验用户名密码，避免管理端登录把密码比对规则散落到控制器里。
func (s *Service) AuthenticateLogin(username, password string) (User, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	user, err := s.findByUsernameWithDB(db, username)
	if err != nil {
		return User{}, fmt.Errorf("用户名或密码错误")
	}
	if hashPassword(password, user.Salt) != user.PasswordHash {
		return User{}, fmt.Errorf("用户名或密码错误")
	}
	return user, nil
}

// ListUsers 返回用户管理页所需列表，避免前端直接依赖数据库表结构。
func (s *Service) ListUsers() ([]User, error) {
	db, err := s.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at FROM users ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

// CreateUser 新增一个用户，并统一生成 UUID 与 API Token 避免调用方绕过安全规则。
func (s *Service) CreateUser(input CreateInput) (User, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	return s.createUserWithDB(db, input)
}

// UpdateUser 修改用户资料，并在需要时重置密码或 API Token。
func (s *Service) UpdateUser(id int64, input UpdateInput) (User, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	existing, err := s.findByIDWithDB(db, id)
	if err != nil {
		return User{}, err
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = existing.Username
	}
	realName := strings.TrimSpace(input.RealName)
	if realName == "" {
		realName = existing.RealName
	}
	passwordHash := existing.PasswordHash
	salt := existing.Salt
	if strings.TrimSpace(input.Password) != "" {
		var err error
		salt, err = randomString(8)
		if err != nil {
			return User{}, err
		}
		passwordHash = hashPassword(input.Password, salt)
	}
	apiToken := existing.APIToken
	if input.RegenerateToken {
		var err error
		apiToken, err = randomString(32)
		if err != nil {
			return User{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`UPDATE users SET username = ?, real_name = ?, password_hash = ?, salt = ?, apitoken = ?, is_admin = ?, updated_at = ? WHERE id = ?`, username, realName, passwordHash, salt, apiToken, boolToInt(input.IsAdmin), now, id)
	if err != nil {
		return User{}, err
	}
	return s.findByIDWithDB(db, id)
}

// DeleteUser 删除指定用户，同时阻止管理员误删自己的当前账号。
func (s *Service) DeleteUser(id int64, requesterUserID string) error {
	db, err := s.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	target, err := s.findByIDWithDB(db, id)
	if err != nil {
		return err
	}
	if requesterUserID != "" && requesterUserID == target.UserID {
		return fmt.Errorf("不能删除当前登录用户")
	}
	_, err = db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// AuthenticateAPIToken 根据 API Token 解析调用用户，为记忆接口补齐来源追踪信息。
func (s *Service) AuthenticateAPIToken(apiToken string) (User, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	return s.findByAPITokenWithDB(db, apiToken)
}

// FindByUserID 供 JWT 中间件在需要时回查用户详情，避免信任过期的令牌内容。
func (s *Service) FindByUserID(userID string) (User, error) {
	db, err := s.openDB()
	if err != nil {
		return User{}, err
	}
	defer db.Close()
	return s.findByUserIDWithDB(db, userID)
}

// openDB 统一复用同一份 SQLite 文件，确保用户和记忆数据共享同一存储位置。
func (s *Service) openDB() (*sql.DB, error) {
	location := memory.ResolveLocation(s.config)
	return memory.ConnectDB(location.MemoryRoot)
}

// createUserWithDB 在事务边界简单场景下复用同一创建逻辑，避免默认管理员和后台新增出现漂移。
func (s *Service) createUserWithDB(db *sql.DB, input CreateInput) (User, error) {
	username := strings.TrimSpace(input.Username)
	realName := strings.TrimSpace(input.RealName)
	password := strings.TrimSpace(input.Password)
	if username == "" {
		return User{}, fmt.Errorf("用户名不能为空")
	}
	if password == "" {
		return User{}, fmt.Errorf("密码不能为空")
	}
	if realName == "" {
		realName = username
	}
	salt, err := randomString(8)
	if err != nil {
		return User{}, err
	}
	apiToken, err := randomString(32)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	userID := uuid.NewString()
	result, err := db.Exec(`INSERT INTO users (userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, userID, username, realName, hashPassword(password, salt), salt, apiToken, boolToInt(input.IsAdmin), now, now)
	if err != nil {
		return User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.findByIDWithDB(db, id)
}

// findByUsernameWithDB 统一按用户名读取用户，避免登录和管理逻辑维护多套扫描顺序。
func (s *Service) findByUsernameWithDB(db *sql.DB, username string) (User, error) {
	return queryOneUser(db, `SELECT id, userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at FROM users WHERE username = ?`, strings.TrimSpace(username))
}

// findByIDWithDB 统一按主键读取用户，方便更新和删除后的结果回读。
func (s *Service) findByIDWithDB(db *sql.DB, id int64) (User, error) {
	return queryOneUser(db, `SELECT id, userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at FROM users WHERE id = ?`, id)
}

// findByUserIDWithDB 统一按业务 UUID 读取用户，避免 JWT 中间件继续暴露内部自增主键。
func (s *Service) findByUserIDWithDB(db *sql.DB, userID string) (User, error) {
	return queryOneUser(db, `SELECT id, userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at FROM users WHERE userid = ?`, strings.TrimSpace(userID))
}

// findByAPITokenWithDB 统一按 API Token 解析用户，保证 token 路由与记忆写入共享同一来源身份。
func (s *Service) findByAPITokenWithDB(db *sql.DB, apiToken string) (User, error) {
	return queryOneUser(db, `SELECT id, userid, username, real_name, password_hash, salt, apitoken, is_admin, created_at, updated_at FROM users WHERE apitoken = ?`, strings.TrimSpace(apiToken))
}

// queryOneUser 集中处理单用户查询结果，避免调用方重复判断 sql.ErrNoRows 细节。
func queryOneUser(db *sql.DB, query string, args ...any) (User, error) {
	var user User
	var isAdmin int
	err := db.QueryRow(query, args...).Scan(&user.ID, &user.UserID, &user.Username, &user.RealName, &user.PasswordHash, &user.Salt, &user.APIToken, &isAdmin, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("用户不存在")
	}
	if err != nil {
		return User{}, err
	}
	user.IsAdmin = isAdmin == 1
	return user, nil
}

// scanUsers 集中转换用户列表，避免不同列表查询重复维护字段顺序。
func scanUsers(rows *sql.Rows) ([]User, error) {
	items := make([]User, 0)
	for rows.Next() {
		var item User
		var isAdmin int
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.RealName, &item.PasswordHash, &item.Salt, &item.APIToken, &isAdmin, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IsAdmin = isAdmin == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

// hashPassword 使用 sha1(password+salt) 满足当前兼容要求，同时把算法细节集中在单点方便后续升级。
func hashPassword(password, salt string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(password) + strings.TrimSpace(salt)))
	return hex.EncodeToString(sum[:])
}

// randomString 统一生成密码、盐值和 API Token，避免不同调用点生成规则不一致。
func randomString(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// boolToInt 让 SQLite 写入布尔字段时保持显式整数语义，避免驱动差异导致兼容问题。
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
