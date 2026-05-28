package account

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server"
)

// StandaloneService 将 Account SDK 封装为独立 HTTP 服务。
// 自动初始化 Manager、注册所有 REST 路由，并启动后台清理和审计写入任务。
type StandaloneService struct {
	m       *Manager
	srv     *server.Server
	cleanup context.CancelFunc
	cfg     StandaloneConfig
}

// RouteModule 路由模块标识。每个模块对应一组逻辑相关的端点，可通过
// StandaloneConfig.Profile / EnabledModules / DisabledModules 独立开关。
type RouteModule string

const (
	ModuleHealth       RouteModule = "health"       // GET /health
	ModuleAuth         RouteModule = "auth"         // /auth/* 登录注册等
	ModuleVerification RouteModule = "verification" // /verifications/* 验证码下发
	ModuleUser         RouteModule = "user"         // /users/me/* 自助管理
	ModuleMFA          RouteModule = "mfa"          // /mfa/* TOTP & step-up
	ModuleSession      RouteModule = "session"      // /sessions/* 自助会话
	ModuleOrg          RouteModule = "org"          // /orgs/* 只读组织
	ModuleIdP          RouteModule = "idp"          // /idp/clients/* OIDC 客户端管理
	ModuleOIDC         RouteModule = "oidc"         // /.well-known/* /oauth/*
	ModulePasskey      RouteModule = "passkey"      // /passkey/*
	ModuleAudit        RouteModule = "audit"        // /audit/* 自助查询
	ModuleAdmin        RouteModule = "admin"        // /admin/* 后台管理端
)

// RouteProfile 部署形态预设。允许业务方一行配置切出"对外/管理端"两套服务。
type RouteProfile string

const (
	// ProfileFull 默认形态：注册全部路由模块（与历史行为一致）。
	ProfileFull RouteProfile = "full"
	// ProfilePublic 面向 C 端/前端：开放认证、用户自助、会话、MFA、组织只读、
	// OIDC、Passkey 与自助审计；关闭 admin、idp 客户端管理。
	ProfilePublic RouteProfile = "public"
	// ProfileAdmin 仅管理端：开放健康检查、登录登出、会话与用户自助接口及 /admin/*。
	// 推荐部署在内网/管理 VPC，与 ProfilePublic 进程分离。
	ProfileAdmin RouteProfile = "admin"
)

// StandaloneConfig 独立服务模式的配置。
type StandaloneConfig struct {
	// ListenAddr 监听地址（仅端口部分，如 "8080"），默认 "8080"
	ListenAddr string
	// CleanupInterval 清理间隔，默认 10 分钟
	CleanupInterval time.Duration
	// Profile 路由形态预设，未设置时取 ProfileFull。
	Profile RouteProfile
	// EnabledModules 在 Profile 之上额外启用的模块（取并集）。
	EnabledModules []RouteModule
	// DisabledModules 在 Profile 之上额外禁用的模块（取差集）。
	// 与 EnabledModules 同时出现时，禁用优先级更高。
	DisabledModules []RouteModule
}

// profileBaseModules 返回某个 Profile 内置启用的模块集合。
func profileBaseModules(p RouteProfile) map[RouteModule]bool {
	switch p {
	case ProfileAdmin:
		// 内部管理端：管理员仍然要走登录/登出/刷新 token，并能查看自己的会话。
		return map[RouteModule]bool{
			ModuleHealth:  true,
			ModuleAuth:    true,
			ModuleUser:    true,
			ModuleSession: true,
			ModuleAdmin:   true,
		}
	case ProfilePublic:
		return map[RouteModule]bool{
			ModuleHealth:       true,
			ModuleAuth:         true,
			ModuleVerification: true,
			ModuleUser:         true,
			ModuleMFA:          true,
			ModuleSession:      true,
			ModuleOrg:          true,
			ModuleOIDC:         true,
			ModulePasskey:      true,
			ModuleAudit:        true,
			// 默认不暴露 ModuleAdmin / ModuleIdP
		}
	case ProfileFull, "":
		fallthrough
	default:
		return map[RouteModule]bool{
			ModuleHealth:       true,
			ModuleAuth:         true,
			ModuleVerification: true,
			ModuleUser:         true,
			ModuleMFA:          true,
			ModuleSession:      true,
			ModuleOrg:          true,
			ModuleIdP:          true,
			ModuleOIDC:         true,
			ModulePasskey:      true,
			ModuleAudit:        true,
			ModuleAdmin:        true,
		}
	}
}

// resolveModules 计算最终启用集合：profile ∪ EnabledModules \ DisabledModules。
func (c StandaloneConfig) resolveModules() map[RouteModule]bool {
	enabled := profileBaseModules(c.Profile)
	for _, m := range c.EnabledModules {
		enabled[m] = true
	}
	for _, m := range c.DisabledModules {
		delete(enabled, m)
	}
	return enabled
}

// NewStandaloneService 从默认框架配置创建独立服务。
// Profile 默认从配置项 Account.Standalone.Profile 读取（full/public/admin），
// 未配置时回退到 ProfileFull，保持历史行为。
func NewStandaloneService() (*StandaloneService, error) {
	m, err := NewFramework()
	if err != nil {
		return nil, fmt.Errorf("create account manager: %w", err)
	}
	scfg := StandaloneConfig{
		Profile: RouteProfile(gaia.GetSafeConfStringWithDefault("Account.Standalone.Profile", string(ProfileFull))),
	}
	return NewStandaloneServiceWithConfig(m.cfg, scfg)
}

// NewStandaloneServiceWithConfig 使用自定义配置创建独立服务。
func NewStandaloneServiceWithConfig(cfg Config, scfg StandaloneConfig) (*StandaloneService, error) {
	m, err := New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create account manager: %w", err)
	}
	if scfg.ListenAddr == "" {
		scfg.ListenAddr = "8080"
	}
	if scfg.CleanupInterval <= 0 {
		scfg.CleanupInterval = 10 * time.Minute
	}
	s := &StandaloneService{
		m:   m,
		srv: server.NewAppWithPort(scfg.ListenAddr),
		cfg: scfg,
	}
	return s, nil
}

// Run 启动独立服务。阻塞直到 context 被取消或服务异常退出。
func (s *StandaloneService) Run(ctx context.Context) error {
	if err := s.m.Bootstrap(ctx); err != nil {
		return fmt.Errorf("account bootstrap: %w", err)
	}

	// 后台清理任务
	cleanupCtx, cancel := context.WithCancel(ctx)
	s.cleanup = cancel
	go func() {
		ticker := time.NewTicker(s.cfg.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				_ = s.m.Cleanup(cleanupCtx)
			}
		}
	}()

	if s.m.cfg.Audit.AsyncWrite {
		s.m.StartAuditWriter(ctx)
	}

	s.registerRoutes()

	gaia.InfoF("[account] standalone service starting on port %s", s.cfg.ListenAddr)
	s.srv.Run()
	return nil
}

// Manager 返回底层的 Manager，用于直接调用 SDK 方法。
func (s *StandaloneService) Manager() *Manager {
	return s.m
}

// registerRoutes 按 StandaloneConfig 配置注册启用的路由模块。
// 通过 Profile + EnabledModules / DisabledModules 控制最终启用集合，
// 默认 ProfileFull 与历史行为一致。
func (s *StandaloneService) registerRoutes() {
	r := s.srv.Group("/api/v1/account")
	enabled := s.cfg.resolveModules()

	if enabled[ModuleHealth] {
		r.GET("/health", s.handler(s.handleHealth))
	}
	if enabled[ModuleAuth] {
		s.registerAuthRoutes(r)
	}
	if enabled[ModuleVerification] {
		s.registerVerificationRoutes(r)
	}
	if enabled[ModuleUser] {
		s.registerUserRoutes(r)
	}
	if enabled[ModuleMFA] {
		s.registerMFARoutes(r)
	}
	if enabled[ModuleSession] {
		s.registerSessionRoutes(r)
	}
	if enabled[ModuleOrg] {
		s.registerOrgRoutes(r)
	}
	if enabled[ModuleIdP] {
		s.registerIdPRoutes(r)
	}
	if enabled[ModuleOIDC] {
		s.registerOIDCEndpointRoutes(r)
	}
	if enabled[ModulePasskey] {
		s.registerPasskeyRoutes(r)
	}
	if enabled[ModuleAudit] {
		s.registerSelfAuditRoutes(r)
	}
	if enabled[ModuleAdmin] {
		s.registerAdminRoutes(r)
	}

	gaia.InfoF("[account] standalone routes registered: profile=%s modules=%v",
		nonEmptyProfile(s.cfg.Profile), modulesToSlice(enabled))
}

func nonEmptyProfile(p RouteProfile) RouteProfile {
	if p == "" {
		return ProfileFull
	}
	return p
}

func modulesToSlice(set map[RouteModule]bool) []RouteModule {
	out := make([]RouteModule, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	return out
}

// registerAuthRoutes 注册 /auth/* 端点。
func (s *StandaloneService) registerAuthRoutes(r *route.RouterGroup) {
	auth := r.Group("/auth")
	auth.POST("/register", s.handler(s.handleRegister))
	auth.POST("/login", s.handler(s.handleLogin))
	auth.POST("/login/code", s.handler(s.handleLoginWithCode))
	auth.POST("/login/oauth/:provider", s.handler(s.handleOAuthLogin))
	auth.POST("/bind-phone-and-login", s.handler(s.handleBindPhoneAndLogin))
	auth.POST("/mfa/complete", s.handler(s.handleCompleteMFA))
	auth.POST("/mfa/code", s.m.Middleware().Authenticate(), s.handler(s.handleRequestMFACode))
	auth.POST("/forgot-password/start", s.handler(s.handleForgotPasswordStart))
	auth.POST("/forgot-password/complete", s.handler(s.handleForgotPasswordComplete))
	auth.POST("/token/refresh", s.handler(s.handleRefresh))
	auth.POST("/logout", s.m.Middleware().Authenticate(), s.handler(s.handleLogout))
}

// registerVerificationRoutes 注册 /verifications/* 端点。
func (s *StandaloneService) registerVerificationRoutes(r *route.RouterGroup) {
	verify := r.Group("/verifications")
	verify.POST("/send", s.handler(s.handleSendVerificationCode))
	verify.POST("/verify", s.handler(s.handleVerifyCode))
}

// registerUserRoutes 注册 /users/* 端点（自助）。
func (s *StandaloneService) registerUserRoutes(r *route.RouterGroup) {
	user := r.Group("/users")
	user.Use(s.m.Middleware().Authenticate())
	user.GET("/me", s.handler(s.handleGetCurrentUser))
	user.PUT("/me", s.handler(s.handleUpdateProfile))
	user.PUT("/password", s.handler(s.handleChangePassword))
	user.DELETE("/me", s.handler(s.handleDeleteAccount))
	user.GET("/me/permissions", s.handler(s.handleGetMyPermissions))
	user.POST("/me/bind-email", s.handler(s.handleBindEmail))
	user.POST("/me/bind-phone", s.handler(s.handleBindPhone))
	user.GET("/me/oauth-accounts", s.handler(s.handleListMyOAuthAccounts))
	user.POST("/me/oauth-accounts/:provider/bind", s.handler(s.handleBindOAuthAccount))
	user.DELETE("/me/oauth-accounts/:provider", s.handler(s.handleUnbindOAuthAccount))
	user.GET("/me/orgs", s.handler(s.handleListMyOrgs))
	user.GET("/me/api-tokens", s.handler(s.handleListAPITokens))
	user.POST("/me/api-tokens", s.handler(s.handleCreateAPIToken))
	user.DELETE("/me/api-tokens/:id", s.handler(s.handleRevokeAPIToken))
	user.GET("/me/consents", s.handler(s.handleListConsents))
	user.POST("/me/consents", s.handler(s.handleRecordConsent))
}

// registerMFARoutes 注册 /mfa/* 端点。
func (s *StandaloneService) registerMFARoutes(r *route.RouterGroup) {
	mfa := r.Group("/mfa")
	mfa.Use(s.m.Middleware().Authenticate())
	mfa.POST("/totp/setup", s.handler(s.handleSetupTOTP))
	mfa.POST("/totp/verify", s.handler(s.handleVerifyTOTP))
	mfa.DELETE("/totp", s.handler(s.handleDisableTOTP))
	mfa.POST("/step-up/start", s.handler(s.handleStepUpStart))
	mfa.POST("/step-up/complete", s.handler(s.handleStepUpComplete))
}

// registerSessionRoutes 注册 /sessions/* 端点（用户自助）。
func (s *StandaloneService) registerSessionRoutes(r *route.RouterGroup) {
	session := r.Group("/sessions")
	session.Use(s.m.Middleware().Authenticate())
	session.GET("", s.handler(s.handleListSessions))
	session.DELETE("/:id", s.handler(s.handleRevokeSession))
	session.POST("/revoke-others", s.handler(s.handleRevokeOtherSessions))
}

// registerOrgRoutes 注册 /orgs/* 只读端点。
func (s *StandaloneService) registerOrgRoutes(r *route.RouterGroup) {
	orgs := r.Group("/orgs")
	orgs.Use(s.m.Middleware().Authenticate())
	orgs.GET("", s.handler(s.handleListOrgs))
	orgs.GET("/tree", s.handler(s.handleGetOrgTree))
	orgs.GET("/:id", s.handler(s.handleGetOrg))
	orgs.GET("/:id/ancestors", s.handler(s.handleListOrgAncestors))
	orgs.GET("/:id/members", s.handler(s.handleListOrgMembers))
}

// registerIdPRoutes 注册 /idp/clients/* 端点（OIDC 客户端管理）。
func (s *StandaloneService) registerIdPRoutes(r *route.RouterGroup) {
	idp := r.Group("/idp/clients")
	idp.POST("", s.handler(s.handleRegisterClient))
	idp.GET("", s.handler(s.handleListClients))
	idp.GET("/:client_id", s.handler(s.handleGetClient))
	idp.DELETE("/:client_id", s.handler(s.handleDeleteClient))
	idp.POST("/token", s.handler(s.handleTokenEndpoint))
}

// registerOIDCEndpointRoutes 注册标准 OIDC 端点（well-known/jwks/oauth/*）。
func (s *StandaloneService) registerOIDCEndpointRoutes(r *route.RouterGroup) {
	r.GET("/.well-known/openid-configuration", s.handler(s.handleOpenIDConfig))
	r.GET("/oauth/certs", s.handler(s.handleJWKS))
	r.GET("/oauth/authorize", s.m.Middleware().Authenticate(), s.handler(s.handleOAuthAuthorize))
	r.GET("/oauth/userinfo", s.m.Middleware().Authenticate(), s.handler(s.handleOAuthUserinfo))
	r.GET("/oauth/authorized-apps", s.m.Middleware().Authenticate(), s.handler(s.handleListAuthorizedApps))
	r.DELETE("/oauth/authorized-apps/:client_id", s.m.Middleware().Authenticate(), s.handler(s.handleRevokeAuthorizedApp))
	r.POST("/oauth/revoke", s.handler(s.handleOAuthRevoke))
	r.POST("/oauth/introspect", s.handler(s.handleOAuthIntrospect))
}

// registerPasskeyRoutes 注册 /passkey/* 端点（WebAuthn）。
func (s *StandaloneService) registerPasskeyRoutes(r *route.RouterGroup) {
	passkey := r.Group("/passkey")
	passkey.POST("/register/start", s.m.Middleware().Authenticate(), s.handler(s.handlePasskeyRegisterStart))
	passkey.POST("/register/complete", s.m.Middleware().Authenticate(), s.handler(s.handlePasskeyRegisterComplete))
	passkey.POST("/auth/start", s.handler(s.handlePasskeyAuthStart))
	passkey.POST("/auth/complete", s.handler(s.handlePasskeyAuthComplete))
	passkey.GET("/credentials", s.m.Middleware().Authenticate(), s.handler(s.handleListPasskeyCredentials))
	passkey.DELETE("/credentials/:id", s.m.Middleware().Authenticate(), s.handler(s.handleDeletePasskeyCredential))
}

// registerSelfAuditRoutes 注册 /audit/* 自助审计查询端点（仅本人）。
func (s *StandaloneService) registerSelfAuditRoutes(r *route.RouterGroup) {
	audit := r.Group("/audit")
	audit.Use(s.m.Middleware().Authenticate())
	audit.GET("", s.handler(s.handleQueryAudit))
	audit.GET("/archived", s.handler(s.handleQueryArchivedAudit))
	audit.POST("/archive", s.handler(s.handleArchiveAuditLogs))
}

// handler 将业务处理函数包装为 Hertz handler。
func (s *StandaloneService) handler(fn func(server.Request) (any, error)) app.HandlerFunc {
	return server.MakeHandler(fn)
}

// ===== 处理函数 =====

func (s *StandaloneService) handleHealth(req server.Request) (any, error) {
	return map[string]string{"status": "ok"}, nil
}

func (s *StandaloneService) handleRegister(req server.Request) (any, error) {
	var body struct {
		TenantID                     string `json:"tenant_id"`
		Username                     string `json:"username"`
		Password                     string `json:"password"`
		Email                        string `json:"email"`
		EmailVerificationChallengeID string `json:"email_verification_challenge_id"`
		EmailVerificationCode        string `json:"email_verification_code"`
		Phone                        string `json:"phone"`
		PhoneVerificationChallengeID string `json:"phone_verification_challenge_id"`
		PhoneVerificationCode        string `json:"phone_verification_code"`
		Nickname                     string `json:"nickname"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().Register(req.TraceContext, RegisterRequest{
		TenantID:                     body.TenantID,
		Username:                     body.Username,
		Password:                     body.Password,
		Email:                        body.Email,
		EmailVerificationChallengeID: body.EmailVerificationChallengeID,
		EmailVerificationCode:        body.EmailVerificationCode,
		Phone:                        body.Phone,
		PhoneVerificationChallengeID: body.PhoneVerificationChallengeID,
		PhoneVerificationCode:        body.PhoneVerificationCode,
		Nickname:                     body.Nickname,
		DeviceID:                     string(req.C().GetHeader("X-Device-ID")),
		IP:                           req.C().ClientIP(),
		UserAgent:                    string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleLogin(req server.Request) (any, error) {
	var body struct {
		Identifier     string `json:"identifier"`
		IdentifierType string `json:"identifier_type"`
		Password       string `json:"password"`
		TenantID       string `json:"tenant_id"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().Login(req.TraceContext, LoginRequest{
		TenantID:       body.TenantID,
		Identifier:     body.Identifier,
		IdentifierType: body.IdentifierType,
		Password:       body.Password,
		DeviceID:       string(req.C().GetHeader("X-Device-ID")),
		IP:             req.C().ClientIP(),
		UserAgent:      string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleRefresh(req server.Request) (any, error) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
		TenantID     string `json:"tenant_id"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().Refresh(req.TraceContext, RefreshRequest{
		RefreshToken: body.RefreshToken,
		DeviceID:     string(req.C().GetHeader("X-Device-ID")),
		IP:           req.C().ClientIP(),
		UserAgent:    string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleLogout(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	if req.GetUrlQuery("all") == "true" {
		return nil, s.m.Auth().LogoutAll(req.TraceContext, p.UserID)
	}
	accessToken := bearerToken(string(req.C().GetHeader("Authorization")))
	return nil, s.m.Auth().Logout(req.TraceContext, LogoutRequest{
		AccessToken: accessToken,
		SessionID:   p.SessionID,
	})
}

func (s *StandaloneService) handleGetCurrentUser(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Users().GetCurrent(req.TraceContext, p.UserID)
}

func (s *StandaloneService) handleUpdateProfile(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Users().UpdateProfile(req.TraceContext, UpdateProfileRequest{
		UserID:    p.UserID,
		Nickname:  body.Nickname,
		AvatarURL: body.AvatarURL,
	})
}

func (s *StandaloneService) handleChangePassword(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Users().ChangePassword(req.TraceContext, ChangePasswordRequest{
		UserID:      p.UserID,
		OldPassword: body.OldPassword,
		NewPassword: body.NewPassword,
		Principal:   p,
	})
}

func (s *StandaloneService) handleDeleteAccount(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.Users().DeleteAccount(req.TraceContext, DeleteAccountRequest{UserID: p.UserID})
}

func (s *StandaloneService) handleListSessions(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Sessions().List(req.TraceContext, p.TenantID, p.UserID, p.SessionID)
}

func (s *StandaloneService) handleRevokeSession(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.Sessions().Revoke(req.TraceContext, p.TenantID, p.UserID, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleRegisterClient(req server.Request) (any, error) {
	var body struct {
		Name         string   `json:"name"`
		RedirectURIs []string `json:"redirect_uris"`
		Scopes       []string `json:"scopes"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.IdP().RegisterClient(req.TraceContext, IdpClientRequest{
		Name:         body.Name,
		RedirectURIs: body.RedirectURIs,
		Scopes:       body.Scopes,
	})
}

func (s *StandaloneService) handleGetClient(req server.Request) (any, error) {
	return s.m.IdP().GetClient(req.TraceContext, req.GetUrlParam("client_id"))
}

func (s *StandaloneService) handleListClients(req server.Request) (any, error) {
	return s.m.IdP().ListClients(req.TraceContext, req.GetUrlQuery("tenant_id"))
}

func (s *StandaloneService) handleDeleteClient(req server.Request) (any, error) {
	return nil, s.m.IdP().DeleteClient(req.TraceContext, req.GetUrlParam("client_id"))
}

func (s *StandaloneService) handleTokenEndpoint(req server.Request) (any, error) {
	var body struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		CodeVerifier string `json:"code_verifier"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.IdP().Token(req.TraceContext, TokenRequest{
		GrantType:    body.GrantType,
		Code:         body.Code,
		RedirectURI:  body.RedirectURI,
		ClientID:     body.ClientID,
		ClientSecret: body.ClientSecret,
		CodeVerifier: body.CodeVerifier,
		RefreshToken: body.RefreshToken,
		Scope:        body.Scope,
	})
}

func (s *StandaloneService) handleOpenIDConfig(req server.Request) (any, error) {
	issuer := fmt.Sprintf("%s://%s", req.C().URI().Scheme(), string(req.C().Host()))
	return s.m.IdP().OpenIDConfig(req.TraceContext, issuer+"/api/v1/account"), nil
}

func (s *StandaloneService) handleJWKS(req server.Request) (any, error) {
	return s.m.IdP().JWKS(req.TraceContext)
}

func (s *StandaloneService) handlePasskeyRegisterStart(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Passkey().StartPasskeyRegistration(req.TraceContext, p.UserID, p.TenantID, p.Username)
}

func (s *StandaloneService) handlePasskeyRegisterComplete(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body PasskeyRegistrationResponse
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Passkey().CompletePasskeyRegistration(req.TraceContext, p.UserID, body)
}

func (s *StandaloneService) handlePasskeyAuthStart(req server.Request) (any, error) {
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Passkey().StartPasskeyAuthentication(req.TraceContext, body.UserID)
}

func (s *StandaloneService) handlePasskeyAuthComplete(req server.Request) (any, error) {
	var body PasskeyAuthenticationResponse
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Passkey().CompletePasskeyAuthentication(req.TraceContext, body.UserHandle, body)
}

func (s *StandaloneService) handleListPasskeyCredentials(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Passkey().ListCredentials(req.TraceContext, p.UserID)
}

func (s *StandaloneService) handleDeletePasskeyCredential(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.Passkey().DeleteCredential(req.TraceContext, p.UserID, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleQueryAudit(req server.Request) (any, error) {
	var body struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Event    string `json:"event"`
		Status   string `json:"status"`
		Keyword  string `json:"keyword"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	req.BindJson(&body)
	return s.m.Audit().Query(req.TraceContext, AuditQueryRequest{
		TenantID: body.TenantID,
		UserID:   body.UserID,
		Event:    body.Event,
		Status:   body.Status,
		Keyword:  body.Keyword,
		Page:     body.Page,
		PageSize: body.PageSize,
	})
}

func (s *StandaloneService) handleQueryArchivedAudit(req server.Request) (any, error) {
	var body struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Event    string `json:"event"`
		Status   string `json:"status"`
		Keyword  string `json:"keyword"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	req.BindJson(&body)
	return s.m.Audit().QueryArchived(req.TraceContext, AuditQueryRequest{
		TenantID: body.TenantID,
		UserID:   body.UserID,
		Event:    body.Event,
		Status:   body.Status,
		Keyword:  body.Keyword,
		Page:     body.Page,
		PageSize: body.PageSize,
	})
}

func (s *StandaloneService) handleArchiveAuditLogs(req server.Request) (any, error) {
	var body struct {
		BeforeDays int `json:"before_days"`
		BatchSize  int `json:"batch_size"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.BeforeDays <= 0 {
		body.BeforeDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -body.BeforeDays)
	n, err := s.m.Audit().ArchiveOldLogs(req.TraceContext, cutoff, body.BatchSize)
	return map[string]int64{"archived": n}, err
}

// ===== 新增：验证码 =====

func (s *StandaloneService) handleSendVerificationCode(req server.Request) (any, error) {
	var body struct {
		TenantID string `json:"tenant_id"`
		Channel  string `json:"channel"`
		Target   string `json:"target"`
		Purpose  string `json:"purpose"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Verification().Send(req.TraceContext, SendVerificationRequest{
		TenantID: body.TenantID,
		Channel:  body.Channel,
		Target:   body.Target,
		Purpose:  body.Purpose,
		IP:       req.C().ClientIP(),
	})
}

func (s *StandaloneService) handleVerifyCode(req server.Request) (any, error) {
	var body struct {
		TenantID    string `json:"tenant_id"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		Purpose     string `json:"purpose"`
		Channel     string `json:"channel"`
		Target      string `json:"target"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Verification().Verify(req.TraceContext, VerifyCodeRequest{
		TenantID:    body.TenantID,
		ChallengeID: body.ChallengeID,
		Code:        body.Code,
		Purpose:     body.Purpose,
		Channel:     body.Channel,
		Target:      body.Target,
	})
}

// ===== 新增：登录扩展 =====

func (s *StandaloneService) handleLoginWithCode(req server.Request) (any, error) {
	var body struct {
		TenantID       string `json:"tenant_id"`
		Identifier     string `json:"identifier"`
		IdentifierType string `json:"identifier_type"`
		ChallengeID    string `json:"challenge_id"`
		Code           string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().LoginWithVerificationCode(req.TraceContext, LoginWithVerificationCodeRequest{
		TenantID:       body.TenantID,
		Identifier:     body.Identifier,
		IdentifierType: body.IdentifierType,
		ChallengeID:    body.ChallengeID,
		Code:           body.Code,
		DeviceID:       string(req.C().GetHeader("X-Device-ID")),
		IP:             req.C().ClientIP(),
		UserAgent:      string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleOAuthLogin(req server.Request) (any, error) {
	var body struct {
		TenantID     string `json:"tenant_id"`
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		State        string `json:"state"`
		Nonce        string `json:"nonce"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.OAuth().Login(req.TraceContext, OAuthLoginRequest{
		TenantID:     body.TenantID,
		Provider:     req.GetUrlParam("provider"),
		Code:         body.Code,
		RedirectURI:  body.RedirectURI,
		State:        body.State,
		Nonce:        body.Nonce,
		CodeVerifier: body.CodeVerifier,
		DeviceID:     string(req.C().GetHeader("X-Device-ID")),
		IP:           req.C().ClientIP(),
		UserAgent:    string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleBindPhoneAndLogin(req server.Request) (any, error) {
	var body struct {
		TenantID       string `json:"tenant_id"`
		Identifier     string `json:"identifier"`
		IdentifierType string `json:"identifier_type"`
		Password       string `json:"password"`
		Phone          string `json:"phone"`
		ChallengeID    string `json:"challenge_id"`
		Code           string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().BindPhoneAndLogin(req.TraceContext, BindPhoneRequest{
		TenantID:       body.TenantID,
		Identifier:     body.Identifier,
		IdentifierType: body.IdentifierType,
		Password:       body.Password,
		Phone:          body.Phone,
		ChallengeID:    body.ChallengeID,
		Code:           body.Code,
		DeviceID:       string(req.C().GetHeader("X-Device-ID")),
		IP:             req.C().ClientIP(),
		UserAgent:      string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleCompleteMFA(req server.Request) (any, error) {
	var body struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Auth().CompleteMFA(req.TraceContext, body.ChallengeID, body.Code, CompleteMFARequest{
		DeviceID:  string(req.C().GetHeader("X-Device-ID")),
		IP:        req.C().ClientIP(),
		UserAgent: string(req.C().UserAgent()),
	})
}

func (s *StandaloneService) handleRequestMFACode(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Channel string `json:"channel"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.mfa.RequestMFACode(req.TraceContext, p.TenantID, p.UserID, body.Channel, req.C().ClientIP())
}

// ===== 新增：忘记密码 =====

func (s *StandaloneService) handleForgotPasswordStart(req server.Request) (any, error) {
	var body struct {
		TenantID   string `json:"tenant_id"`
		Username   string `json:"username"`
		Identifier string `json:"identifier"`
		Channel    string `json:"channel"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Users().StartResetPassword(req.TraceContext, StartResetPasswordRequest{
		TenantID:   body.TenantID,
		Username:   body.Username,
		Identifier: body.Identifier,
		Channel:    body.Channel,
	})
}

func (s *StandaloneService) handleForgotPasswordComplete(req server.Request) (any, error) {
	var body struct {
		TenantID    string `json:"tenant_id"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Users().CompleteResetPassword(req.TraceContext, CompleteResetPasswordRequest{
		TenantID:    body.TenantID,
		ChallengeID: body.ChallengeID,
		Code:        body.Code,
		NewPassword: body.NewPassword,
	})
}

// ===== 新增：用户敏感操作 =====

func (s *StandaloneService) handleGetMyPermissions(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	perms, err := s.m.Authorizer().GetEffectivePermissionsForPrincipal(req.TraceContext, p)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"user_id":     p.UserID,
		"tenant_id":   p.TenantID,
		"roles":       p.Roles,
		"permissions": perms,
	}, nil
}

func (s *StandaloneService) handleBindEmail(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Email       string `json:"email"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Users().BindEmail(req.TraceContext, BindEmailRequest{
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		Email:       body.Email,
		ChallengeID: body.ChallengeID,
		Code:        body.Code,
	})
}

func (s *StandaloneService) handleBindPhone(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Phone       string `json:"phone"`
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Users().BindPhone(req.TraceContext, p.TenantID, p.UserID, body.Phone, body.ChallengeID, body.Code)
}

// ===== 新增：第三方账号绑定 =====

func (s *StandaloneService) handleListMyOAuthAccounts(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.OAuth().GetOAuthAccounts(req.TraceContext, p.TenantID, p.UserID)
}

func (s *StandaloneService) handleBindOAuthAccount(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Code         string `json:"code"`
		RedirectURI  string `json:"redirect_uri"`
		State        string `json:"state"`
		Nonce        string `json:"nonce"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.OAuth().Bind(req.TraceContext, OAuthBindRequest{
		TenantID:     p.TenantID,
		UserID:       p.UserID,
		Provider:     req.GetUrlParam("provider"),
		Code:         body.Code,
		RedirectURI:  body.RedirectURI,
		State:        body.State,
		Nonce:        body.Nonce,
		CodeVerifier: body.CodeVerifier,
	})
}

func (s *StandaloneService) handleUnbindOAuthAccount(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.OAuth().Unbind(req.TraceContext, p.TenantID, p.UserID, req.GetUrlParam("provider"))
}

// ===== 新增：MFA TOTP =====

func (s *StandaloneService) handleSetupTOTP(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	// email 用于 TOTP issuer 显示
	user, err := s.m.Users().GetByID(req.TraceContext, p.UserID)
	if err != nil {
		return nil, err
	}
	return s.m.mfa.SetupTOTP(req.TraceContext, p.TenantID, p.UserID, user.Email)
}

func (s *StandaloneService) handleVerifyTOTP(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	ok, err := s.m.mfa.VerifyTOTP(req.TraceContext, TOTPVerifyRequest{
		TenantID: p.TenantID,
		UserID:   p.UserID,
		Code:     body.Code,
	})
	return map[string]bool{"ok": ok}, err
}

func (s *StandaloneService) handleDisableTOTP(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.mfa.DisableTOTP(req.TraceContext, p.TenantID, p.UserID, p)
}

// ===== 新增：Step-up MFA =====

func (s *StandaloneService) handleStepUpStart(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	challengeID, err := s.m.Auth().RequestStepUp(req.TraceContext, p)
	if err != nil {
		return nil, err
	}
	return map[string]string{"challenge_id": challengeID}, nil
}

func (s *StandaloneService) handleStepUpComplete(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Auth().CompleteStepUp(req.TraceContext, p, body.ChallengeID, body.Code)
}

// ===== 新增：会话管理 =====

func (s *StandaloneService) handleRevokeOtherSessions(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	n, err := s.m.Sessions().RevokeOther(req.TraceContext, p.TenantID, p.UserID, p.SessionID)
	if err != nil {
		return nil, err
	}
	return map[string]int64{"revoked": n}, nil
}

// ===== 新增：组织（只读） =====

func (s *StandaloneService) handleListOrgs(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Organizations().ListOrgs(req.TraceContext, p.TenantID)
}

func (s *StandaloneService) handleGetOrgTree(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Organizations().GetOrgTree(req.TraceContext, p.TenantID)
}

func (s *StandaloneService) handleGetOrg(req server.Request) (any, error) {
	return s.m.Organizations().GetOrg(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleListOrgAncestors(req server.Request) (any, error) {
	return s.m.Organizations().OrgAncestors(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleListOrgMembers(req server.Request) (any, error) {
	return s.m.Organizations().ListOrgMembers(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleListMyOrgs(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Organizations().ListUserOrgs(req.TraceContext, p.UserID)
}

// ===== SDK 扩展：个人访问令牌 =====

func (s *StandaloneService) handleListAPITokens(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.APITokens().List(req.TraceContext, p.TenantID, p.UserID)
}

func (s *StandaloneService) handleCreateAPIToken(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.APITokens().Create(req.TraceContext, CreateAPITokenRequest{
		TenantID:  p.TenantID,
		UserID:    p.UserID,
		Name:      body.Name,
		Scopes:    body.Scopes,
		ExpiresAt: body.ExpiresAt,
	})
}

func (s *StandaloneService) handleRevokeAPIToken(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.APITokens().Revoke(req.TraceContext, p.TenantID, p.UserID, req.GetUrlParam("id"))
}

// ===== SDK 扩展：隐私协议签署 =====

func (s *StandaloneService) handleListConsents(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.Consents().List(req.TraceContext, p.TenantID, p.UserID)
}

func (s *StandaloneService) handleRecordConsent(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		DocumentType string `json:"document_type"`
		Version      string `json:"version"`
		Locale       string `json:"locale"`
		Source       string `json:"source"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return s.m.Consents().Record(req.TraceContext, RecordConsentRequest{
		TenantID:     p.TenantID,
		UserID:       p.UserID,
		DocumentType: body.DocumentType,
		Version:      body.Version,
		Locale:       body.Locale,
		Source:       body.Source,
		IP:           req.C().ClientIP(),
		UserAgent:    string(req.C().UserAgent()),
	})
}

// ===== 新增：OIDC 端点 =====

func (s *StandaloneService) handleOAuthAuthorize(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	code, err := s.m.IdP().Authorize(req.TraceContext, AuthorizeRequest{
		ClientID:            req.GetUrlQuery("client_id"),
		RedirectURI:         req.GetUrlQuery("redirect_uri"),
		ResponseType:        req.GetUrlQuery("response_type"),
		Scope:               req.GetUrlQuery("scope"),
		State:               req.GetUrlQuery("state"),
		UserID:              p.UserID,
		TenantID:            p.TenantID,
		CodeChallenge:       req.GetUrlQuery("code_challenge"),
		CodeChallengeMethod: req.GetUrlQuery("code_challenge_method"),
	})
	if err != nil {
		return nil, err
	}
	return code, nil
}

func (s *StandaloneService) handleOAuthUserinfo(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	user, err := s.m.Users().GetByID(req.TraceContext, p.UserID)
	if err != nil {
		return nil, err
	}
	resp := map[string]any{
		"sub":            p.UserID,
		"tenant_id":      p.TenantID,
		"username":       user.Username,
		"name":           user.Nickname,
		"email":          user.Email,
		"email_verified": user.EmailVerifiedAt != nil,
		"phone":          user.Phone,
		"phone_verified": user.PhoneVerifiedAt != nil,
		"picture":        user.AvatarURL,
		"roles":          user.Roles,
	}
	return resp, nil
}

func (s *StandaloneService) handleListAuthorizedApps(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return s.m.IdP().ListAuthorizedApps(req.TraceContext, p.TenantID, p.UserID)
}

func (s *StandaloneService) handleRevokeAuthorizedApp(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	return nil, s.m.IdP().RevokeAuthorizedApp(req.TraceContext, p.TenantID, p.UserID, req.GetUrlParam("client_id"))
}

func (s *StandaloneService) handleOAuthRevoke(req server.Request) (any, error) {
	var body struct {
		Token         string `json:"token" form:"token"`
		TokenTypeHint string `json:"token_type_hint" form:"token_type_hint"`
	}
	_ = req.BindJson(&body)
	if body.Token == "" {
		body.Token = string(req.C().FormValue("token"))
	}
	// 仅支持 access_token 立刻拉黑；refresh_token 用 Logout 清理。
	p, err := s.m.Auth().Validate(req.TraceContext, body.Token)
	if err == nil && p != nil {
		_ = s.m.Auth().Logout(req.TraceContext, LogoutRequest{
			AccessToken: body.Token,
			SessionID:   p.SessionID,
		})
	}
	return map[string]bool{"revoked": true}, nil
}

func (s *StandaloneService) handleOAuthIntrospect(req server.Request) (any, error) {
	var body struct {
		Token         string `json:"token" form:"token"`
		TokenTypeHint string `json:"token_type_hint" form:"token_type_hint"`
	}
	_ = req.BindJson(&body)
	if body.Token == "" {
		body.Token = string(req.C().FormValue("token"))
	}
	p, err := s.m.Auth().Validate(req.TraceContext, body.Token)
	if err != nil || p == nil {
		return map[string]any{"active": false}, nil
	}
	return map[string]any{
		"active":     true,
		"sub":        p.UserID,
		"username":   p.Username,
		"tenant_id":  p.TenantID,
		"session_id": p.SessionID,
		"roles":      p.Roles,
	}, nil
}

// contextPrincipal 从请求上下文中提取经过认证的身份主体。
func contextPrincipal(req server.Request) *Principal {
	v, _ := req.C().Get(principalContextKey)
	if p, ok := v.(*Principal); ok {
		return p
	}
	return nil
}
