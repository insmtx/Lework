package db

import (
	"context"
	"fmt"

	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// BatchCreateMessageResources creates multiple message_resource records in a single transaction.
// It generates resource_public_id for each record using snowflake.
func BatchCreateMessageResources(ctx context.Context, db *gorm.DB, records []*types.MessageResource) error {
	if len(records) == 0 {
		return nil
	}

	for _, r := range records {
		if r.ResourcePublicID == "" {
			r.ResourcePublicID = fmt.Sprintf("msgr_%s", snowflake.GenerateIDBase58())
		}
	}

	return db.WithContext(ctx).Create(records).Error
}
