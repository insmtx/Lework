package spark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// ErrModelConflict requires a new observation before a model action can be attempted.
var ErrModelConflict = errors.New("spark model conflict")

// ValidModelRequest bounds revisions to integers that browser clients can compare exactly.
func ValidModelRequest(r types.SparkModelRequest) bool {
	return shortText(r.BootID) && r.ExpectedRevision > 0 && r.ExpectedRevision < 9007199254740991 &&
		r.MaintenanceRevision > 0 && r.MaintenanceRevision <= 9007199254740991 &&
		operationIDPattern.MatchString(r.OperationID) && (r.Action == "start" || r.Action == "stop")
}

func (s *Service) modelRequest(ctx context.Context, change *types.SparkModelRequest) (types.SparkModelControl, error) {
	var result types.SparkModelControl
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 8192,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", s.cfg.SocketPath)
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var body io.Reader
	method := http.MethodGet
	if change != nil {
		payload := struct {
			types.SparkModelRequest
			HostID string `json:"host_id"`
		}{*change, s.cfg.HostID}
		raw, err := json.Marshal(payload)
		if err != nil {
			return result, ErrBadResponse
		}
		body = bytes.NewReader(raw)
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://spark-agent/model", body)
	if err != nil {
		return result, ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return result, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		return result, ErrModelConflict
	}
	if response.StatusCode != http.StatusOK {
		return result, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil {
		return result, ErrUnavailable
	}
	// 必填布尔值和可为空字符串不能靠零值判断是否存在。
	var wire struct {
		ProtocolVersion int     `json:"protocol_version"`
		HostID          string  `json:"host_id"`
		BootID          string  `json:"boot_id"`
		ObservedAt      int64   `json:"observed_at"`
		ModelID         string  `json:"model_id"`
		Label           string  `json:"label"`
		State           string  `json:"state"`
		Revision        uint64  `json:"revision"`
		OperationID     string  `json:"operation_id"`
		Action          string  `json:"action"`
		ErrorCode       *string `json:"error_code"`
		Busy            *bool   `json:"busy"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 16384 || decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		wire.Busy == nil || wire.ErrorCode == nil {
		return result, ErrBadResponse
	}
	if wire.ProtocolVersion != 1 || wire.HostID != s.cfg.HostID || !shortText(wire.BootID) || !fresh(wire.ObservedAt) ||
		!shortText(wire.ModelID) || !shortText(wire.Label) || wire.Revision == 0 || wire.Revision > 9007199254740991 ||
		!operationIDPattern.MatchString(wire.OperationID) ||
		(wire.Action != "none" && wire.Action != "start" && wire.Action != "stop") ||
		(*wire.ErrorCode != "" && !shortText(*wire.ErrorCode)) {
		return result, ErrBadResponse
	}
	switch wire.State {
	case "starting", "draining", "stopping":
		if !*wire.Busy {
			return result, ErrBadResponse
		}
	case "ready", "stopped":
		if *wire.Busy {
			return result, ErrBadResponse
		}
	case "error", "unknown":
	default:
		return result, ErrBadResponse
	}
	return types.SparkModelControl{ProtocolVersion: wire.ProtocolVersion, HostID: wire.HostID, BootID: wire.BootID,
		ObservedAt: wire.ObservedAt, ModelID: wire.ModelID, Label: wire.Label, State: wire.State, Revision: wire.Revision,
		OperationID: wire.OperationID, Action: wire.Action, ErrorCode: *wire.ErrorCode, Busy: *wire.Busy}, nil
}

func (s *Service) observeModel(ctx context.Context, status *types.SparkStatus) {
	if !s.cfg.ModelControl {
		return
	}
	// 未知模型观察不能恢复任务入口，成功读取后再按状态开放。
	if status.MaintenanceControl != nil {
		status.MaintenanceControl.CanResume = false
	}
	model, err := s.modelRequest(ctx, nil)
	if err != nil || model.BootID != status.Snapshot.BootID || !fresh(status.Snapshot.ObservedAt) {
		status.Checks = append(status.Checks, types.SparkCheck{
			Code: "model_unknown", Message: "本机模型状态无法确认。",
		})
		return
	}
	observationsFresh := status.ValidUntil >= time.Now().Unix()
	allowed := observationsFresh && !model.Busy && model.Revision < 9007199254740991 && status.MaintenanceControl != nil &&
		status.MaintenanceControl.Confirmed && status.MaintenanceControl.Active && len(status.Workers) > 0
	for _, worker := range status.Workers {
		allowed = allowed && worker.State == "observed" && fresh(worker.ObservedAt)
	}
	model.CanStart = allowed && model.State == "stopped"
	model.CanStop = allowed && model.State == "ready"
	status.ModelControl = &model
	if control := status.MaintenanceControl; control != nil {
		control.CanResume = observationsFresh && control.Confirmed && control.Active && control.Revision < 9007199254740991 &&
			!model.Busy && model.State != "unknown"
	}
	status.CanStartModel = model.CanStart
	status.CanStopModel = model.CanStop
	if until := model.ObservedAt + 30; until < status.ValidUntil {
		status.ValidUntil = until
	}
}

// ChangeModel rechecks both revisions and sends one fixed host operation without replay.
func (s *Service) changeModel(ctx context.Context, r types.SparkModelRequest) (types.SparkModelControl, error) {
	return s.changeModelChecked(ctx, r, "")
}

func (s *Service) changeModelChecked(ctx context.Context, r types.SparkModelRequest, modelID string) (types.SparkModelControl, error) {
	var result types.SparkModelControl
	if !s.cfg.ModelControl || !s.cfg.MaintenanceControl {
		return result, ErrNotConfigured
	}
	if !ValidModelRequest(r) {
		return result, ErrModelConflict
	}
	status, err := s.rawStatus(ctx)
	if err != nil {
		return result, err
	}
	model, gate := status.ModelControl, status.MaintenanceControl
	if model == nil || (modelID != "" && model.ModelID != modelID) || gate == nil || !gate.Confirmed || !gate.Active || status.ValidUntil < time.Now().Unix() ||
		r.BootID != status.Snapshot.BootID || r.ExpectedRevision != model.Revision ||
		r.MaintenanceRevision != gate.Revision ||
		(r.Action == "start" && !model.CanStart) || (r.Action == "stop" && !model.CanStop) {
		return result, ErrModelConflict
	}
	result, err = s.modelRequest(ctx, &r)
	if err != nil {
		return types.SparkModelControl{}, err
	}
	if result.BootID != r.BootID || result.ModelID != model.ModelID || result.Revision != r.ExpectedRevision+1 ||
		result.OperationID != r.OperationID || result.Action != r.Action {
		return types.SparkModelControl{}, ErrBadResponse
	}
	return result, nil
}
