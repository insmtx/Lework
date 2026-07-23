package account

import "github.com/insmtx/Leros/backend/pkg/accounterror"

var (
	ErrDatabaseRequired            = accounterror.ErrDatabaseRequired
	ErrEmailRequired               = accounterror.ErrEmailRequired
	ErrInvalidEmailFormat          = accounterror.ErrInvalidEmailFormat
	ErrPasswordRequired            = accounterror.ErrPasswordRequired
	ErrPasswordsDoNotMatch         = accounterror.ErrPasswordsDoNotMatch
	ErrPasswordTooShort            = accounterror.ErrPasswordTooShort
	ErrPasswordTooLong             = accounterror.ErrPasswordTooLong
	ErrPasswordContainsChinese     = accounterror.ErrPasswordContainsChinese
	ErrPasswordContainsWhitespace  = accounterror.ErrPasswordContainsWhitespace
	ErrPasswordStrength            = accounterror.ErrPasswordStrength
	ErrEmailAlreadyExists          = accounterror.ErrEmailAlreadyExists
	ErrInvalidEmailOrPassword      = accounterror.ErrInvalidEmailOrPassword
	ErrLoginAttemptsExceeded       = accounterror.ErrLoginAttemptsExceeded
	ErrPhoneRequired               = accounterror.ErrPhoneRequired
	ErrInvalidPhoneFormat          = accounterror.ErrInvalidPhoneFormat
	ErrPhoneCodeRequired           = accounterror.ErrPhoneCodeRequired
	ErrInvalidPhoneCode            = accounterror.ErrInvalidPhoneCode
	ErrPhoneCodeSendTooOften       = accounterror.ErrPhoneCodeSendTooOften
	ErrSMSDeliveryFailed           = accounterror.ErrSMSDeliveryFailed
	ErrRefreshTokenRequired        = accounterror.ErrRefreshTokenRequired
	ErrRefreshTokenInvalid         = accounterror.ErrRefreshTokenInvalid
	ErrUserNotFound                = accounterror.ErrUserNotFound
	ErrUserOrgNotFound             = accounterror.ErrUserOrgNotFound
	ErrUserOrgNotAllowed           = accounterror.ErrUserOrgNotAllowed
	ErrLoginRequired               = accounterror.ErrLoginRequired
	ErrOrgNotFound                 = accounterror.ErrOrgNotFound
	ErrOrgIDRequired               = accounterror.ErrOrgIDRequired
	ErrJWTSecretRequired           = accounterror.ErrJWTSecretRequired
	ErrOrganizationLimitExceeded   = accounterror.ErrOrganizationLimitExceeded
	ErrPermissionDenied            = accounterror.ErrPermissionDenied
	ErrWorkerBootstrapTokenInvalid = accounterror.ErrWorkerBootstrapTokenInvalid
	ErrWorkerNotFound              = accounterror.ErrWorkerNotFound
	ErrWorkerOrgMismatch           = accounterror.ErrWorkerOrgMismatch
	ErrWorkerNotActive             = accounterror.ErrWorkerNotActive
	ErrInvalidArg                  = accounterror.ErrInvalidArg
	ErrNotImplementedEdition       = accounterror.ErrNotImplementedEdition
)
