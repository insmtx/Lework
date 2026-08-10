package seed

import (
	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

// isOSS 判断当前 edition 是否为开源版（OSS）。企业版返回 false。
func isOSS(edition adapter.Edition) bool {
	return edition != nil && edition.Edition() == account.EditionOSS
}
