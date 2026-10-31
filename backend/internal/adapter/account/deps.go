package account

import (
	"github.com/insmtx/Lework/backend/config"
	"github.com/insmtx/Lework/backend/internal/infra/sms"

	"gorm.io/gorm"
)

type Deps struct {
	DB                 *gorm.DB
	JWTSecret          string
	IAM                *config.IAMConfig
	Env                string
	WorkerProvisioning WorkerProvisioner
	SmsSender          sms.SmsSender
	WorkerAuth         *config.WorkerAuthConfig
}
