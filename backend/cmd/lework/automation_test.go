package main

import "testing"

func TestBuildAutomationScheduleCreateDefaultsTimezone(t *testing.T) {
	schedule, timezone, err := buildAutomationSchedule(automationScheduleOptions{
		mode:   "calendar",
		preset: "daily",
		hour:   9,
		minute: 30,
	}, true)
	if err != nil {
		t.Fatalf("buildAutomationSchedule() error = %v", err)
	}
	if timezone != defaultAutomationTimezone || schedule.Timezone != defaultAutomationTimezone {
		t.Fatalf("timezone = %q, schedule timezone = %q", timezone, schedule.Timezone)
	}
	if schedule.Calendar == nil || schedule.Calendar.Hour != 9 || schedule.Calendar.Minute != 30 {
		t.Fatalf("unexpected calendar schedule: %+v", schedule.Calendar)
	}
}

func TestBuildAutomationScheduleUpdatePreservesTimezone(t *testing.T) {
	schedule, timezone, err := buildAutomationSchedule(automationScheduleOptions{
		mode:            "interval",
		intervalMinutes: 15,
	}, false)
	if err != nil {
		t.Fatalf("buildAutomationSchedule() error = %v", err)
	}
	if timezone != "" || schedule.Timezone != "" {
		t.Fatalf("timezone = %q, schedule timezone = %q; update should preserve server value", timezone, schedule.Timezone)
	}
	if schedule.Interval == nil || schedule.Interval.IntervalMinutes != 15 {
		t.Fatalf("unexpected interval schedule: %+v", schedule.Interval)
	}
}

func TestParseAutomationStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		enabled bool
		wantErr bool
	}{
		{name: "enabled", input: "enabled", enabled: true},
		{name: "paused", input: "paused", enabled: false},
		{name: "invalid", input: "running", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAutomationStatus(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAutomationStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.enabled {
				t.Fatalf("parseAutomationStatus() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestParseAutomationDays(t *testing.T) {
	days, err := parseAutomationDays("1, 3,5", 0, 6)
	if err != nil {
		t.Fatalf("parseAutomationDays() error = %v", err)
	}
	if len(days) != 3 || days[0] != 1 || days[1] != 3 || days[2] != 5 {
		t.Fatalf("parseAutomationDays() = %#v", days)
	}
	if _, err := parseAutomationDays("1,1", 0, 6); err == nil {
		t.Fatal("parseAutomationDays() accepted duplicate values")
	}
}

func TestResolveAutomationTargetUserID(t *testing.T) {
	got, err := resolveAutomationTargetUserID(0)
	if err != nil {
		t.Fatalf("resolveAutomationTargetUserID() error = %v", err)
	}
	if got != nil {
		t.Fatalf("resolved user ID = %v, want nil without explicit target", got)
	}

	explicit, err := resolveAutomationTargetUserID(7)
	if err != nil {
		t.Fatalf("explicit resolve error = %v", err)
	}
	if explicit == nil || *explicit != 7 {
		t.Fatalf("explicit user ID = %v, want 7", explicit)
	}

}
