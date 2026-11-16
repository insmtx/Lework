package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestControlPlaneSubjects(t *testing.T) {
	event, err := ControlPlaneEventSubject("node-1")
	if err != nil {
		t.Fatalf("ControlPlaneEventSubject: %v", err)
	}
	if event != "cp.events.node.node-1" {
		t.Fatalf("event subject = %q, want %q", event, "cp.events.node.node-1")
	}
	if got := ControlPlaneEventWildcard(); got != "cp.events.node.>" {
		t.Fatalf("event wildcard = %q, want %q", got, "cp.events.node.>")
	}

	request, err := ControlPlaneNodeRequestSubject("node-1")
	if err != nil {
		t.Fatalf("ControlPlaneNodeRequestSubject: %v", err)
	}
	if request != "cp.node.node-1.request" {
		t.Fatalf("request subject = %q, want %q", request, "cp.node.node-1.request")
	}

	pty, err := ControlPlanePTYSubject("node-1", "pty-9")
	if err != nil {
		t.Fatalf("ControlPlanePTYSubject: %v", err)
	}
	if pty != "cp.pty.node-1.pty-9" {
		t.Fatalf("pty subject = %q, want %q", pty, "cp.pty.node-1.pty-9")
	}

	pair, err := ControlPlanePairSubject("claim-1")
	if err != nil {
		t.Fatalf("ControlPlanePairSubject: %v", err)
	}
	if pair != "cp.pair.claim-1" {
		t.Fatalf("pair subject = %q, want %q", pair, "cp.pair.claim-1")
	}

	if StreamNameControlPlane != "CONTROLPLANE_EVENT_STREAM" {
		t.Fatalf("stream name = %q", StreamNameControlPlane)
	}
	if ControlPlanePushConsumer != "controlplane-push-consumer" {
		t.Fatalf("push consumer = %q", ControlPlanePushConsumer)
	}

	// 空 node / pty 无法构造合法 subject。
	if _, err := ControlPlaneEventSubject(""); err == nil {
		t.Error("empty node id should fail for the event subject")
	}
	if _, err := ControlPlaneNodeRequestSubject(""); err == nil {
		t.Error("empty node id should fail for the request subject")
	}
	if _, err := ControlPlanePTYSubject("", "pty-1"); err == nil {
		t.Error("empty node id should fail for the pty subject")
	}
	if _, err := ControlPlanePTYSubject("node-1", ""); err == nil {
		t.Error("empty pty id should fail for the pty subject")
	}
	if _, err := ControlPlanePairSubject(""); err == nil {
		t.Error("empty claim id should fail for the pair subject")
	}

	// 非法 token 里的分隔符与通配符必须被替换，不能伪造出额外的 subject 层级。
	event, err = ControlPlaneEventSubject("n.1")
	if err != nil {
		t.Fatalf("ControlPlaneEventSubject(n.1): %v", err)
	}
	request, err = ControlPlaneNodeRequestSubject("n.1")
	if err != nil {
		t.Fatalf("ControlPlaneNodeRequestSubject(n.1): %v", err)
	}
	pty, err = ControlPlanePTYSubject("n.1", "p.1")
	if err != nil {
		t.Fatalf("ControlPlanePTYSubject(n.1, p.1): %v", err)
	}
	pair, err = ControlPlanePairSubject("c.1")
	if err != nil {
		t.Fatalf("ControlPlanePairSubject(c.1): %v", err)
	}
	if event != "cp.events.node.n_1" {
		t.Errorf("event subject = %q, want sanitized token", event)
	}
	if request != "cp.node.n_1.request" {
		t.Errorf("request subject = %q, want sanitized token", request)
	}
	if pty != "cp.pty.n_1.p_1" {
		t.Errorf("pty subject = %q, want sanitized token", pty)
	}
	if pair != "cp.pair.c_1" {
		t.Errorf("pair subject = %q, want sanitized token", pair)
	}

	for _, id := range []string{"n*", "n>", " n "} {
		subject, err := ControlPlaneEventSubject(id)
		if err != nil {
			t.Fatalf("ControlPlaneEventSubject(%q): %v", id, err)
		}
		if strings.ContainsAny(subject[len("cp.events.node."):], ".*> ") {
			t.Errorf("event subject %q keeps a wildcard or separator from node id %q", subject, id)
		}
	}
}

func TestControlPlaneLiveSubjects(t *testing.T) {
	subject, err := ControlPlaneLiveSubject("node-1")
	if err != nil {
		t.Fatalf("ControlPlaneLiveSubject: %v", err)
	}
	if subject != "cp.live.node.node-1" {
		t.Fatalf("live subject = %q, want %q", subject, "cp.live.node.node-1")
	}
	if got := ControlPlaneLiveWildcard(); got != "cp.live.>" {
		t.Fatalf("live wildcard = %q, want %q", got, "cp.live.>")
	}
	if _, err := ControlPlaneLiveSubject(""); err == nil {
		t.Error("empty node id should fail for the live subject")
	}

	sanitized, err := ControlPlaneLiveSubject("n.1")
	if err != nil {
		t.Fatalf("ControlPlaneLiveSubject(n.1): %v", err)
	}
	if sanitized != "cp.live.node.n_1" {
		t.Errorf("live subject = %q, want sanitized token", sanitized)
	}
}

func TestStreamConfigsIncludesControlPlane(t *testing.T) {
	configs := StreamConfigs()
	cfg, ok := configs[StreamNameControlPlane]
	if !ok {
		t.Fatalf("missing stream config for %q", StreamNameControlPlane)
	}
	if cfg.Name != StreamNameControlPlane {
		t.Errorf("stream name = %q, want %q", cfg.Name, StreamNameControlPlane)
	}
	if len(cfg.Subjects) != 1 || cfg.Subjects[0] != ControlPlaneEventWildcard() {
		t.Errorf("subjects = %v, want [%q]", cfg.Subjects, ControlPlaneEventWildcard())
	}
	if cfg.Storage != nats.FileStorage {
		t.Errorf("storage = %v, want FileStorage", cfg.Storage)
	}
	if cfg.Retention != nats.LimitsPolicy {
		t.Errorf("retention = %v, want LimitsPolicy", cfg.Retention)
	}
	if cfg.Discard != nats.DiscardOld {
		t.Errorf("discard = %v, want DiscardOld", cfg.Discard)
	}
	if cfg.MaxAge != 24*time.Hour {
		t.Errorf("max age = %v, want %v", cfg.MaxAge, 24*time.Hour)
	}
	if cfg.MaxMsgsPerSubject != 10000 {
		t.Errorf("max msgs per subject = %d, want 10000", cfg.MaxMsgsPerSubject)
	}

	// 节点请求、PTY 与增量事件走 Core NATS，不得被任何 stream 捕获。
	forbidden := []string{
		"cp.node.node-1.request",
		"cp.pty.node-1.pty-9",
		"cp.live.node.node-1",
		"cp.pair.claim-1",
	}
	for name, stream := range configs {
		for _, subject := range stream.Subjects {
			for _, want := range forbidden {
				if subjectMatches(subject, want) {
					t.Errorf("stream %q captures %q via %q", name, want, subject)
				}
			}
		}
	}
}

// subjectMatches 用最简单的 NATS 通配语义比较两个 subject：* 匹配单个 token，
// > 匹配剩余全部 token。只用于本测试的少量固定 subject。
func subjectMatches(pattern, subject string) bool {
	patternTokens := strings.Split(pattern, ".")
	subjectTokens := strings.Split(subject, ".")
	for i, token := range patternTokens {
		if token == ">" {
			return len(subjectTokens) > i
		}
		if i >= len(subjectTokens) {
			return false
		}
		if token != "*" && token != subjectTokens[i] {
			return false
		}
	}
	return len(patternTokens) == len(subjectTokens)
}

func TestControlPlaneNodeCloseSubject(t *testing.T) {
	subject, err := ControlPlaneNodeCloseSubject("node_abc")
	if err != nil || subject != "cp.node.node_abc.close" {
		t.Fatalf("subject = %q, err = %v", subject, err)
	}
	// sanitizeSubjectToken 的既有契约是「非法字符替换成下划线」而不是报错：
	// 关键是不能让点号 / 通配符伪造出别的 subject。空 ID 才是错误。
	if got, err := ControlPlaneNodeCloseSubject("bad id"); err != nil || got != "cp.node.bad_id.close" {
		t.Fatalf("sanitized subject = %q, err = %v", got, err)
	}
	if _, err := ControlPlaneNodeCloseSubject("   "); err == nil {
		t.Fatal("empty node id must be rejected")
	}
	if got := ControlPlaneNodeCloseWildcard(); got != "cp.node.*.close" {
		t.Fatalf("wildcard = %q", got)
	}
}
