//go:build !enterprise

package oss

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
)

func resolveSingleAvatarURL(ctx context.Context, db *gorm.DB, orgID uint, avatarURL string) (string, error) {
	if !account.IsFilePublicID(avatarURL) {
		return avatarURL, nil
	}

	if db == nil {
		return "", fmt.Errorf("db is nil")
	}

	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, avatarURL)
	if err != nil {
		return "", fmt.Errorf("get file upload: %w", err)
	}
	if fileUpload == nil {
		return "", fmt.Errorf("file upload not found: %s", avatarURL)
	}

	url, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
	if err != nil {
		return "", fmt.Errorf("resolve public url: %w", err)
	}
	return url, nil
}

func resolveAvatarURLs(ctx context.Context, db *gorm.DB, orgID uint, users []*types.User) map[string]string {
	var publicIDs []string
	for _, u := range users {
		if account.IsFilePublicID(u.AvatarURL) {
			publicIDs = append(publicIDs, u.AvatarURL)
		}
	}
	if len(publicIDs) == 0 || db == nil {
		return nil
	}

	files, err := infradb.GetFileUploadsByPublicIDs(ctx, db, orgID, publicIDs)
	if err != nil {
		return nil
	}

	result := make(map[string]string, len(files))
	for _, f := range files {
		url, err := filestore.ResolvePublicURL(ctx, f.StorageURI)
		if err != nil {
			continue
		}
		result[f.PublicID] = url
	}
	return result
}

func resolveSingleAvatarMap(ctx context.Context, db *gorm.DB, orgID uint, avatarURL string) (map[string]string, error) {
	if !account.IsFilePublicID(avatarURL) {
		return nil, nil
	}
	resolved, err := resolveSingleAvatarURL(ctx, db, orgID, avatarURL)
	if err != nil {
		return nil, err
	}
	if resolved == avatarURL {
		return nil, nil
	}
	return map[string]string{avatarURL: resolved}, nil
}
