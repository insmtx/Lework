// @title Leros API
// @version 1.0
// @description Leros 数字助手平台 API，提供数字助手管理、技能调用、事件处理等功能
// @host localhost:8080
// @BasePath /v1
// @schemes http https
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/api"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/lifecycle"
	"github.com/ygpkg/yg-go/logs"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

var (
	serverConfigPath    string
	serverWorkspaceRoot string
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Leros backend HTTP server",
		Long:  `Start the HTTP server that handles API requests and publishes external events.`,
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := loadConfig(serverConfigPath)
			if err != nil {
				logs.Fatalf("Failed to load config: %v", err)
				return
			}
			if strings.TrimSpace(serverWorkspaceRoot) != "" {
				cfg.WorkspaceRoot = serverWorkspaceRoot
				logs.Infof("Using workspace root from flag: %s", serverWorkspaceRoot)
			}
			if err := applyServerWorkspaceRoot(cfg); err != nil {
				logs.Fatalf("Invalid server workspace config: %v", err)
				return
			}

			if _, err := leros.EnsureStateDir(); err != nil {
				logs.Fatalf("Failed to ensure state dir: %v", err)
				return
			}
			if err := engines.SyncServerSkillsDir(""); err != nil {
				logs.Warnf("Sync server built-in skills failed: %v", err)
			}

			natsUrl := "nats://nats:4222"
			if cfg.NATS != nil && cfg.NATS.URL != "" {
				natsUrl = cfg.NATS.URL
			}

			publisher, err := mq.NewNATS(natsUrl)
			if err != nil {
				logs.Fatalf("Failed to create event publisher: %v", err)
				return
			}

			var db *gorm.DB
			if cfg.Database != nil && cfg.Database.URL != "" {
				db, err = infradb.InitDB(*cfg.Database, cfg.LLM)
				if err != nil {
					logs.Fatalf("Failed to initialize database: %v", err)
					return
				}
				logs.Info("Database initialized successfully")
			} else {
				logs.Warn("No database configuration provided")
				logs.Warn("  - Database-dependent features (user persistence, etc.) will be unavailable")
				logs.Warn("  - To enable database, add database.url to your config file")
				logs.Warn("  - See example-config.yaml for database configuration example")
			}

			r := api.SetupRouter(*cfg, publisher, db)

			srv := &http.Server{
				Addr:    fmt.Sprintf(":%s", cfg.Server.Port),
				Handler: r,
			}

			logs.Info("Starting Leros backend service...")
			logs.Infof("Listening on %s", srv.Addr)

			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logs.Fatalf("Failed to start server: %v", err)
				}
			}()

			lifecycle.Std().AddCloseFunc(func() error {
				if err := srv.Shutdown(cmd.Context()); err != nil {
					logs.Errorf("Server forced to shutdown: %v", err)
				}
				return nil
			})

			lifecycle.Std().AddCloseFunc(publisher.Close)
			lifecycle.Std().WaitExit()

			logs.Info("Server exited")
		},
	}

	cmd.Flags().StringVar(&serverConfigPath, "config", "", "Configuration file path")
	cmd.Flags().StringVar(&serverWorkspaceRoot, "workspace-root", "", "Default server workspace root for worker mounts")
	return cmd
}

func applyServerWorkspaceRoot(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	root := strings.TrimSpace(cfg.WorkspaceRoot)
	if root == "" {
		return nil
	}
	if err := os.Setenv(leros.EnvWorkspaceRoot, root); err != nil {
		return fmt.Errorf("set %s: %w", leros.EnvWorkspaceRoot, err)
	}
	logs.Infof("Using server workspace root from config: %s", root)
	return nil
}

func loadConfig(configPath string) (*config.Config, error) {
	var cfg config.Config

	if configPath != "" {
		err := LoadYamlLocalFile(configPath, &cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to load config from %s: %v", configPath, err)
		}
	} else {
		pathsToTry := []string{"./config.yaml", "/app/config.yaml"}

		err := fmt.Errorf("config file not found in any location")
		for _, path := range pathsToTry {
			if err = LoadYamlLocalFile(path, &cfg); err == nil {
				logs.Infof("Loaded config from: %s", path)
				break
			}
		}

		if err != nil {
			logs.Warnf("Could not load config from any path (%v), will proceed without config", err)
		}
	}

	logs.Info("Configuration loaded successfully")
	return &cfg, nil
}

// LoadYamlLocalFile .
func LoadYamlLocalFile(file string, cfg interface{}) error {
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Printf("[config] laod %s failed, %s\n", file, err)
		return err
	}

	data = []byte(os.ExpandEnv(string(data)))

	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		fmt.Printf("[config] decode %s failed, %s\n", file, err)
		return err
	}

	return nil
}
