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
	defaultBucketName  = "dev-bucket"
	defaultDriver      = "local"
	defaultLocalDir    = "leros-storage"
	defaultSignSecret  = "leros-local-presign"
	defaultSignBaseURL = ""
)

var (
	defaultStorage storage.Storage
	defaultBucket  string = defaultBucketName
	driverType     storage.DriverType
	signSecret     string = defaultSignSecret
)

func Init(cfg *config.StorageConfig) error {
	if cfg == nil {
		if dir := strings.TrimSpace(os.Getenv("LEROS_STORAGE_LOCAL_DIR")); dir != "" {
			cfg = &config.StorageConfig{
				Driver:     defaultDriver,
				LocalDir:   dir,
				Bucket:     defaultBucketName,
				SignSecret: defaultSignSecret,
				BaseURL:    defaultSignBaseURL,
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
				Driver:     defaultDriver,
				LocalDir:   filepath.Join(root, defaultLocalDir),
				Bucket:     defaultBucketName,
				SignSecret: defaultSignSecret,
				BaseURL:    defaultSignBaseURL,
			}
		}
	}
	driver := storage.DriverType(cfg.Driver)
	if driver == "" {
		driver = defaultDriver
	}
	if cfg.LocalDir == "" {
		cfg.LocalDir = defaultLocalDir
	}
	if cfg.Bucket == "" {
		cfg.Bucket = defaultBucketName
	}
	if cfg.SignSecret == "" {
		cfg.SignSecret = defaultSignSecret
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultSignBaseURL
	}
	driverType = driver
	sCfg := storage.Config{
		Endpoint:   cfg.Endpoint,
		AccessKey:  cfg.AccessKey,
		SecretKey:  cfg.SecretKey,
		Bucket:     cfg.Bucket,
		UseSSL:     cfg.UseSSL,
		BaseDir:    cfg.LocalDir,
		BaseURL:    cfg.BaseURL,
		SignSecret: cfg.SignSecret,
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
	signSecret = cfg.SignSecret
	return nil
}

func GetStorage() storage.Storage {
	return defaultStorage
}

func DefaultBucket() string {
	return defaultBucket
}

// SignSecret returns the current presign signing secret
func SignSecret() string {
	return signSecret
}

// IsLocal 返回当前 storage 驱动是否为 local
func IsLocal() bool {
	return driverType == "local"
}
