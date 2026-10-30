package types

// SparkCheck explains why an observed device cannot yet be safely controlled.
type SparkCheck struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SparkComponent exposes inventory metadata without container arguments or secrets.
type SparkComponent struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Registered bool   `json:"registered"`
}

// SparkSnapshot binds host observations to one boot and a bounded freshness window.
type SparkSnapshot struct {
	ProtocolVersion   int              `json:"protocol_version"`
	HostID            string           `json:"host_id"`
	BootID            string           `json:"boot_id"`
	Hostname          string           `json:"hostname"`
	ObservedAt        int64            `json:"observed_at"`
	Mode              string           `json:"mode"`
	ModelState        string           `json:"model_state"`
	InventoryComplete bool             `json:"inventory_complete"`
	Components        []SparkComponent `json:"components"`
	Checks            []SparkCheck     `json:"checks"`
}

// SparkWorkerState keeps missing observations distinct from zero running tasks.
type SparkWorkerState struct {
	Maintenance  string `json:"maintenance"`
	ObservedAt   int64  `json:"observed_at"`
	WorkerID     uint   `json:"worker_id"`
	State        string `json:"state"`
	RunningCount *int   `json:"running_count"`
	WaitingCount *int   `json:"waiting_count"`
}

// SparkStatus keeps capability decisions separate from observed process states.
type SparkStatus struct {
	Maintenance   string             `json:"maintenance"`
	ValidUntil    int64              `json:"valid_until"`
	Snapshot      SparkSnapshot      `json:"snapshot"`
	Workers       []SparkWorkerState `json:"workers"`
	Checks        []SparkCheck       `json:"checks"`
	CanStartModel bool               `json:"can_start_model"`
	CanStopModel  bool               `json:"can_stop_model"`
	CanShutdown   bool               `json:"can_shutdown"`
}

// SparkPreflight is advisory; it is never an authorization token for shutdown.
type SparkPreflight struct {
	Ready     bool        `json:"ready"`
	CheckedAt int64       `json:"checked_at"`
	Status    SparkStatus `json:"status"`
}

// SparkMaintenanceState distinguishes disabled support, a verified gate, and an unreadable gate.
type SparkMaintenanceState struct {
	HostID      string `json:"host_id"`
	Configured  bool   `json:"configured"`
	Known       bool   `json:"known"`
	Active      bool   `json:"active"`
	Revision    uint64 `json:"revision"`
	OperationID string `json:"operation_id"`
	ErrorCode   string `json:"error_code,omitempty"`
}
