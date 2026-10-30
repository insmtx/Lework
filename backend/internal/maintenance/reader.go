// Package maintenance reads host-owned admission state without granting business processes write access.
package maintenance

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/types"
)

var operationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Reader observes atomic file replacement; mount the containing directory rather than a subPath file.
type Reader struct {
	cfg *config.SparkMaintenanceConfig
}

// NewReader copies installation bindings so later caller mutations cannot change the controlled host.
func NewReader(cfg *config.SparkMaintenanceConfig) *Reader {
	if cfg == nil {
		return &Reader{}
	}
	copied := *cfg
	return &Reader{cfg: &copied}
}

// Read fails closed after opt-in, including when a state file disappears during a restart.
func (r *Reader) Read() types.SparkMaintenanceState {
	if r == nil || r.cfg == nil {
		return types.SparkMaintenanceState{Known: true}
	}
	failed := types.SparkMaintenanceState{Configured: true, Active: true, ErrorCode: "maintenance_state_unavailable"}
	if !filepath.IsAbs(r.cfg.StatePath) || r.cfg.HostID == "" {
		return failed
	}
	info, err := os.Lstat(r.cfg.StatePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return failed
	}
	file, err := os.Open(r.cfg.StatePath)
	if err != nil {
		return failed
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() {
		return failed
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return failed
	}
	var disk struct {
		Version     int    `json:"version"`
		HostID      string `json:"host_id"`
		Active      *bool  `json:"active"`
		Revision    uint64 `json:"revision"`
		OperationID string `json:"operation_id"`
		UpdatedAt   int64  `json:"updated_at"`
	}
	if json.Unmarshal(data, &disk) != nil || disk.Version != 1 || disk.HostID != r.cfg.HostID || disk.Active == nil || disk.Revision == 0 || disk.Revision > 9007199254740991 || !operationPattern.MatchString(disk.OperationID) || disk.UpdatedAt <= 0 || disk.UpdatedAt > time.Now().Unix()+5 {
		return failed
	}
	return types.SparkMaintenanceState{HostID: disk.HostID, Configured: true, Known: true, Active: *disk.Active, Revision: disk.Revision, OperationID: disk.OperationID}
}
