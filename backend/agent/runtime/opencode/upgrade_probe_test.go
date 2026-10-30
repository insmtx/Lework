package opencode

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// TestLegacyDBUpgradeProbe 演练「v1 时代遗留库 → v2.1.4」：用一份真实老库的副本
// 打开、跑一轮、再用老会话 ID 续跑，最后断言老会话一条不少。
//
// 只读副本，绝不碰原始库：
//
//	LEROS_ACP_PROBE=1 LEROS_ACP_PROBE_CONFIG_FILE=... LEROS_ACP_PROBE_MODEL=... \
//	LEROS_ACP_LEGACY_DB=<老库路径> go test ./backend/agent/runtime/opencode -run TestLegacyDBUpgradeProbe -v
func TestLegacyDBUpgradeProbe(t *testing.T) {
	if os.Getenv("LEROS_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEROS_ACP_PROBE=1")
	}
	legacy := os.Getenv("LEROS_ACP_LEGACY_DB")
	if legacy == "" {
		t.Skip("manual probe: set LEROS_ACP_LEGACY_DB to a legacy opencode.db")
	}
	model, err := probeModelConfig()
	if err != nil {
		t.Fatalf("load probe model: %v", err)
	}

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "opencode.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyIfExists(legacy+suffix, dbPath+suffix); err != nil {
			t.Fatalf("copy %s: %v", legacy+suffix, err)
		}
	}

	before, legacySession, err := countSessions(dbPath)
	if err != nil {
		t.Fatalf("read legacy sessions: %v", err)
	}
	t.Logf("legacy db before: session=%d session_v2=%d sample=%s",
		before.legacy, before.v2, legacySession)

	workDir := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	invoker := NewACPInvoker("opencode", nil, dataDir, 3*time.Minute)
	handler := &smokeHandler{approveAction: agent.ApprovalActionApprove, answer: [][]string{{"是"}}}

	// 1) 新会话：验证 v2.1.4 能在老库上正常迁移并落新数据。
	fresh := runUpgradeTurn(t, invoker, handler, cli.InvocationRequest{
		ExecutionID: "upgrade-fresh",
		WorkDir:     workDir,
		Prompt:      "只回答两个字：收到",
		Model:       model,
	})
	if fresh.Err != nil {
		t.Fatalf("fresh turn on legacy db failed: %v", fresh.Err)
	}
	if fresh.ProviderSessionID == "" {
		t.Fatal("fresh turn produced no provider session id")
	}
	t.Logf("fresh session=%s", fresh.ProviderSessionID)

	// 2) 老会话续跑：跨版本 resume 失败可接受（记日志），但不能崩、不能丢库。
	resumed := runUpgradeTurn(t, invoker, handler, cli.InvocationRequest{
		ExecutionID: "upgrade-resume",
		WorkDir:     workDir,
		Prompt:      "只回答两个字：收到",
		Model:       model,
		Resume:      true,
		SessionID:   legacySession,
	})
	if resumed.Err != nil {
		t.Logf("legacy resume surfaced error (acceptable if session incompatible): %v", resumed.Err)
	} else if resumed.ProviderSessionID != legacySession {
		// v2 用 session_v2 表，v1 会话无法续跑时运行层会新建会话并打 resume_fallback。
		t.Logf("legacy resume degraded to new session (expected for v1 sessions): %s -> %s",
			legacySession, resumed.ProviderSessionID)
	} else {
		t.Logf("legacy resume reused original session=%s message=%q", resumed.ProviderSessionID, resumed.Message)
	}

	// 3) 老会话必须一条不少；新会话可以落在 session 或 session_v2，总数不得变少。
	after, _, err := countSessions(dbPath)
	if err != nil {
		t.Fatalf("read sessions after upgrade: %v", err)
	}
	if after.legacy < before.legacy {
		t.Fatalf("legacy session rows shrank after upgrade: %d -> %d", before.legacy, after.legacy)
	}
	// v2 会把 v1 会话回填进 session_v2；老会话必须能在新表里找到，才算"升级不丢数据"。
	present, err := sessionExists(dbPath, "session_v2", legacySession)
	if err != nil {
		t.Fatalf("check legacy session in session_v2: %v", err)
	}
	if !present {
		t.Fatalf("legacy session %s missing from session_v2 after upgrade", legacySession)
	}
	t.Logf("after upgrade: session=%d session_v2=%d (before session=%d session_v2=%d) legacy_backfilled=%v",
		after.legacy, after.v2, before.legacy, before.v2, present)
}

func runUpgradeTurn(
	t *testing.T,
	invoker *ACPInvoker,
	handler *smokeHandler,
	req cli.InvocationRequest,
) cli.InvocationResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("invoke %s: %v", req.ExecutionID, err)
	}
	defer handle.Process.Stop()
	return consumeTurn(t, ctx, handle, req, handler)
}

// consumeTurn 独立于 smoke 的消费逻辑，避免探针之间相互依赖细节。
func consumeTurn(
	t *testing.T,
	ctx context.Context,
	handle *cli.Invocation,
	req cli.InvocationRequest,
	handler *smokeHandler,
) cli.InvocationResult {
	t.Helper()
	observer := &smokeObserver{}
	result, err := cli.ConsumeEvents(ctx, observer, handle, req.ExecutionID, "probe", handler, handler)
	if err != nil && result.Err == nil {
		result.Err = err
	}
	return result
}

// sessionCounts 分别记录 v1 表与 v2 表的会话数：
// v2.1.4 打开老库时会把 session 整表回填到 session_v2，两者相加会重复计数，
// 因此必须分开看。
type sessionCounts struct {
	legacy int
	v2     int
}

func countSessions(dbPath string) (sessionCounts, string, error) {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return sessionCounts{}, "", err
	}
	defer db.Close()

	var counts sessionCounts
	if err := db.QueryRow("select count(*) from session").Scan(&counts.legacy); err != nil {
		return sessionCounts{}, "", err
	}
	if err := db.QueryRow("select count(*) from session_v2").Scan(&counts.v2); err != nil {
		return sessionCounts{}, "", err
	}
	var sample string
	err = db.QueryRow("select id from session order by time_created limit 1").Scan(&sample)
	if err == sql.ErrNoRows {
		return counts, "", nil
	}
	return counts, sample, err
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// sessionExists 判断指定会话是否仍在库里（升级不得丢老数据）。
func sessionExists(dbPath, table, sessionID string) (bool, error) {
	if sessionID == "" {
		return false, nil
	}
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return false, err
	}
	defer db.Close()
	var found int
	err = db.QueryRow("select count(*) from "+table+" where id = ?", sessionID).Scan(&found)
	return found > 0, err
}
