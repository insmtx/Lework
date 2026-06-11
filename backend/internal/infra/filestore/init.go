package filestore

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/local"
	_ "github.com/ygpkg/storage-go/driver/minio"

	"github.com/insmtx/Leros/backend/config"
)

const (
	defaultBucketName = "dev-bucket"
	defaultDriver     = "local"
	defaultLocalDir   = "leros-storage"
)

var (
	defaultStorage storage.Storage
	defaultBucket  string = defaultBucketName
)

func Init(cfg *config.StorageConfig) error {
	if cfg == nil {
		if dir := strings.TrimSpace(os.Getenv("LEROS_STORAGE_LOCAL_DIR")); dir != "" {
			cfg = &config.StorageConfig{
				Driver:   defaultDriver,
				LocalDir: dir,
				Bucket:   defaultBucketName,
			}
		} else {
			var root string
			if exe, err := os.Executable(); err == nil {
				root = filepath.Dir(exe)
			} else {
				root, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}
			cfg = &config.StorageConfig{
				Driver:   defaultDriver,
				LocalDir: filepath.Join(root, defaultLocalDir),
				Bucket:   defaultBucketName,
			}
		}
	}
	driver := storage.DriverType(cfg.Driver)
	sCfg := storage.Config{
		Endpoint:  cfg.Endpoint,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		Bucket:    cfg.Bucket,
		UseSSL:    cfg.UseSSL,
		BaseDir:   cfg.LocalDir,
		BaseURL:   cfg.BaseURL,
	}
	s, err := storage.New(driver, sCfg)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	if cfg.Driver == "local" {
		if abs, e := filepath.Abs(cfg.LocalDir); e == nil {
			log.Printf("[filestore] local bucket path: %s", abs)
		}
	}
	defaultStorage = s
	defaultBucket = cfg.Bucket
	return nil
}

func GetStorage() storage.Storage {
	return defaultStorage
}

func DefaultBucket() string {
	return defaultBucket
}
