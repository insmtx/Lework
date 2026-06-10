package storage

import (
	"fmt"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/local"
	_ "github.com/ygpkg/storage-go/driver/minio"

	"github.com/insmtx/Leros/backend/config"
)

var (
	global        storage.Storage
	defaultBucket string
)

func Init(cfg *config.StorageConfig) error {
	if cfg == nil {
		return fmt.Errorf("storage config is required")
	}
	driver := storage.DriverType(cfg.Driver)
	sCfg := storage.Config{
		Endpoint:  cfg.Endpoint,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		Bucket:    cfg.Bucket,
		UseSSL:    cfg.UseSSL,
		BaseDir:   cfg.LocalDir,
	}
	var pb storage.PathBuilder
	if driver == storage.DriverLocal {
		pb = &storage.LocalPathBuilder{
			AbsDir:  cfg.LocalDir,
			BaseURL: cfg.BaseURL,
		}
	} else {
		urlStyle := storage.URLStylePath
		if cfg.URLStyle == "virtual-hosted" {
			urlStyle = storage.URLStyleVirtualHosted
		}
		pb = &storage.S3PathBuilder{
			BaseURL:  cfg.BaseURL,
			Endpoint: cfg.Endpoint,
			URLStyle: urlStyle,
		}
	}
	s, err := storage.New(driver, sCfg, pb)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	global = s
	defaultBucket = cfg.Bucket
	return nil
}

func Get() storage.Storage {
	return global
}

func DefaultBucket() string {
	return defaultBucket
}
