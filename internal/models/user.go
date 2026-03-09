package models

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

// ListUsers 返回全部用户，保持管理端列表查询只依赖模型层接口。
func (s *Store) ListUsers() ([]User, error) {
	var items []User
	err := s.db.Order("created_at DESC").Order("id DESC").Find(&items).Error
	return items, err
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

// findOneUser 收敛单用户查询逻辑，避免多个入口维护重复的未命中处理。
func (s *Store) findOneUser(query string, args ...any) (User, error) {
	var item User
	err := s.db.Where(query, args...).First(&item).Error
	if err != nil {
		return User{}, normalizeNotFound(err)
	}
	return item, nil
}
