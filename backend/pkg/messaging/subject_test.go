package messaging

import (
	"testing"
)

func TestWorkerCommandSubject(t *testing.T) {
	tests := []struct {
		name     string
		orgID    uint
		workerID uint
		lane     Lane
		want     string
		wantErr  bool
	}{
		{
			name:     "cmd.run lane",
			orgID:    1,
			workerID: 2,
			lane:     LaneRun,
			want:     "org.1.worker.2.cmd.run",
		},
		{
			name:     "cmd.control lane",
			orgID:    10,
			workerID: 20,
			lane:     LaneControl,
			want:     "org.10.worker.20.cmd.control",
		},
		{
			name:     "cmd.interaction lane",
			orgID:    5,
			workerID: 3,
			lane:     LaneInteraction,
			want:     "org.5.worker.3.cmd.interaction",
		},
		{
			name:     "cmd.skill lane",
			orgID:    7,
			workerID: 8,
			lane:     LaneSkill,
			want:     "org.7.worker.8.cmd.skill",
		},
		{
			name:     "missing orgID",
			orgID:    0,
			workerID: 1,
			lane:     LaneRun,
			wantErr:  true,
		},
		{
			name:     "missing workerID",
			orgID:    1,
			workerID: 0,
			lane:     LaneRun,
			wantErr:  true,
		},
		{
			name:     "missing lane",
			orgID:    1,
			workerID: 1,
			lane:     "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WorkerCommandSubject(tt.orgID, tt.workerID, tt.lane)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestRunEventSubject(t *testing.T) {
	tests := []struct {
		name      string
		orgID     uint
		sessionID string
		lane      RunEventLane
		want      string
		wantErr   bool
	}{
		{
			name:      "run.stream lane",
			orgID:     1,
			sessionID: "sess-abc",
			lane:      RunEventLaneStream,
			want:      "org.1.session.sess-abc.run.stream",
		},
		{
			name:      "run.state lane",
			orgID:     5,
			sessionID: "xyz-123",
			lane:      RunEventLaneState,
			want:      "org.5.session.xyz-123.run.state",
		},
		{
			name:      "missing orgID",
			orgID:     0,
			sessionID: "sess",
			lane:      RunEventLaneStream,
			wantErr:   true,
		},
		{
			name:      "missing sessionID",
			orgID:     1,
			sessionID: "",
			lane:      RunEventLaneStream,
			wantErr:   true,
		},
		{
			name:      "missing lane",
			orgID:     1,
			sessionID: "sess",
			lane:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunEventSubject(tt.orgID, tt.sessionID, tt.lane)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestWildcardSubjects(t *testing.T) {
	if got := WorkerCommandWildcard(); got != "org.*.worker.*.cmd.>" {
		t.Errorf("WorkerCommandWildcard: got %q", got)
	}
	if got := RunEventWildcard(); got != "org.*.session.*.run.>" {
		t.Errorf("RunEventWildcard: got %q", got)
	}
	if got := RunEventStateWildcard(); got != "org.*.session.*.run.state" {
		t.Errorf("RunEventStateWildcard: got %q", got)
	}
	if got := RunEventStreamWildcard(); got != "org.*.session.*.run.stream" {
		t.Errorf("RunEventStreamWildcard: got %q", got)
	}
}

func TestCommandLane(t *testing.T) {
	tests := []struct {
		cmdType CommandType
		want    Lane
	}{
		{CommandTypeRun, LaneRun},
		{CommandTypeCancel, LaneControl},
		{CommandTypeApprovalResolve, LaneInteraction},
		{CommandTypeQuestionAnswer, LaneInteraction},
		{CommandTypeSkill, LaneSkill},
	}

	for _, tt := range tests {
		t.Run(string(tt.cmdType), func(t *testing.T) {
			if got := CommandLane(tt.cmdType); got != tt.want {
				t.Errorf("CommandLane(%q) = %q, want %q", tt.cmdType, got, tt.want)
			}
		})
	}
}

func TestClassifyRunEvent(t *testing.T) {
	tests := []struct {
		eventType RunEventType
		want      RunEventLane
	}{
		// State lane events
		{RunEventRunStarted, RunEventLaneState},
		{RunEventRunCompleted, RunEventLaneState},
		{RunEventRunFailed, RunEventLaneState},
		{RunEventRunCancelled, RunEventLaneState},
		{RunEventArtifactDeclared, RunEventLaneState},
		{RunEventApprovalRequested, RunEventLaneState},
		{RunEventApprovalResolved, RunEventLaneState},
		{RunEventQuestionAsked, RunEventLaneState},
		{RunEventQuestionAnswered, RunEventLaneState},
		{RunEventWorkTitleUpdated, RunEventLaneState},

		// Stream lane events
		{RunEventMessageDelta, RunEventLaneStream},
		{RunEventReasoningDelta, RunEventLaneStream},
		{RunEventMessageCompleted, RunEventLaneStream},
		{RunEventToolCallStarted, RunEventLaneStream},
		{RunEventToolCallFinished, RunEventLaneStream},
		{RunEventTodoSnapshot, RunEventLaneStream},
		{RunEventTodoUpdated, RunEventLaneStream},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if got := ClassifyRunEvent(tt.eventType); got != tt.want {
				t.Errorf("ClassifyRunEvent(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestClassifyRunEventDefaultToStream(t *testing.T) {
	// Unknown event types should default to stream lane
	if got := ClassifyRunEvent("unknown.event"); got != RunEventLaneStream {
		t.Errorf("expected unknown event to default to stream lane, got %q", got)
	}
}

func TestCommandLaneDefaultToRun(t *testing.T) {
	// Unknown command types should default to run lane
	if got := CommandLane("unknown.cmd"); got != LaneRun {
		t.Errorf("expected unknown command to default to run lane, got %q", got)
	}
}

func TestConsumerNames(t *testing.T) {
	if got := WorkerRunConsumer(); got != "worker-run-consumer" {
		t.Errorf("WorkerRunConsumer: got %q", got)
	}
	if got := WorkerControlConsumer(); got != "worker-control-consumer" {
		t.Errorf("WorkerControlConsumer: got %q", got)
	}
	if got := WorkerInteractionConsumer(); got != "worker-interaction-consumer" {
		t.Errorf("WorkerInteractionConsumer: got %q", got)
	}
	if got := WorkerSkillConsumer(); got != "worker-skill-consumer" {
		t.Errorf("WorkerSkillConsumer: got %q", got)
	}
	if got := SessionRunStateConsumer(); got != "session-run-state-projector" {
		t.Errorf("SessionRunStateConsumer: got %q", got)
	}
}

func TestStreamConfigs(t *testing.T) {
	configs := StreamConfigs()

	// Verify worker command stream
	workerCfg, ok := configs[StreamNameWorker]
	if !ok {
		t.Fatal("missing worker command stream config")
	}
	if workerCfg.Name != StreamNameWorker {
		t.Errorf("expected name %q, got %q", StreamNameWorker, workerCfg.Name)
	}
	if len(workerCfg.Subjects) != 1 || workerCfg.Subjects[0] != WorkerCommandWildcard() {
		t.Errorf("expected subjects [%q], got %v", WorkerCommandWildcard(), workerCfg.Subjects)
	}
	if workerCfg.MaxAge == 0 {
		t.Error("expected non-zero MaxAge")
	}
	if workerCfg.MaxMsgsPerSubject != 200 {
		t.Errorf("expected MaxMsgsPerSubject 200, got %d", workerCfg.MaxMsgsPerSubject)
	}

	// Verify session run stream
	sessionCfg, ok := configs[StreamNameSession]
	if !ok {
		t.Fatal("missing session run stream config")
	}
	if sessionCfg.Name != StreamNameSession {
		t.Errorf("expected name %q, got %q", StreamNameSession, sessionCfg.Name)
	}
	if len(sessionCfg.Subjects) != 1 || sessionCfg.Subjects[0] != RunEventWildcard() {
		t.Errorf("expected subjects [%q], got %v", RunEventWildcard(), sessionCfg.Subjects)
	}
	if sessionCfg.MaxAge == 0 {
		t.Error("expected non-zero MaxAge")
	}
	if sessionCfg.MaxMsgsPerSubject != 10000 {
		t.Errorf("expected MaxMsgsPerSubject 10000, got %d", sessionCfg.MaxMsgsPerSubject)
	}
}

func TestStreamNameFromSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"org.1.worker.2.cmd.run", StreamNameWorker},
		{"org.10.worker.20.cmd.control", StreamNameWorker},
		{"org.5.worker.3.cmd.interaction", StreamNameWorker},
		{"org.7.worker.8.cmd.skill", StreamNameWorker},
		{"org.1.session.sess-abc.run.stream", StreamNameSession},
		{"org.5.session.xyz-123.run.state", StreamNameSession},
		{"invalid.subject", ""},
		{"org", ""},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			if got := StreamNameFromSubject(tt.subject); got != tt.want {
				t.Errorf("StreamNameFromSubject(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}
