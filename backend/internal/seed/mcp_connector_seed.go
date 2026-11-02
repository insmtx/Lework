package seed

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/service"
	skilllinks "github.com/insmtx/Lework/backend/internal/skill/links"
	"github.com/insmtx/Lework/backend/types"
)

var configuredConnectorSyncMu sync.Mutex

// SyncConfiguredMCPConnectors reconciles only the system connectors declared by server configuration.
// Unlisted database channels remain untouched; an empty configuration is a no-op.
func SyncConfiguredMCPConnectors(
	ctx context.Context,
	database *gorm.DB,
	sourceDir string,
	configured []types.MCPConnectorSpec,
) (*service.BuiltinSkillSyncReport, error) {
	configuredConnectorSyncMu.Lock()
	defer configuredConnectorSyncMu.Unlock()

	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	report := &service.BuiltinSkillSyncReport{}
	if len(configured) == 0 {
		return report, nil
	}
	connectorDir, err := skilllinks.ResolveBuiltinSkillsSource(sourceDir, "connectors")
	if err != nil {
		return nil, err
	}

	configuredByCode := make(map[string][]types.MCPConnectorSpec, len(configured))
	for _, spec := range configured {
		code := strings.TrimSpace(spec.Channel)
		configuredByCode[code] = append(configuredByCode[code], spec)
	}
	configuredCodesList := make([]string, 0, len(configuredByCode))
	for code := range configuredByCode {
		configuredCodesList = append(configuredCodesList, code)
	}
	sort.Strings(configuredCodesList)
	for _, code := range configuredCodesList {
		entries := configuredByCode[code]
		if len(entries) != 1 {
			for range entries {
				report.Failures = append(report.Failures, service.BuiltinSkillSyncFailure{
					Code: code,
					Err:  fmt.Errorf("duplicate configured MCP channel"),
				})
			}
			continue
		}
		report.Scanned++
		operation, syncErr := service.SyncSystemConnectorTemplate(ctx, database, connectorDir, entries[0])
		if syncErr != nil {
			report.Failures = append(report.Failures, service.BuiltinSkillSyncFailure{Code: code, Err: syncErr})
			continue
		}
		addConfiguredConnectorOperation(report, operation)
	}
	return report, nil
}

func addConfiguredConnectorOperation(report *service.BuiltinSkillSyncReport, operation string) {
	switch operation {
	case "created":
		report.Created++
	case "updated":
		report.Updated++
	case "restored":
		report.Restored++
	default:
		report.Unchanged++
	}
}
