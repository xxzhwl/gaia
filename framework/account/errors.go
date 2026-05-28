package account

import (
	"github.com/xxzhwl/gaia/errwrap"
)

const (
	ErrInvalidArgument      int64 = 400
	ErrInvalidCredential    int64 = 401
	ErrInvalidToken         int64 = 401
	ErrExpiredToken         int64 = 401
	ErrRevokedToken         int64 = 401
	ErrPermissionDenied     int64 = 403
	ErrPhoneBindingRequired int64 = 428
	ErrIdentifierExists     int64 = 409
	ErrAccountLocked        int64 = 423
	ErrRateLimited          int64 = 429
	ErrRiskBlocked          int64 = 429
	ErrInternal             int64 = 500
)

// accountError creates an error with the account module namespace and the given code and message.
func accountError(code int64, msg string) error {
	return errwrap.New("account", code, msg)
}
