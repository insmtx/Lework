package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResumeOrNewSessionFallsBackAndCounts(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{
		"FAKE_ACP_MODE=ok",
		"FAKE_ACP_RESUME=fail",
		"FAKE_ACP_MODEL=fake/model",
	})
	defer conn.Close()

	sess, resumed, err := conn.ResumeOrNewSession(context.Background(), "ses_old", workDir, nil)
	if err != nil {
		t.Fatalf("resume or new: %v", err)
	}
	if resumed {
		t.Fatal("resume should have failed and fallen back")
	}
	if string(sess.ID) != "s1" {
		t.Fatalf("session id = %q, want fallback session s1", sess.ID)
	}
	if got := conn.Stats().Snapshot().ResumeFallbacks; got != 1 {
		t.Fatalf("ResumeFallbacks = %d, want 1", got)
	}
}

func TestResumeOrNewSessionResumesExisting(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{
		"FAKE_ACP_MODE=ok",
		"FAKE_ACP_RESUME=ok",
		"FAKE_ACP_MODEL=fake/model",
	})
	defer conn.Close()

	sess, resumed, err := conn.ResumeOrNewSession(context.Background(), "ses_old", workDir, nil)
	if err != nil {
		t.Fatalf("resume or new: %v", err)
	}
	if !resumed {
		t.Fatal("expected resume to succeed")
	}
	if string(sess.ID) != "ses_old" {
		t.Fatalf("session id = %q, want ses_old", sess.ID)
	}
	if got := conn.Stats().Snapshot().ResumeFallbacks; got != 0 {
		t.Fatalf("ResumeFallbacks = %d, want 0", got)
	}
}

func TestRequireModelReturnsConfigMismatch(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{
		"FAKE_ACP_MODE=ok",
		"FAKE_ACP_MODEL=fake/other",
	})
	defer conn.Close()

	sess, err := conn.NewSession(context.Background(), workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	err = sess.RequireModel("fake/want")
	var mismatch *ConfigMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ConfigMismatchError", err)
	}
	if mismatch.Want != "fake/want" || mismatch.Got != "fake/other" {
		t.Fatalf("mismatch = %+v", mismatch)
	}
}

func TestRequireModelMissingOption(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{"FAKE_ACP_MODE=ok"})
	defer conn.Close()

	sess, err := conn.NewSession(context.Background(), workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	err = sess.RequireModel("fake/want")
	var mismatch *ConfigMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ConfigMismatchError", err)
	}
	if !strings.Contains(mismatch.Error(), "no model option") {
		t.Fatalf("error = %v, want missing model reason", err)
	}
}

func startFakeConn(t *testing.T, env []string) (*Conn, string) {
	t.Helper()
	binary, countFile := writeFakeACP(t, "ok")
	env = append(env, "FAKE_ACP_COUNT_FILE="+countFile)
	workDir := t.TempDir()
	conn, err := Start(context.Background(), Config{
		Binary:         binary,
		Dir:            workDir,
		Env:            env,
		StartupTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("start fake acp: %v", err)
	}
	return conn, workDir
}
