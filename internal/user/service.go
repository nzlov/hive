package user

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
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
func (s *Service) EnsureDefaultAdmin(ctx context.Context) (User, string, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, "", err
	}

	total, err := store.CountUsers()
	if err != nil {
		return User{}, "", err
	}
	if total > 0 {
		user, err := s.findByUsername(store, "admin")
		if err == nil {
			return user, "", nil
		}
		return User{}, "", nil
	}
	password, err := randomString(12)
	if err != nil {
		return User{}, "", err
	}
	created, err := s.createUser(store, CreateInput{
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
func (s *Service) AuthenticateLogin(ctx context.Context, username, password string) (User, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	user, err := s.findByUsername(store, username)
	if err != nil {
		return User{}, fmt.Errorf("用户名或密码错误")
	}
	if hashPassword(password, user.Salt) != user.PasswordHash {
		return User{}, fmt.Errorf("用户名或密码错误")
	}
	return user, nil
}

// ListUsers 返回用户管理页所需分页列表，避免控制器自己拼接筛选和分页细节。
func (s *Service) ListUsers(ctx context.Context, page, pageSize int, keyword string) ([]User, int64, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, total, err := store.ListUsersPaginated(page, pageSize, keyword)
	if err != nil {
		return nil, 0, err
	}
	return toUsers(items), total, nil
}

// CreateUser 新增一个用户，并统一生成 UUID 与 API Token 避免调用方绕过安全规则。
func (s *Service) CreateUser(ctx context.Context, input CreateInput) (User, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	return s.createUser(store, input)
}

// UpdateUser 修改用户资料，并在需要时重置密码或 API Token。
func (s *Service) UpdateUser(ctx context.Context, id int64, input UpdateInput) (User, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	existing, err := s.findByID(store, id)
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
	return s.saveUser(store, models.User{ID: id, UserID: existing.UserID, Username: username, RealName: realName, PasswordHash: passwordHash, Salt: salt, APIToken: apiToken, IsAdmin: input.IsAdmin, CreatedAt: existing.CreatedAt, UpdatedAt: now})
}

// DeleteUser 删除指定用户，同时阻止管理员误删自己的当前账号。
func (s *Service) DeleteUser(ctx context.Context, id int64, requesterUserID string) error {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return err
	}
	target, err := s.findByID(store, id)
	if err != nil {
		return err
	}
	if requesterUserID != "" && requesterUserID == target.UserID {
		return fmt.Errorf("不能删除当前登录用户")
	}
	return store.DeleteUserByID(id)
}

// AuthenticateAPIToken 根据 API Token 解析调用用户，为记忆接口补齐来源追踪信息。
func (s *Service) AuthenticateAPIToken(ctx context.Context, apiToken string) (User, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	return s.findByAPIToken(store, apiToken)
}

// FindByUserID 供 JWT 中间件在需要时回查用户详情，避免信任过期的令牌内容。
func (s *Service) FindByUserID(ctx context.Context, userID string) (User, error) {
	store, err := models.StoreFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	return s.findByUserID(store, userID)
}

// createUser 在默认管理员和后台新增场景复用同一套持久化逻辑，避免安全规则漂移。
func (s *Service) createUser(store *models.Store, input CreateInput) (User, error) {
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
	item, err := store.CreateUser(models.User{UserID: userID, Username: username, RealName: realName, PasswordHash: hashPassword(password, salt), Salt: salt, APIToken: apiToken, IsAdmin: input.IsAdmin, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return User{}, err
	}
	return toUser(item), nil
}

// findByUsername 统一按用户名读取用户，避免登录和管理逻辑维护多套查询路径。
func (s *Service) findByUsername(store *models.Store, username string) (User, error) {
	item, err := store.FindUserByUsername(strings.TrimSpace(username))
	if err == models.ErrNotFound {
		return User{}, fmt.Errorf("用户不存在")
	}
	if err != nil {
		return User{}, err
	}
	return toUser(item), nil
}

// findByIDWithDB 统一按主键读取用户，方便更新和删除后的结果回读。
func (s *Service) findByID(store *models.Store, id int64) (User, error) {
	item, err := store.FindUserByID(id)
	if err == models.ErrNotFound {
		return User{}, fmt.Errorf("用户不存在")
	}
	if err != nil {
		return User{}, err
	}
	return toUser(item), nil
}

// findByUserIDWithDB 统一按业务 UUID 读取用户，避免 JWT 中间件继续暴露内部自增主键。
func (s *Service) findByUserID(store *models.Store, userID string) (User, error) {
	item, err := store.FindUserByUserID(strings.TrimSpace(userID))
	if err == models.ErrNotFound {
		return User{}, fmt.Errorf("用户不存在")
	}
	if err != nil {
		return User{}, err
	}
	return toUser(item), nil
}

// findByAPITokenWithDB 统一按 API Token 解析用户，保证 token 路由与记忆写入共享同一来源身份。
func (s *Service) findByAPIToken(store *models.Store, apiToken string) (User, error) {
	item, err := store.FindUserByAPIToken(strings.TrimSpace(apiToken))
	if err == models.ErrNotFound {
		return User{}, fmt.Errorf("用户不存在")
	}
	if err != nil {
		return User{}, err
	}
	return toUser(item), nil
}

// saveUser 统一把业务层更新后的用户对象落库，避免服务层感知字段级更新细节。
func (s *Service) saveUser(store *models.Store, item models.User) (User, error) {
	updated, err := store.SaveUser(item)
	if err != nil {
		return User{}, err
	}
	return toUser(updated), nil
}

// toUser 收敛模型层到业务层的映射，避免其他逻辑直接依赖持久化结构体。
func toUser(item models.User) User {
	return User{ID: item.ID, UserID: item.UserID, Username: item.Username, RealName: item.RealName, PasswordHash: item.PasswordHash, Salt: item.Salt, APIToken: item.APIToken, IsAdmin: item.IsAdmin, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

// toUsers 批量转换用户列表，避免管理端查询重复维护字段拷贝逻辑。
func toUsers(items []models.User) []User {
	out := make([]User, 0, len(items))
	for _, item := range items {
		out = append(out, toUser(item))
	}
	return out
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
