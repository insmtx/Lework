package spark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// ErrMaintenanceConflict requires a fresh observation rather than a replayed mutation.
var ErrMaintenanceConflict = errors.New("spark maintenance conflict")
var operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type maintenanceObservation struct {
	Version     int    `json:"version"`
	HostID      string `json:"host_id"`
	BootID      string `json:"boot_id"`
	Active      bool   `json:"active"`
	Revision    uint64 `json:"revision"`
	OperationID string `json:"operation_id"`
	UpdatedAt   int64  `json:"updated_at"`
	ObservedAt  int64  `json:"observed_at"`
}

// ValidMaintenanceRequest rejects incomplete and unsafe integer values before any host access.
func ValidMaintenanceRequest(r types.SparkMaintenanceRequest) bool {
	return shortText(r.BootID) && r.Active != nil && r.ExpectedRevision > 0 &&
		r.ExpectedRevision < 9007199254740991 && operationIDPattern.MatchString(r.OperationID)
}

func (s *Service) maintenanceRequest(ctx context.Context, change *types.SparkMaintenanceRequest) (maintenanceObservation, error) {
	var result maintenanceObservation
	var body io.Reader
	method := http.MethodGet
	if change != nil {
		payload := struct {
			types.SparkMaintenanceRequest
			HostID string `json:"host_id"`
		}{*change, s.cfg.HostID}
		raw, err := json.Marshal(payload)
		if err != nil {
			return result, ErrBadResponse
		}
		body = bytes.NewReader(raw)
		method = http.MethodPost
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 8192,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", s.cfg.SocketPath)
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, method, "http://spark-agent/maintenance", body)
	if err != nil {
		return result, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return result, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		return result, ErrMaintenanceConflict
	}
	if response.StatusCode != http.StatusOK {
		return result, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return result, ErrUnavailable
	}
	// 指针区分合法 false 与缺失字段，避免畸形响应误开启恢复入口。
	var wire struct {
		maintenanceObservation
		Active *bool `json:"active"`
	}
	if len(raw) > 4096 || json.Unmarshal(raw, &wire) != nil || wire.Active == nil {
		return result, ErrBadResponse
	}
	result = wire.maintenanceObservation
	result.Active = *wire.Active
	if result.Version != 1 || result.HostID != s.cfg.HostID || !shortText(result.BootID) || !fresh(result.ObservedAt) ||
		result.Revision == 0 || result.Revision > 9007199254740991 || !operationIDPattern.MatchString(result.OperationID) ||
		result.UpdatedAt <= 0 || result.UpdatedAt > time.Now().Unix()+5 {
		return maintenanceObservation{}, ErrBadResponse
	}
	return result, nil
}

func sameGate(g *types.SparkMaintenanceState, o maintenanceObservation) bool {
	return g != nil && g.Configured && g.Known && g.HostID == o.HostID && g.Revision == o.Revision && g.OperationID == o.OperationID && g.Active == o.Active
}

func (s *Service) observeMaintenance(ctx context.Context, status *types.SparkStatus, workers []*types.SparkMaintenanceState) {
	if !s.cfg.MaintenanceControl {
		return
	}
	o, err := s.maintenanceRequest(ctx, nil)
	if err != nil || o.BootID != status.Snapshot.BootID || !fresh(status.Snapshot.ObservedAt) {
		return
	}
	gate := s.maintenance.Read()
	confirmed := sameGate(&gate, o) && len(workers) > 0 && len(workers) == len(s.cfg.WorkerIDs)
	for _, g := range workers {
		confirmed = confirmed && sameGate(g, o)
	}
	for _, w := range status.Workers {
		confirmed = confirmed && w.State != "unknown" && fresh(w.ObservedAt)
	}
	status.MaintenanceControl = &types.SparkMaintenanceControl{Revision: o.Revision, OperationID: o.OperationID, Active: o.Active,
		Confirmed: confirmed, CanPause: confirmed && !o.Active && o.Revision < 9007199254740991, CanResume: confirmed && o.Active && o.Revision < 9007199254740991}
	if until := o.ObservedAt + 30; until < status.ValidUntil {
		status.ValidUntil = until
	}
}

// ChangeMaintenance requires fresh consumer agreement and sends exactly one fixed host mutation.
// A receipt never asserts that models are ready or that the device is safe to unplug.
func (s *Service) changeMaintenance(ctx context.Context, r types.SparkMaintenanceRequest) (types.SparkMaintenanceReceipt, error) {
	return s.changeMaintenanceChecked(ctx, r, nil)
}

func (s *Service) changeMaintenanceChecked(ctx context.Context, r types.SparkMaintenanceRequest, expected *types.SparkWorkflowOperation) (types.SparkMaintenanceReceipt, error) {
	var receipt types.SparkMaintenanceReceipt
	if !s.cfg.MaintenanceControl {
		return receipt, ErrNotConfigured
	}
	if !ValidMaintenanceRequest(r) {
		return receipt, ErrMaintenanceConflict
	}
	status, err := s.rawStatus(ctx)
	if err != nil {
		return receipt, err
	}
	if expected != nil && (status.ModelControl == nil || !modelReceipt(expected, status.ModelControl) || !modelDone(expected, status.ModelControl)) {
		return receipt, ErrMaintenanceConflict
	}
	control := status.MaintenanceControl
	if control == nil || !control.Confirmed || status.ValidUntil < time.Now().Unix() || r.BootID != status.Snapshot.BootID ||
		control.Revision != r.ExpectedRevision || (*r.Active && !control.CanPause) || (!*r.Active && (!control.CanResume || r.OperationID != control.OperationID)) {
		return receipt, ErrMaintenanceConflict
	}
	result, err := s.maintenanceRequest(ctx, &r)
	if err != nil {
		return receipt, err
	}
	if result.BootID != r.BootID || result.Revision != r.ExpectedRevision+1 || result.OperationID != r.OperationID || result.Active != *r.Active {
		return receipt, ErrBadResponse
	}
	return types.SparkMaintenanceReceipt{Revision: result.Revision, OperationID: result.OperationID, Active: result.Active}, nil
}
