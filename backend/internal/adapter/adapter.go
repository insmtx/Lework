package adapter

import (
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/sms"
)

type Config struct {
	DB                 *gorm.DB
	JWTSecret          string
	IAM                *config.IAMConfig
	Env                string
	WorkerProvisioning account.WorkerProvisioner
	SmsSender          sms.SmsSender
	WorkerAuth         *config.WorkerAuthConfig
}

func (c Config) ToDeps() account.Deps {
	return account.Deps{
		DB:                 c.DB,
		JWTSecret:          c.JWTSecret,
		IAM:                c.IAM,
		Env:                c.Env,
		WorkerProvisioning: c.WorkerProvisioning,
		SmsSender:          c.SmsSender,
		WorkerAuth:         c.WorkerAuth,
	}
}
