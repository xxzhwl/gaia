package account

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// AccountMetrics 持有账户模块的全部 Prometheus/OTel 指标。
// 通过 sync.Once 全局初始化一次；指标由 auth.go、middleware.go 等文件中的关键路径录制。
type AccountMetrics struct {
	// LoginTotal 密码登录总次数，按 status (success/failed) 和 reason 标签区分。
	LoginTotal metric.Int64Counter
	// LoginDuration 登录处理耗时（毫秒）。
	LoginDuration metric.Float64Histogram

	// RegisterTotal 注册总次数，按 status 标签区分。
	RegisterTotal metric.Int64Counter
	// TokenValidationTotal token 验证总次数，按 status 和 reason 标签区分。
	TokenValidationTotal metric.Int64Counter

	// TokenIssued 颁发的令牌总数（含 access + refresh）。
	TokenIssued metric.Int64Counter
	// TokenReplay refresh token 重放攻击检测次数。
	TokenReplay metric.Int64Counter
	// TOTPReplay TOTP 一次性密码重放检测次数（同一 code 或更早窗口被复用）。
	TOTPReplay metric.Int64Counter
	// StepUpGranted step-up MFA 验证通过并刷新 session.MFASatisfiedAt 的次数。
	StepUpGranted metric.Int64Counter
	// StepUpDenied 因 step-up 检查不通过而拦截敏感操作的次数，按 reason 区分
	// （expired_or_missing / session_not_found / session_inactive / no_sid）。
	StepUpDenied metric.Int64Counter

	// PermissionDenied 权限拒绝总次数。
	PermissionDenied metric.Int64Counter

	// RiskBlocked 风控阻断总次数，按 reason 标签区分。
	RiskBlocked metric.Int64Counter

	// DBError 数据库操作错误次数。
	DBError metric.Int64Counter

	// AuthzCheckDuration 权限检查处理耗时（毫秒），用于 P50/P95/P99 分析。
	AuthzCheckDuration metric.Float64Histogram

	// VerificationSendTotal 验证码发送总次数，按 channel/purpose/status 区分。
	VerificationSendTotal metric.Int64Counter

	// VerificationVerifyTotal 验证码校验总次数，按 channel/purpose/status 区分。
	VerificationVerifyTotal metric.Int64Counter

	// AuthMiddlewareDuration 认证中间件处理耗时（毫秒），用于 P50/P95/P99 分析。
	AuthMiddlewareDuration metric.Float64Histogram
}

// MetricAttr 为常用指标标签提供常量，确保标签名称一致性。
var MetricAttr = struct {
	Status    attribute.Key
	Reason    attribute.Key
	EventType attribute.Key
	Source    attribute.Key
	TokenType attribute.Key
	Method    attribute.Key
}{
	Status:    attribute.Key("status"),
	Reason:    attribute.Key("reason"),
	EventType: attribute.Key("event_type"),
	Source:    attribute.Key("source"),
	TokenType: attribute.Key("token_type"),
	Method:    attribute.Key("method"),
}

var (
	metricsOnce   sync.Once
	globalMetrics *AccountMetrics
)

// initAccountMetrics 创建 OTel 指标并注册到全局 MeterProvider。
// 若未配置 OTel SDK 提供者，指标将安全地被丢弃（noop）。
func initAccountMetrics() *AccountMetrics {
	metricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/framework/account",
			metric.WithInstrumentationVersion("1.0.0"),
		)

		m := &AccountMetrics{}
		var err error

		m.LoginTotal, err = meter.Int64Counter("acct.auth.login.total",
			metric.WithDescription("Total login attempts by status and reason"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.LoginDuration, err = meter.Float64Histogram("acct.auth.login.duration",
			metric.WithDescription("Login processing time in milliseconds"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.RegisterTotal, err = meter.Int64Counter("acct.auth.register.total",
			metric.WithDescription("Total registration attempts by status"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TokenValidationTotal, err = meter.Int64Counter("acct.auth.token.validate.total",
			metric.WithDescription("Token validation attempts by status and reason"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TokenIssued, err = meter.Int64Counter("acct.auth.token.issued",
			metric.WithDescription("Total tokens issued (access + refresh)"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TokenReplay, err = meter.Int64Counter("acct.auth.token.replay",
			metric.WithDescription("Refresh token replay attempts detected"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TOTPReplay, err = meter.Int64Counter("acct.mfa.totp.replay",
			metric.WithDescription("TOTP one-time code replay attempts detected (same/earlier time window reused)"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.StepUpGranted, err = meter.Int64Counter("acct.mfa.stepup.granted",
			metric.WithDescription("Step-up MFA verifications that successfully marked the current session"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.StepUpDenied, err = meter.Int64Counter("acct.mfa.stepup.denied",
			metric.WithDescription("Sensitive operations rejected because the current session has no valid step-up"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.PermissionDenied, err = meter.Int64Counter("acct.auth.permission.denied",
			metric.WithDescription("Permission denied decisions"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.RiskBlocked, err = meter.Int64Counter("acct.risk.blocked",
			metric.WithDescription("Login attempts blocked by risk assessment"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.DBError, err = meter.Int64Counter("acct.errors.db",
			metric.WithDescription("Database operation errors"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.AuthMiddlewareDuration, err = meter.Float64Histogram("acct.auth.middleware.duration",
			metric.WithDescription("Authentication middleware processing time in ms"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.AuthzCheckDuration, err = meter.Float64Histogram("acct.authz.check.duration",
			metric.WithDescription("Authorization check processing time in ms"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.VerificationSendTotal, err = meter.Int64Counter("acct.verification.send.total",
			metric.WithDescription("Verification code send attempts by channel, purpose and status"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.VerificationVerifyTotal, err = meter.Int64Counter("acct.verification.verify.total",
			metric.WithDescription("Verification code verify attempts by channel, purpose and status"),
		)
		if err != nil {
			otel.Handle(err)
		}

		globalMetrics = m
	})
	return globalMetrics
}

// recordTokenReplay records a refresh token replay detection event.
func recordTokenReplay(ctx context.Context) {
	if m := globalMetrics; m != nil {
		m.TokenReplay.Add(ctx, 1)
	}
}

// recordDBError increments the database error counter.
func recordDBError(ctx context.Context) {
	if m := globalMetrics; m != nil {
		m.DBError.Add(ctx, 1)
	}
}

// recordDuration records the elapsed time as a histogram observation.
func recordDuration(ctx context.Context, histogram metric.Float64Histogram, start time.Time, attrs ...attribute.KeyValue) {
	if histogram != nil {
		histogram.Record(ctx, float64(time.Since(start).Microseconds())/1000.0, metric.WithAttributes(attrs...))
	}
}
