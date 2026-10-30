package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
)

func TestReaderDisabledAndFailClosed(t *testing.T) {
	if s := NewReader(nil).Read(); s.Configured || s.Active || !s.Known {
		t.Fatalf("disabled=%+v", s)
	}
	p := filepath.Join(t.TempDir(), "state.json")
	r := NewReader(&config.SparkMaintenanceConfig{StatePath: p, HostID: "host"})
	for _, body := range []string{"", `{}`, `{"version":2}`, `{"version":1,"host_id":"host","revision":1,"operation_id":"op","updated_at":1}`, `{"version":1,"host_id":"other","active":false,"revision":1,"operation_id":"op","updated_at":1}`} {
		if body != "" {
			if err := os.WriteFile(p, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		s := r.Read()
		if !s.Configured || !s.Active || s.Known {
			t.Fatalf("bad state %q opened admission: %+v", body, s)
		}
	}
	if s := NewReader(&config.SparkMaintenanceConfig{}).Read(); !s.Active || s.Known {
		t.Fatal(s)
	}
}
func TestReaderPauseAndExplicitResumeSurviveRecreation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	cfg := &config.SparkMaintenanceConfig{StatePath: p, HostID: "host"}
	for _, active := range []bool{true, false} {
		raw := fmt.Sprintf(`{"version":1,"host_id":"host","active":%t,"revision":2,"operation_id":"op-1","updated_at":%d}`, active, time.Now().Unix())
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		s := NewReader(cfg).Read()
		if !s.Known || s.Active != active || s.Revision != 2 || s.OperationID != "op-1" {
			t.Fatal(s)
		}
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if s := NewReader(cfg).Read(); !s.Active || s.Known {
		t.Fatal("deletion resumed admission")
	}
}
func TestReaderRejectsNonregularAndOversizedState(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "state")
	cfg := &config.SparkMaintenanceConfig{StatePath: p, HostID: "host"}
	if err := os.Symlink(d, p); err != nil {
		t.Fatal(err)
	}
	if s := NewReader(cfg).Read(); s.Known || !s.Active {
		t.Fatal(s)
	}
	os.Remove(p)
	os.WriteFile(p, make([]byte, 4097), 0600)
	if s := NewReader(cfg).Read(); s.Known || !s.Active {
		t.Fatal(s)
	}
}
