package accountService

import (
	"time"
)

// User 用户模型
// 对应数据库表: users
type User struct {
	ID            int64      `json:"id" gorm:"primaryKey;autoIncrement;comment:用户ID[ID]"`
	UUID          string     `json:"uuid" gorm:"uniqueIndex;size:36;not null;comment:用户唯一标识[UUID]"`
	Username      string     `json:"username" gorm:"uniqueIndex;size:50;not null;comment:用户名[用户名]"`
	Email         *string    `json:"email" gorm:"uniqueIndex;size:100;comment:邮箱[邮箱]"`
	Phone         *string    `json:"phone" gorm:"uniqueIndex;size:20;comment:手机号[手机号]"`
	PasswordHash  string     `json:"-" gorm:"size:255;not null;comment:密码哈希[密码哈希]"`
	Salt          string     `json:"-" gorm:"size:64;comment:密码盐值[密码盐值]"`
	Nickname      *string    `json:"nickname" gorm:"size:50;comment:昵称[昵称]"`
	Status        int8       `json:"status" gorm:"type:tinyint;default:1;comment:用户状态[0:禁用;1:正常;2:锁定]"`
	EmailVerified int8       `json:"email_verified" gorm:"type:tinyint;default:0;comment:邮箱是否验证[0:未验证;1:已验证]"`
	PhoneVerified int8       `json:"phone_verified" gorm:"type:tinyint;default:0;comment:手机是否验证[0:未验证;1:已验证]"`
	LastLoginAt   *time.Time `json:"last_login_at" gorm:"comment:最后登录时间[最后登录时间]"`
	LastLoginIP   *string    `json:"last_login_ip" gorm:"size:45;comment:最后登录IP[最后登录IP]"`
	LoginCount    int64      `json:"login_count" gorm:"default:0;comment:登录次数[登录次数]"`
	CreatedAt     time.Time  `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
	UpdatedAt     time.Time  `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间[更新时间]"`
	DeletedAt     *time.Time `json:"deleted_at" gorm:"index;comment:删除时间[删除时间]"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// TableComment 返回表注释
func (User) TableComment() string {
	return "用户表"
}

// Role 角色模型
// 对应数据库表: roles
type Role struct {
	ID          int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:角色ID[角色ID]"`
	Name        string    `json:"name" gorm:"uniqueIndex;size:50;not null;comment:角色名称[角色名称]"`
	Code        string    `json:"code" gorm:"uniqueIndex;size:50;not null;comment:角色编码[角色编码]"`
	Description string    `json:"description" gorm:"size:255;comment:角色描述[角色描述]"`
	IsSystem    int8      `json:"is_system" gorm:"type:tinyint;default:0;comment:是否系统角色[0:否;1:是]"`
	Status      int8      `json:"status" gorm:"type:tinyint;default:1;comment:角色状态[0:禁用;1:启用]"`
	SortOrder   int       `json:"sort_order" gorm:"default:0;comment:排序顺序[排序顺序]"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间[更新时间]"`
}

// TableName 指定表名
func (Role) TableName() string {
	return "roles"
}

// TableComment 返回表注释
func (Role) TableComment() string {
	return "角色表"
}

// Permission 权限模型
// 对应数据库表: permissions
type Permission struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:权限ID[权限ID]"`
	Name      string    `json:"name" gorm:"size:100;not null;comment:权限名称[权限名称]"`
	Code      string    `json:"code" gorm:"uniqueIndex;size:100;not null;comment:权限编码[权限编码]"`
	Type      string    `json:"type" gorm:"size:20;default:'menu';comment:权限类型[menu:菜单;button:按钮;api:API]"`
	ParentID  int64     `json:"parent_id" gorm:"default:0;comment:父权限ID[父权限ID]"`
	Path      string    `json:"path" gorm:"size:255;comment:权限路径[权限路径]"`
	Icon      string    `json:"icon" gorm:"size:100;comment:图标[图标]"`
	SortOrder int       `json:"sort_order" gorm:"default:0;comment:排序顺序[排序顺序]"`
	Status    int8      `json:"status" gorm:"type:tinyint;default:1;comment:权限状态[0:禁用;1:启用]"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间[更新时间]"`
}

// TableName 指定表名
func (Permission) TableName() string {
	return "permissions"
}

// TableComment 返回表注释
func (Permission) TableComment() string {
	return "权限表"
}

// UserRole 用户角色关联模型
// 对应数据库表: user_roles
type UserRole struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:ID[ID]"`
	UserID    int64     `json:"user_id" gorm:"not null;index;comment:用户ID[用户ID]"`
	RoleID    int64     `json:"role_id" gorm:"not null;index;comment:角色ID[角色ID]"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (UserRole) TableName() string {
	return "user_roles"
}

// TableComment 返回表注释
func (UserRole) TableComment() string {
	return "用户角色关联表"
}

// RolePermission 角色权限关联模型
// 对应数据库表: role_permissions
type RolePermission struct {
	ID           int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:ID[ID]"`
	RoleID       int64     `json:"role_id" gorm:"not null;index;comment:角色ID[角色ID]"`
	PermissionID int64     `json:"permission_id" gorm:"not null;index;comment:权限ID[权限ID]"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (RolePermission) TableName() string {
	return "role_permissions"
}

// TableComment 返回表注释
func (RolePermission) TableComment() string {
	return "角色权限关联表"
}

// UserToken 用户Token模型
// 对应数据库表: user_tokens
type UserToken struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:TokenID[TokenID]"`
	UserID    int64     `json:"user_id" gorm:"not null;index;comment:用户ID[用户ID]"`
	TokenType string    `json:"token_type" gorm:"size:20;default:'access';comment:Token类型[access:访问Token;refresh:刷新Token]"`
	Token     string    `json:"token" gorm:"size:500;not null;comment:Token值[Token值]"`
	DeviceID  *string   `json:"device_id" gorm:"size:100;comment:设备ID[设备ID]"`
	ExpiresAt time.Time `json:"expires_at" gorm:"not null;comment:过期时间[过期时间]"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (UserToken) TableName() string {
	return "user_tokens"
}

// TableComment 返回表注释
func (UserToken) TableComment() string {
	return "用户Token表"
}

// LoginLog 登录日志模型
// 对应数据库表: login_logs
type LoginLog struct {
	ID         int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:日志ID[日志ID]"`
	UserID     *int64    `json:"user_id" gorm:"index;comment:用户ID[用户ID]"`
	Username   *string   `json:"username" gorm:"size:50;comment:用户名[用户名]"`
	LoginType  string    `json:"login_type" gorm:"size:20;default:'password';comment:登录类型[password:密码;sms:短信;oauth:第三方]"`
	IPAddress  *string   `json:"ip_address" gorm:"size:45;comment:IP地址[IP地址]"`
	UserAgent  *string   `json:"user_agent" gorm:"size:500;comment:用户代理[用户代理]"`
	DeviceType *string   `json:"device_type" gorm:"size:20;comment:设备类型[设备类型]"`
	OS         *string   `json:"os" gorm:"size:50;comment:操作系统[操作系统]"`
	Browser    *string   `json:"browser" gorm:"size:50;comment:浏览器[浏览器]"`
	Location   *string   `json:"location" gorm:"size:100;comment:登录地点[登录地点]"`
	Status     int8      `json:"status" gorm:"type:tinyint;default:1;comment:登录状态[0:失败;1:成功]"`
	FailReason *string   `json:"fail_reason" gorm:"size:255;comment:失败原因[失败原因]"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (LoginLog) TableName() string {
	return "login_logs"
}

// TableComment 返回表注释
func (LoginLog) TableComment() string {
	return "登录日志表"
}

// VerificationCode 验证码模型
// 对应数据库表: verification_codes
type VerificationCode struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:验证码ID[验证码ID]"`
	Target    string    `json:"target" gorm:"size:100;not null;comment:验证目标[验证目标]"`
	Code      string    `json:"code" gorm:"size:10;not null;comment:验证码[验证码]"`
	Type      string    `json:"type" gorm:"size:20;not null;comment:验证类型[register:注册;login:登录;reset_password:重置密码;bind:绑定]"`
	Used      int8      `json:"used" gorm:"type:tinyint;default:0;comment:是否已使用[0:未使用;1:已使用]"`
	ExpiresAt time.Time `json:"expires_at" gorm:"not null;comment:过期时间[过期时间]"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (VerificationCode) TableName() string {
	return "verification_codes"
}

// TableComment 返回表注释
func (VerificationCode) TableComment() string {
	return "验证码表"
}

// OperationLog 操作日志模型
// 对应数据库表: operation_logs
type OperationLog struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement;comment:日志ID[日志ID]"`
	UserID    *int64    `json:"user_id" gorm:"index;comment:用户ID[用户ID]"`
	Username  *string   `json:"username" gorm:"size:50;comment:用户名[用户名]"`
	Module    *string   `json:"module" gorm:"size:50;comment:操作模块[操作模块]"`
	Action    *string   `json:"action" gorm:"size:50;comment:操作动作[操作动作]"`
	Method    *string   `json:"method" gorm:"size:10;comment:请求方法[请求方法]"`
	Path      *string   `json:"path" gorm:"size:255;comment:请求路径[请求路径]"`
	Params    *string   `json:"params" gorm:"type:json;comment:请求参数[请求参数]"`
	Result    *string   `json:"result" gorm:"type:json;comment:响应结果[响应结果]"`
	IPAddress *string   `json:"ip_address" gorm:"size:45;comment:IP地址[IP地址]"`
	UserAgent *string   `json:"user_agent" gorm:"size:500;comment:用户代理[用户代理]"`
	Duration  *int      `json:"duration" gorm:"comment:执行时长(毫秒)[执行时长]"`
	Status    int8      `json:"status" gorm:"type:tinyint;default:1;comment:操作状态[0:失败;1:成功]"`
	ErrorMsg  *string   `json:"error_msg" gorm:"type:text;comment:错误信息[错误信息]"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
}

// TableName 指定表名
func (OperationLog) TableName() string {
	return "operation_logs"
}

// TableComment 返回表注释
func (OperationLog) TableComment() string {
	return "操作日志表"
}

// OAuthBinding 第三方登录绑定模型
// 对应数据库表: oauth_bindings
type OAuthBinding struct {
	ID           int64      `json:"id" gorm:"primaryKey;autoIncrement;comment:绑定ID[绑定ID]"`
	UserID       int64      `json:"user_id" gorm:"not null;index;comment:用户ID[用户ID]"`
	Provider     string     `json:"provider" gorm:"size:30;not null;comment:提供商[wechat:微信;qq:QQ;weibo:微博;github:GitHub]"`
	OpenID       string     `json:"open_id" gorm:"size:100;not null;comment:OpenID[OpenID]"`
	UnionID      *string    `json:"union_id" gorm:"size:100;comment:UnionID[UnionID]"`
	AccessToken  *string    `json:"-" gorm:"size:500;comment:访问令牌[访问令牌]"`
	RefreshToken *string    `json:"-" gorm:"size:500;comment:刷新令牌[刷新令牌]"`
	ExpiresAt    *time.Time `json:"expires_at" gorm:"comment:令牌过期时间[令牌过期时间]"`
	Nickname     *string    `json:"nickname" gorm:"size:50;comment:第三方昵称[第三方昵称]"`
	AvatarURL    *string    `json:"avatar_url" gorm:"size:500;comment:头像URL[头像URL]"`
	RawData      *string    `json:"raw_data" gorm:"type:json;comment:原始数据[原始数据]"`
	CreatedAt    time.Time  `json:"created_at" gorm:"autoCreateTime;comment:创建时间[创建时间]"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"autoUpdateTime;comment:更新时间[更新时间]"`
}

// TableName 指定表名
func (OAuthBinding) TableName() string {
	return "oauth_bindings"
}

// TableComment 返回表注释
func (OAuthBinding) TableComment() string {
	return "第三方登录绑定表"
}

// Commentable 定义可注释接口
type Commentable interface {
	TableName() string
	TableComment() string
}

// 常量定义
const (
	UserStatusDisabled int8 = iota // 禁用
	UserStatusNormal               // 正常
	UserStatusLocked               // 锁定

	RoleStatusDisabled int8 = 0 // 禁用
	RoleStatusEnabled  int8 = 1 // 启用

	PermissionTypeMenu   = "menu"   // 菜单权限
	PermissionTypeButton = "button" // 按钮权限
	PermissionTypeAPI    = "api"    // API权限

	TokenTypeAccess  = "access"  // 访问Token
	TokenTypeRefresh = "refresh" // 刷新Token

	LoginTypePassword = "password" // 密码登录
	LoginTypeSMS      = "sms"      // 短信登录
	LoginTypeOAuth    = "oauth"    // 第三方登录

	VerificationTypeRegister  = "register"       // 注册验证
	VerificationTypeLogin     = "login"          // 登录验证
	VerificationTypeResetPass = "reset_password" // 重置密码
	VerificationTypeBind      = "bind"           // 绑定验证
)

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Username         string `json:"username" memo:"用户名" require:"1"`
	Password         string `json:"password" memo:"密码" require:"1"`
	LoginType        string `json:"login_type" memo:"登录类型" range:"password;sms;oauth"`
	DeviceID         string `json:"device_id" memo:"设备ID"`
	VerificationCode string `json:"verification_code" memo:"验证码"`
}

// RegisterRequest 注册请求结构体
type RegisterRequest struct {
	Username         string `json:"username" memo:"用户名" require:"1"`
	Password         string `json:"password" memo:"密码" require:"1"`
	Email            string `json:"email" memo:"邮箱" validator:"Mail"`
	Phone            string `json:"phone" memo:"手机号"`
	Nickname         string `json:"nickname" memo:"昵称"`
	VerificationCode string `json:"verification_code" memo:"验证码"`
}

// TokenResponse Token响应结构体
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

// UserInfoResponse 用户信息响应结构体
type UserInfoResponse struct {
	ID            int64      `json:"id"`
	UUID          string     `json:"uuid"`
	Username      string     `json:"username"`
	Email         *string    `json:"email"`
	Phone         *string    `json:"phone"`
	Nickname      *string    `json:"nickname"`
	Status        int8       `json:"status"`
	EmailVerified int8       `json:"email_verified"`
	PhoneVerified int8       `json:"phone_verified"`
	LastLoginAt   *time.Time `json:"last_login_at"`
	LoginCount    int64      `json:"login_count"`
	Roles         []string   `json:"roles"`
	Permissions   []string   `json:"permissions"`
	CreatedAt     time.Time  `json:"created_at"`
}

// PermissionInfo 权限信息结构体
type PermissionInfo struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Code      string            `json:"code"`
	Type      string            `json:"type"`
	ParentID  int64             `json:"parent_id"`
	Path      string            `json:"path"`
	Icon      string            `json:"icon"`
	SortOrder int               `json:"sort_order"`
	Children  []*PermissionInfo `json:"children"`
}
