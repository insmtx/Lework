package agentgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	logsconfig "github.com/ygpkg/yg-go/config"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// pairTestUser 是配对流程里出码/认领的账号。
	pairTestUser = "user-pair-test"

	// pairLogSentinel 证明日志捕获真实生效，避免「日志里没有凭证」成为空断言。
	pairLogSentinel = "agentgw-pair-log-sentinel"
)

// pairHelloParams 是待定连接握手帧的参数，字段与 agent 侧的 mode/pair_key 对齐。
type pairHelloParams struct {
	NodeName     string `json:"node_name"`
	AgentVersion string `json:"agent_version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Mode         string `json:"mode"`
	PairKey      string `json:"pair_key"`
	Code         string `json:"code"`
}

// pairPendingInfo 是 pair.pending 帧的结果。
type pairPendingInfo struct {
	ClaimID string `json:"claim_id"`
	Code    string `json:"code"`
}

// pairResultInfo 是 pair.result 帧的结果；Credential 是本流程唯一的明文出口。
type pairResultInfo struct {
	NodeID     string `json:"node_id"`
	Credential string `json:"credential"`
}

func newPairHello(name, mode, pairKey, code string) pairHelloParams {
	return pairHelloParams{
		NodeName:     name,
		AgentVersion: "dev",
		OS:           "linux",
		Arch:         "amd64",
		Mode:         mode,
		PairKey:      pairKey,
		Code:         code,
	}
}

// pairAuthHeader 构造待定连接的握手头。value 为空时仍然写出方案名，用来覆盖
// 「Pair 后面没有值」的拒绝分支。
func pairAuthHeader(value string) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Pair "+value)
	return header
}

func writePairHello(t *testing.T, client *transport.Client, params pairHelloParams) {
	t.Helper()

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode pair hello: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHello, ID: "hello-pair", Params: raw}); err != nil {
		t.Fatalf("write pair hello: %v", err)
	}
}

// dialPair 建立一条待定连接并发送 hello，返回客户端与拔号错误。
// hello 写入失败同样返回错误，调用方据此断言握手被拒绝。
func dialPair(t *testing.T, env *gatewayEnv, header http.Header, params pairHelloParams) (*transport.Client, error) {
	t.Helper()

	client, err := env.dial(t, header)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = client.Close() })

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode pair hello: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHello, ID: "hello-pair", Params: raw}); err != nil {
		return nil, fmt.Errorf("write pair hello: %w", err)
	}
	return client, nil
}

// dialPairMode 用 pair 模式建立待定连接：Pair 头与 hello 的 pair_key 相同。
func dialPairMode(t *testing.T, env *gatewayEnv, pairKey, name string) (*transport.Client, error) {
	t.Helper()

	return dialPair(t, env, pairAuthHeader(pairKey), newPairHello(name, "pair", pairKey, ""))
}

// readFrameOfType 读取下一帧并断言类型。
func readFrameOfType(t *testing.T, client *transport.Client, want protocol.FrameType) protocol.Frame {
	t.Helper()

	frame := readFrame(t, client)
	if frame.Type != want {
		t.Fatalf("frame type = %q, want %q", frame.Type, want)
	}
	return frame
}

func readPairPending(t *testing.T, client *transport.Client) pairPendingInfo {
	t.Helper()

	frame := readFrameOfType(t, client, protocol.FramePairPending)
	var info pairPendingInfo
	if err := json.Unmarshal(frame.Result, &info); err != nil {
		t.Fatalf("decode pair.pending %s: %v", frame.Result, err)
	}
	return info
}

func readPairResult(t *testing.T, client *transport.Client) pairResultInfo {
	t.Helper()

	frame := readFrameOfType(t, client, protocol.FramePairResult)
	var info pairResultInfo
	if err := json.Unmarshal(frame.Result, &info); err != nil {
		t.Fatalf("decode pair.result %s: %v", frame.Result, err)
	}
	return info
}

// bindPairClaim 走生产路径认领一条待定记录：BindClaimToUser 同时建节点与
// owner 归属行，gateway 的投递要求 claim.NodeID 已经写好。
func bindPairClaim(t *testing.T, env *gatewayEnv, pairKey, userID, nodeName string) (store.NodeClaim, store.Node) {
	t.Helper()

	ctx := context.Background()
	claim, ok, err := env.store.ClaimByPairKey(ctx, pairKey)
	if err != nil {
		t.Fatalf("ClaimByPairKey(%q): %v", pairKey, err)
	}
	if !ok {
		t.Fatalf("claim for pair key %q was not persisted", pairKey)
	}
	node, err := env.store.BindClaimToUser(ctx, claim.ID, userID, nodeName)
	if err != nil {
		t.Fatalf("BindClaimToUser(%q): %v", claim.ID, err)
	}
	return claim, node
}

// awaitCoreSubject 等待网关建立指定 subject 的 Core 订阅。
//
// 订阅 handler 在写入 made 之前已经注册到假总线里，因此等到它就能确定
// 后续 publish 不会落在「订阅尚未生效」的窗口里。
func awaitCoreSubject(t *testing.T, sub *noopCoreSub, subject string) coreSubscription {
	t.Helper()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case made := <-sub.made:
			if made.subject == subject {
				return made
			}
		case <-deadline:
			t.Fatalf("gateway did not subscribe to %q", subject)
		}
	}
}

// notifyClaim 通过假总线的 Core handler 发布认领通知，模拟 API 实例在事务
// 提交后只带 claim id 的 Core NATS 消息。
func notifyClaim(t *testing.T, env *gatewayEnv, claimID string) {
	t.Helper()

	subject := mustPairSubject(t, claimID)
	awaitCoreSubject(t, env.coreSub, subject)
	if !env.coreSub.publishCore(context.Background(), subject, nil) {
		t.Fatalf("no Core subscription for %q", subject)
	}
}

func mustPairSubject(t *testing.T, claimID string) string {
	t.Helper()

	subject, err := messaging.ControlPlanePairSubject(claimID)
	if err != nil {
		t.Fatalf("pair subject for %q: %v", claimID, err)
	}
	return subject
}

// completePairFlow 跑完「建待定连接 → 用户认领 → 下发凭证」的主路径。
func completePairFlow(
	t *testing.T,
	env *gatewayEnv,
	pairKey, nodeName, userID string,
) (*transport.Client, pairPendingInfo, pairResultInfo) {
	t.Helper()

	client, err := dialPair(t, env, pairAuthHeader(pairKey), newPairHello(nodeName, "pair", pairKey, ""))
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	pending := readPairPending(t, client)
	if pending.ClaimID == "" {
		t.Fatal("pair.pending has no claim id")
	}

	claim, node := bindPairClaim(t, env, pairKey, userID, nodeName)
	if claim.ID != pending.ClaimID {
		t.Fatalf("claim id = %q, want %q from pair.pending", claim.ID, pending.ClaimID)
	}
	notifyClaim(t, env, pending.ClaimID)

	result := readPairResult(t, client)
	if result.NodeID != node.ID {
		t.Fatalf("pair.result node id = %q, want %q", result.NodeID, node.ID)
	}
	return client, pending, result
}

// capturePairLogs 把默认日志重定向到临时文件，并在测试结束时恢复控制台输出。
func capturePairLogs(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agentgw-pair.log")
	reload := func(cfg logsconfig.LogsConfig) {
		if err := logs.ReloadConfig("-", cfg); err != nil {
			t.Fatalf("reload logs: %v", err)
		}
	}
	reload(logsconfig.LogsConfig{
		"default": {{
			Writer:  "file",
			Encoder: "default",
			Level:   zapcore.InfoLevel,
			Logger:  &lumberjack.Logger{Filename: path},
		}},
	})
	logs.Infof("%s", pairLogSentinel)
	_ = logs.Sync()

	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(pairLogSentinel)) {
		t.Fatalf("log capture is not active (err %v): %s", err, data)
	}
	t.Cleanup(func() {
		_ = logs.Sync()
		reload(logsconfig.LogsConfig{
			"default": {{Writer: "console", Encoder: "default", Level: zapcore.InfoLevel}},
		})
	})
	return path
}

func TestPairRejectsEmptyHandshakeValue(t *testing.T) {
	env := newGatewayEnv(t)

	for name, header := range map[string]http.Header{
		"empty value": pairAuthHeader(""),
		"no space":    {"Authorization": []string{"Pair"}},
	} {
		t.Run(name, func(t *testing.T) {
			if client, err := env.dial(t, header); err == nil {
				_ = client.Close()
				t.Fatal("dial succeeded with an empty Pair handshake value")
			}
		})
	}
}

func TestPairHandshakeCreatesPendingClaim(t *testing.T) {
	env := newGatewayEnv(t)

	client, err := dialPairMode(t, env, "pair-key-create", "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	pending := readPairPending(t, client)

	if len(pending.Code) < 4 {
		t.Fatalf("claim code = %q, want at least 4 characters", pending.Code)
	}
	if pending.Code != strings.ToUpper(pending.Code) {
		t.Fatalf("claim code = %q, want upper case", pending.Code)
	}

	claim, ok, err := env.store.ClaimByPairKey(context.Background(), "pair-key-create")
	if err != nil {
		t.Fatalf("ClaimByPairKey: %v", err)
	}
	if !ok {
		t.Fatal("pending claim was not persisted")
	}
	if claim.ID != pending.ClaimID {
		t.Fatalf("stored claim id = %q, want %q", claim.ID, pending.ClaimID)
	}
	if claim.Code != pending.Code {
		t.Fatalf("stored code = %q, want %q from pair.pending", claim.Code, pending.Code)
	}
	if claim.Status != store.ClaimStatusPending {
		t.Fatalf("claim status = %q, want %q", claim.Status, store.ClaimStatusPending)
	}
	if claim.NodeName != "work-mac" || claim.OS != "linux" || claim.Arch != "amd64" {
		t.Fatalf("claim machine info = %+v, want the hello metadata", claim)
	}

	// 待定连接不是节点：注册表必须仍然为空。
	if infos := env.registry.List(); len(infos) != 0 {
		t.Fatalf("registry nodes = %+v, want none for a pending connection", infos)
	}
}

// TestPairReconnectReusesPendingClaim 固化「码稳定」：同一个 pair_key 重连必须
// 拿回同一行、同一个码，否则终端上的码会随网络抖动失效。
func TestPairReconnectReusesPendingClaim(t *testing.T) {
	env := newGatewayEnv(t)

	first, err := dialPairMode(t, env, "pair-key-stable", "work-mac")
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	firstPending := readPairPending(t, first)

	second, err := dialPairMode(t, env, "pair-key-stable", "work-mac")
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	secondPending := readPairPending(t, second)

	if secondPending.ClaimID != firstPending.ClaimID {
		t.Fatalf("reconnect claim id = %q, want %q", secondPending.ClaimID, firstPending.ClaimID)
	}
	if secondPending.Code != firstPending.Code {
		t.Fatalf("reconnect code = %q, want the stable %q", secondPending.Code, firstPending.Code)
	}
}

func TestPairFlowDeliversCredentialOverTheSameConnection(t *testing.T) {
	env := newGatewayEnv(t)
	logPath := capturePairLogs(t)
	ctx := context.Background()

	pairKey := "pair-key-deliver"
	client, err := dialPairMode(t, env, pairKey, "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	pendingFrame := readFrameOfType(t, client, protocol.FramePairPending)
	var pending pairPendingInfo
	if err := json.Unmarshal(pendingFrame.Result, &pending); err != nil {
		t.Fatalf("decode pair.pending: %v", err)
	}
	if len(pending.Code) < 4 {
		t.Fatalf("claim code = %q, want at least 4 characters", pending.Code)
	}

	claim, node := bindPairClaim(t, env, pairKey, pairTestUser, "work-mac")
	if claim.ID != pending.ClaimID {
		t.Fatalf("claim id = %q, want %q", claim.ID, pending.ClaimID)
	}
	notifyClaim(t, env, claim.ID)

	resultFrame := readFrameOfType(t, client, protocol.FramePairResult)
	var result pairResultInfo
	if err := json.Unmarshal(resultFrame.Result, &result); err != nil {
		t.Fatalf("decode pair.result %s: %v", resultFrame.Result, err)
	}
	if result.Credential == "" {
		t.Fatal("pair.result carried an empty credential")
	}
	if result.NodeID != node.ID {
		t.Fatalf("pair.result node id = %q, want %q", result.NodeID, node.ID)
	}

	// 凭证只允许出现在 pair.result：pair.pending 与之前所有帧都不得包含它。
	pendingRaw, err := json.Marshal(pendingFrame)
	if err != nil {
		t.Fatalf("encode pair.pending: %v", err)
	}
	if bytes.Contains(pendingRaw, []byte(result.Credential)) {
		t.Fatalf("pair.pending leaks the credential: %s", pendingRaw)
	}
	if strings.Contains(pending.Code, result.Credential) {
		t.Fatal("claim code equals the credential")
	}

	rec, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(result.Credential))
	if err != nil {
		t.Fatalf("LookupNodeByCredential: %v", err)
	}
	if !found || rec.ID != node.ID {
		t.Fatalf("credential resolves to %+v found %v, want node %q", rec, found, node.ID)
	}

	// 投递完成后服务端必须主动断开：agent 会带着凭证重新走普通节点路径。
	if err := readUntilError(t, client); err == nil {
		t.Fatal("pending connection stayed open after pair.result")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending connection was not closed within the read deadline: %v", err)
	}

	// 日志是全局的：明文凭证、pair_key、短码都不得出现。
	_ = logs.Sync()
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read captured logs: %v", err)
	}
	for label, secret := range map[string]string{
		"credential": result.Credential,
		"pair key":   pairKey,
		"claim code": pending.Code,
	} {
		if bytes.Contains(logged, []byte(secret)) {
			t.Fatalf("logs leak the %s: %s", label, logged)
		}
	}
}

// TestPairRedeliveryIssuesFreshCredential 证明重连复用同一个节点但换一份新凭证：
// agent 只有在丢了凭证时才会重连，旧凭证必须随之失效。
func TestPairRedeliveryIssuesFreshCredential(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	pairKey := "pair-key-redeliver"
	_, _, first := completePairFlow(t, env, pairKey, "work-mac", pairTestUser)

	// 原连接已经被服务端关闭；用同一个 pair_key 再连一次会立刻拿到新凭证。
	secondClient, err := dialPair(t, env, pairAuthHeader(pairKey), newPairHello("work-mac", "pair", pairKey, ""))
	if err != nil {
		t.Fatalf("redelivery dial: %v", err)
	}
	second := readPairResult(t, secondClient)

	if second.NodeID != first.NodeID {
		t.Fatalf("redelivery node id = %q, want %q", second.NodeID, first.NodeID)
	}
	if second.Credential == "" || second.Credential == first.Credential {
		t.Fatalf("redelivery credential = %q, want a fresh value", second.Credential)
	}

	if _, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(first.Credential)); err != nil {
		t.Fatalf("lookup old credential: %v", err)
	} else if found {
		t.Fatal("the previous credential still resolves after redelivery")
	}
	rec, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(second.Credential))
	if err != nil {
		t.Fatalf("lookup new credential: %v", err)
	}
	if !found || rec.ID != first.NodeID {
		t.Fatalf("new credential resolves to %+v found %v, want node %q", rec, found, first.NodeID)
	}
}

// TestPairClaimModeBindsWithoutConfirmation 覆盖 `lework agent --code`：码是账号
// 事先出的，pending 行的 ClaimedBy 已指向该账号，agent 出示码即完成绑定。
func TestPairClaimModeBindsWithoutConfirmation(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	issued, err := env.store.CreateClaim(ctx, store.NodeClaim{
		PairKey:    "api-issued-claim",
		Code:       "AB234",
		NodeName:   "placeholder",
		OS:         "linux",
		Arch:       "amd64",
		ClaimedBy:  pairTestUser,
		ExpiresAt:  time.Now().UTC().Add(10 * time.Minute),
		InstanceID: "api-instance",
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	client, err := dialPair(t, env, pairAuthHeader("claim-client"), newPairHello("work-mac", "claim", "", "ab234"))
	if err != nil {
		t.Fatalf("dial claim connection: %v", err)
	}

	result := readPairResult(t, client)
	if result.NodeID == "" || result.Credential == "" {
		t.Fatalf("pair.result = %+v, want node id and credential", result)
	}

	rec, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(result.Credential))
	if err != nil {
		t.Fatalf("LookupNodeByCredential: %v", err)
	}
	if !found || rec.ID != result.NodeID {
		t.Fatalf("credential resolves to %+v found %v, want node %q", rec, found, result.NodeID)
	}
	if rec.Name != "work-mac" || rec.OS != "linux" || rec.Arch != "amd64" {
		t.Fatalf("node metadata = %+v, want the hello machine info", rec)
	}

	role, ok, err := env.store.NodeRole(ctx, result.NodeID, pairTestUser)
	if err != nil {
		t.Fatalf("NodeRole: %v", err)
	}
	if !ok || role != store.NodeRoleOwner {
		t.Fatalf("role = %q found %v, want owner for the issuing account", role, ok)
	}

	// 码是 pending 行上的绑定关系，不需要二次认领。
	bound, ok, err := env.store.ClaimByPairKey(ctx, issued.PairKey)
	if err != nil {
		t.Fatalf("ClaimByPairKey: %v", err)
	}
	if !ok || bound.Status != store.ClaimStatusClaimed || bound.NodeID != result.NodeID {
		t.Fatalf("claim after claim-mode pairing = %+v found %v", bound, ok)
	}
}

func TestPairClaimModeRejectsUnusableCode(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, env *gatewayEnv)
		code    string
	}{
		{
			name: "unknown code",
			prepare: func(t *testing.T, env *gatewayEnv) {
			},
			code: "ZZZZZ",
		},
		{
			name: "expired code",
			prepare: func(t *testing.T, env *gatewayEnv) {
				_, err := env.store.CreateClaim(context.Background(), store.NodeClaim{
					PairKey:   "expired-code-key",
					Code:      "CD345",
					NodeName:  "old-box",
					ClaimedBy: pairTestUser,
					ExpiresAt: time.Now().UTC().Add(-time.Minute),
				})
				if err != nil {
					t.Fatalf("CreateClaim(expired): %v", err)
				}
			},
			code: "CD345",
		},
		{
			name: "no claiming account",
			prepare: func(t *testing.T, env *gatewayEnv) {
				_, err := env.store.CreateClaim(context.Background(), store.NodeClaim{
					PairKey:   "unowned-code-key",
					Code:      "EF456",
					NodeName:  "old-box",
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				})
				if err != nil {
					t.Fatalf("CreateClaim(pending): %v", err)
				}
			},
			code: "EF456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newGatewayEnv(t)
			tt.prepare(t, env)

			client, err := dialPair(t, env, pairAuthHeader("claim-client"), newPairHello("work-mac", "claim", "", tt.code))
			if err != nil {
				t.Fatalf("dial claim connection: %v", err)
			}
			if err := readUntilError(t, client); err == nil {
				t.Fatal("connection stayed open for an unusable claim code")
			}
		})
	}
}

// TestPairClaimedWithoutNodeClosesWithoutCreatingNode 覆盖内部不一致：认领行已经
// claimed 但 node_id 为空（实例在 BindClaimToUser 之前挂掉）。投递没有可归属的
// 节点，连接必须关闭，且不能顺手新建一个节点。
func TestPairClaimedWithoutNodeClosesWithoutCreatingNode(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	created, err := env.store.CreateClaim(ctx, store.NodeClaim{
		PairKey:   "pair-key-orphan",
		Code:      "AB234",
		NodeName:  "work-mac",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}
	if _, ok, err := env.store.ClaimClaimForUser(ctx, created.ID, pairTestUser); err != nil || !ok {
		t.Fatalf("ClaimClaimForUser = %v, %v; want the row claimed", ok, err)
	}

	client, err := dialPairMode(t, env, "pair-key-orphan", "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	if err := readUntilError(t, client); err == nil {
		t.Fatal("connection stayed open for a claim without a node")
	}
	if nodes := listNodes(t, env.store); len(nodes) != 0 {
		t.Fatalf("stored nodes = %d, want none (delivery must not create a node)", len(nodes))
	}
}

// TestPairExpiredClaimClosesWithoutResult 覆盖 TTL 过期的 pending 行：不能在同一
// pair_key 上复活旧码，agent 必须换一个 pair_key 重新开始。
func TestPairExpiredClaimClosesWithoutResult(t *testing.T) {
	env := newGatewayEnv(t)

	if _, err := env.store.CreateClaim(context.Background(), store.NodeClaim{
		PairKey:   "pair-key-expired",
		Code:      "AB234",
		NodeName:  "old-box",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}

	client, err := dialPairMode(t, env, "pair-key-expired", "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	if err := readUntilError(t, client); err == nil {
		t.Fatal("expired claim was revived on the same pair key")
	}

	// 过期行没有被重写成 pending：重连依旧不可用。
	claim, ok, err := env.store.ClaimByPairKey(context.Background(), "pair-key-expired")
	if err != nil {
		t.Fatalf("ClaimByPairKey: %v", err)
	}
	if !ok || claim.Status != store.ClaimStatusExpired {
		t.Fatalf("claim = %+v found %v, want expired", claim, ok)
	}
}

// TestPairPendingConnectionsAreQuotaLimited 覆盖每 IP 的待定连接上限。
// httptest 客户端全部来自 127.0.0.1，因此第四条连接必须被拒绝。
func TestPairPendingConnectionsAreQuotaLimited(t *testing.T) {
	env := newGatewayEnv(t)

	for i := 0; i < maxPendingPerIP; i++ {
		pairKey := fmt.Sprintf("quota-key-%d", i)
		client, err := dialPairMode(t, env, pairKey, "work-mac")
		if err != nil {
			t.Fatalf("pending connection #%d: %v", i, err)
		}
		readPairPending(t, client)
	}

	if client, err := env.dial(t, pairAuthHeader("quota-key-overflow")); err == nil {
		_ = client.Close()
		t.Fatal("the pending connection over the per-IP quota was accepted")
	}

	// HTTP 面同样应给出限流状态，而不是 401（鉴权是通过的）。
	req, err := http.NewRequest(http.MethodGet, env.server.URL+"/api/agent/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = pairAuthHeader("quota-key-overflow")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
}

// TestPendingLimiterEnforcesCaps 单测覆盖两个上限与释放：3/IP、100/全局，
// 释放后名额可以再次获取。
func TestPendingLimiterEnforcesCaps(t *testing.T) {
	limiter := newPendingLimiter()

	for i := 0; i < maxPendingPerIP; i++ {
		if !limiter.acquire("10.0.0.1") {
			t.Fatalf("acquire #%d for 10.0.0.1 rejected below the per-IP cap", i)
		}
	}
	if limiter.acquire("10.0.0.1") {
		t.Fatal("per-IP cap was not enforced")
	}
	if !limiter.acquire("10.0.0.2") {
		t.Fatal("another IP was rejected although the per-IP cap only applies to 10.0.0.1")
	}
	limiter.release("10.0.0.1")
	if !limiter.acquire("10.0.0.1") {
		t.Fatal("released per-IP slot was not reusable")
	}

	// 解析失败（ip 为空）不按 IP 计数，但仍受全局上限约束。
	global := newPendingLimiter()
	for i := 0; i < maxPendingTotal; i++ {
		if !global.acquire("") {
			t.Fatalf("acquire #%d rejected below the global cap", i)
		}
	}
	if global.acquire("") {
		t.Fatal("global cap was not enforced")
	}
	if global.acquire("10.0.0.9") {
		t.Fatal("global cap was bypassed by a different IP")
	}
	global.release("")
	if !global.acquire("10.0.0.9") {
		t.Fatal("released global slot was not reusable")
	}
}

// TestPairIgnoresNonHeartbeatFrames 证明待定连接在读阶段只把帧当作存活信号：
// 心跳以外的内容一律忽略，不能因为一次乱码或提前发来的帧就断开配对。
func TestPairIgnoresNonHeartbeatFrames(t *testing.T) {
	env := newGatewayEnv(t)

	pairKey := "pair-key-junk"
	client, err := dialPairMode(t, env, pairKey, "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	readPairPending(t, client)

	writeRawFrame(t, client, protocol.Frame{
		Type:  protocol.FrameEvent,
		Event: &protocol.Event{Type: "session.created", Data: json.RawMessage(`{"junk":true}`)},
	})
	writeRawFrame(t, client, protocol.Frame{Type: protocol.FrameHeartbeat})

	claim, node := bindPairClaim(t, env, pairKey, pairTestUser, "work-mac")
	notifyClaim(t, env, claim.ID)

	result := readPairResult(t, client)
	if result.NodeID != node.ID {
		t.Fatalf("pair.result node id = %q, want %q", result.NodeID, node.ID)
	}
	if result.Credential == "" {
		t.Fatal("pair.result carried an empty credential")
	}
}
