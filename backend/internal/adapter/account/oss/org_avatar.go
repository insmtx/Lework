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

func resolveSingleOrgLogoURL(ctx context.Context, db *gorm.DB, orgID uint, logoURL string) (string, error) {
	if !account.IsFilePublicID(logoURL) {
		return logoURL, nil
	}
	if db == nil {
		return "", fmt.Errorf("db is nil")
	}

	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, db, orgID, logoURL)
	if err != nil {
		return "", fmt.Errorf("get file upload: %w", err)
	}
	if fileUpload == nil {
		return "", fmt.Errorf("file upload not found: %s", logoURL)
	}

	url, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
	if err != nil {
		return "", fmt.Errorf("resolve public url: %w", err)
	}
	return url, nil
}

func resolveSingleOrgLogoMap(ctx context.Context, db *gorm.DB, orgID uint, logoURL string) (map[string]string, error) {
	if !account.IsFilePublicID(logoURL) {
		return nil, nil
	}
	resolved, err := resolveSingleOrgLogoURL(ctx, db, orgID, logoURL)
	if err != nil {
		return nil, err
	}
	if resolved == logoURL {
		return nil, nil
	}
	return map[string]string{logoURL: resolved}, nil
}

func resolveOrgLogoURLs(ctx context.Context, db *gorm.DB, orgID uint, orgs []*types.Organization) map[string]string {
	var publicIDs []string
	for _, o := range orgs {
		if account.IsFilePublicID(o.Logo) {
			publicIDs = append(publicIDs, o.Logo)
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
