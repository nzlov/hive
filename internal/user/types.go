package user

// User 统一描述数据库中的用户对象，避免鉴权、中间件和管理接口重复维护字段。
type User struct {
	ID           int64
	UserID       string
	Username     string
	RealName     string
	PasswordHash string
	Salt         string
	APIToken     string
	IsAdmin      bool
	CreatedAt    string
	UpdatedAt    string
}

// CreateInput 描述新增用户时允许外部提供的字段，统一由服务层补齐安全相关数据。
type CreateInput struct {
	Username string
	RealName string
	Password string
	IsAdmin  bool
}

// UpdateInput 描述用户更新能力，避免 HTTP 层直接感知数据库写入细节。
type UpdateInput struct {
	Username        string
	RealName        string
	Password        string
	IsAdmin         bool
	RegenerateToken bool
}
