package provider

import "testing"

func TestBuildRunEnvMergesBaseExtraAndModelEnv(t *testing.T) {
	env := BuildRunEnv([]string{"PATH=/bin"}, []string{"CUSTOM=1"}, map[string]string{
		"MODEL_API_KEY": "key",
	})

	want := map[string]bool{
		"PATH=/bin":         false,
		"CUSTOM=1":          false,
		"MODEL_API_KEY=key": false,
	}
	for _, item := range env {
		if _, ok := want[item]; ok {
			want[item] = true
		}
	}
	for item, found := range want {
		if !found {
			t.Fatalf("missing env entry %q in %#v", item, env)
		}
	}
}

func TestBuildRunEnvModelEnvOverridesEarlierEntries(t *testing.T) {
	env := BuildRunEnv([]string{"API_KEY=base"}, []string{"API_KEY=extra"}, map[string]string{
		"API_KEY": "model",
	})

	want := map[string]bool{
		"API_KEY=model": false,
	}
	for _, item := range env {
		if _, ok := want[item]; ok {
			want[item] = true
		}
	}
	for item, found := range want {
		if !found {
			t.Fatalf("missing env entry %q in %#v", item, env)
		}
	}
}
