package messaging

import (
	"encoding/json"
	"testing"
)

func TestMaxWaitingTasksLimit(t *testing.T) {
	if MaxWaitingTasks != 100 {
		t.Fatalf("MaxWaitingTasks = %d, want 100", MaxWaitingTasks)
	}
}

func TestWorkerStatusRequestJSON(t *testing.T) {
	req := WorkerStatusRequest{OrgID: 1, WorkerID: 2}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded WorkerStatusRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if decoded.OrgID != 1 || decoded.WorkerID != 2 {
		t.Fatalf("decoded = %+v, want org=1 worker=2", decoded)
	}
}

func TestWorkerStatusRequestKeys(t *testing.T) {
	data, err := json.Marshal(WorkerStatusRequest{OrgID: 1, WorkerID: 2})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["org_id"]; !ok {
		t.Errorf("expected org_id key in request JSON")
	}
	if _, ok := raw["worker_id"]; !ok {
		t.Errorf("expected worker_id key in request JSON")
	}
}

func TestWorkerStatusSnapshotJSONRoundTrip(t *testing.T) {
	snapshot := WorkerStatusSnapshot{
		OrgID:                 1,
		WorkerID:              2,
		MaxConcurrency:        10,
		RunningCount:          2,
		WaitingCount:          3,
		AdmissionWaitingCount: 1,
		AcceptedCount:         5,
		InboxPendingCount:     1,
		InboxProcessingCount:  4,
		SnapshotAt:            1700000000,
		RunningTasks: []WorkerRunSummary{
			{
				RunID:     "run-1",
				TaskID:    "task-1",
				SessionID: "sess-1",
				CommandID: "cmd-1",
				StreamSeq: 11,
				Status:    "running",
				CreatedAt: 1700000000,
				UpdatedAt: 1700000001,
				StartedAt: 1700000002,
			},
		},
		WaitingTasks: []WorkerRunSummary{
			{
				RunID:     "run-2",
				TaskID:    "task-2",
				SessionID: "sess-2",
				CommandID: "cmd-2",
				StreamSeq: 12,
				Status:    "waiting",
				CreatedAt: 1700000003,
				UpdatedAt: 1700000003,
			},
		},
		WaitingTruncated: true,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	var decoded WorkerStatusSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if decoded.OrgID != 1 || decoded.WorkerID != 2 || decoded.MaxConcurrency != 10 || decoded.RunningCount != 2 || decoded.WaitingCount != 3 {
		t.Fatalf("counts decoded = %+v", decoded)
	}
	if decoded.SnapshotAt != 1700000000 {
		t.Fatalf("snapshot_at = %d, want 1700000000", decoded.SnapshotAt)
	}
	if len(decoded.RunningTasks) != 1 || decoded.RunningTasks[0].RunID != "run-1" {
		t.Fatalf("running tasks decoded = %+v", decoded.RunningTasks)
	}
	if decoded.RunningTasks[0].StartedAt != 1700000002 {
		t.Fatalf("running task started_at = %d, want 1700000002", decoded.RunningTasks[0].StartedAt)
	}
	if len(decoded.WaitingTasks) != 1 || decoded.WaitingTasks[0].StreamSeq != 12 {
		t.Fatalf("waiting tasks decoded = %+v", decoded.WaitingTasks)
	}
	if !decoded.WaitingTruncated {
		t.Fatalf("expected waiting_truncated=true")
	}
}
