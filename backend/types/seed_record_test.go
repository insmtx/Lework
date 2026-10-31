package types

import "testing"

func TestSeedRecordTableName(t *testing.T) {
	var r SeedRecord
	if got := r.TableName(); got != "lework_seed_record" {
		t.Fatalf("expected lework_seed_record, got %s", got)
	}
}
