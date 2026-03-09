package models

import (
	"math"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// User 对应 users 表，集中维护用户持久化字段与唯一索引约束。
type User struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement"`
	UserID       string `gorm:"column:userid;type:text;not null;uniqueIndex:idx_users_userid"`
	Username     string `gorm:"column:username;type:text;not null;uniqueIndex:idx_users_username"`
	RealName     string `gorm:"column:real_name;type:text;not null;default:''"`
	PasswordHash string `gorm:"column:password_hash;type:text;not null"`
	Salt         string `gorm:"column:salt;type:text;not null"`
	APIToken     string `gorm:"column:apitoken;type:text;not null;uniqueIndex:idx_users_apitoken"`
	IsAdmin      bool   `gorm:"column:is_admin;not null;default:false"`
	CreatedAt    string `gorm:"column:created_at;type:text;not null"`
	UpdatedAt    string `gorm:"column:updated_at;type:text;not null"`
}

// TableName 固定表名，避免命名策略改变影响既有用户数据。
func (User) TableName() string {
	return "users"
}

// CountUsers 返回用户总数，供默认管理员初始化逻辑判断是否为空库。
func (s *Store) CountUsers() (int64, error) {
	var total int64
	err := s.db.Model(&User{}).Count(&total).Error
	return total, err
}

// ListUsersPaginated 返回分页用户列表，并统一在用户名与真实名上应用关键字筛选。
func (s *Store) ListUsersPaginated(page, pageSize int, keyword string) ([]User, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	pageSize = int(math.Min(float64(pageSize), 100))

	db := applyUserKeywordFilter(s.db.Model(&User{}), keyword)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []User
	err := db.Order("created_at DESC").Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// CreateUser 写入一个新用户，并返回数据库实际持久化后的记录。
func (s *Store) CreateUser(item User) (User, error) {
	if err := s.db.Create(&item).Error; err != nil {
		return User{}, err
	}
	return item, nil
}

// SaveUser 更新现有用户记录，避免业务层直接拼接更新字段列表。
func (s *Store) SaveUser(item User) (User, error) {
	if err := s.db.Model(&User{}).Where("id = ?", item.ID).Updates(map[string]any{
		"username":      item.Username,
		"real_name":     item.RealName,
		"password_hash": item.PasswordHash,
		"salt":          item.Salt,
		"apitoken":      item.APIToken,
		"is_admin":      item.IsAdmin,
		"updated_at":    item.UpdatedAt,
	}).Error; err != nil {
		return User{}, err
	}
	return s.FindUserByID(item.ID)
}

// DeleteUserByID 删除指定用户，避免业务层继续使用裸 SQL 删除记录。
func (s *Store) DeleteUserByID(id int64) error {
	return s.db.Delete(&User{}, id).Error
}

// FindUserByID 按主键读取用户，供更新和删除后的回读逻辑复用。
func (s *Store) FindUserByID(id int64) (User, error) {
	return s.findOneUser("id = ?", id)
}

// FindUserByUserID 按业务用户 ID 查询用户，避免上层依赖内部自增主键。
func (s *Store) FindUserByUserID(userID string) (User, error) {
	return s.findOneUser("userid = ?", userID)
}

// FindUserByUsername 按用户名查询用户，统一登录和管理端的读取入口。
func (s *Store) FindUserByUsername(username string) (User, error) {
	return s.findOneUser("username = ?", username)
}

// FindUserByAPIToken 按 API Token 查询用户，供接口鉴权和记忆写入复用。
func (s *Store) FindUserByAPIToken(apiToken string) (User, error) {
	return s.findOneUser("apitoken = ?", apiToken)
}

// FindUsersByUserIDs 批量按业务用户 ID 查询用户，避免记忆列表按行回查造成额外数据库压力。
func (s *Store) FindUsersByUserIDs(userIDs []string) ([]User, error) {
	cleaned := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		value := strings.TrimSpace(userID)
		if value == "" || slices.Contains(cleaned, value) {
			continue
		}
		cleaned = append(cleaned, value)
	}
	if len(cleaned) == 0 {
		return []User{}, nil
	}
	var items []User
	err := s.db.Where("userid IN ?", cleaned).Find(&items).Error
	return items, err
}

// findOneUser 收敛单用户查询逻辑，避免多个入口维护重复的未命中处理。
func (s *Store) findOneUser(query string, args ...any) (User, error) {
	var item User
	err := s.db.Where(query, args...).First(&item).Error
	if err != nil {
		return User{}, normalizeNotFound(err)
	}
	return item, nil
}

// applyUserKeywordFilter 统一用户名与真实名的搜索条件，避免分页列表和后续扩展出现筛选口径漂移。
func applyUserKeywordFilter(db *gorm.DB, keyword string) *gorm.DB {
	cleaned := strings.TrimSpace(keyword)
	if cleaned == "" {
		return db
	}
	likeValue := "%" + escapeUserLike(cleaned) + "%"
	return db.Where(strings.Join([]string{
		"(",
		"LOWER(username) LIKE LOWER(?) ESCAPE '\\'",
		"OR LOWER(real_name) LIKE LOWER(?) ESCAPE '\\'",
		")",
	}, " "), likeValue, likeValue)
}

// escapeUserLike 转义 LIKE 通配符，避免关键字里的特殊字符改变用户名搜索语义。
func escapeUserLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}
