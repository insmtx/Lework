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
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// requireCloseReason 断言对端关闭连接，且关闭原因与固定文案完全一致。
//
// 固定原因是对 agent 的可编程契约（决定提示用户重试还是重新配对），只断言
// 「读失败」会让文案漂移悄悄通过。
func requireCloseReason(t *testing.T, client *transport.Client, want string) {
	t.Helper()

	err := requireClosed(t, client)
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read error = %v, want a websocket close error", err)
	}
	if closeErr.Text != want {
		t.Fatalf("close reason = %q, want %q", closeErr.Text, want)
	}
}

// requireClosed 等待服务端关闭连接，把「连接一直开着直到测试读超时」与真正的
// 关闭区分开，避免超时被误当成通过。
func requireClosed(t *testing.T, client *transport.Client) error {
	t.Helper()

	err := readUntilError(t, client)
	if err == nil {
		t.Fatal("connection stayed open, want it closed")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection was not closed before the read deadline: %v", err)
	}
	return err
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
	// 订阅可能已经被「复查发现已认领、提前投递并关闭连接」移除；那同样是一次
	// 有效投递，调用方紧接着会读到 pair.result。因此这里不把并发时序当成失败，
	// 真正没投递会在随后的读帧超时里暴露。
	env.coreSub.publishCore(context.Background(), subject, nil)
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
	// 先注册恢复，再写 sentinel 并断言：断言失败时也要把全局日志配置还原，
	// 否则后续用例会把内容写进这个临时文件或丢失控制台输出。
	t.Cleanup(func() {
		_ = logs.Sync()
		reload(logsconfig.LogsConfig{
			"default": {{Writer: "console", Encoder: "default", Level: zapcore.InfoLevel}},
		})
	})
	logs.Infof("%s", pairLogSentinel)
	_ = logs.Sync()

	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(pairLogSentinel)) {
		t.Fatalf("log capture is not active (err %v): %s", err, data)
	}
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
	if resultFrame.ID != "hello-pair" {
		t.Fatalf("pair.result id = %q, want the hello id %q", resultFrame.ID, "hello-pair")
	}
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

// TestPairReconnectAfterDeliveryIsRejected 固化「pair_key 不是永久造凭证能力」：
// 首次投递后节点已有凭证，同 pair_key 重连必须被拒绝（agent 应提示重新配对），
// 旧凭证继续有效，不能被重投轮换掉。
func TestPairReconnectAfterDeliveryIsRejected(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	pairKey := "pair-key-no-rotate"
	_, _, first := completePairFlow(t, env, pairKey, "work-mac", pairTestUser)

	// 原连接已经被服务端关闭；用同一个 pair_key 再连一次必须被拒绝。
	secondClient, err := dialPair(t, env, pairAuthHeader(pairKey), newPairHello("work-mac", "pair", pairKey, ""))
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	requireCloseReason(t, secondClient, credentialAlreadyIssuedReason)

	rec, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(first.Credential))
	if err != nil {
		t.Fatalf("lookup old credential: %v", err)
	}
	if !found || rec.ID != first.NodeID {
		t.Fatalf("old credential resolves to %+v found %v, want node %q", rec, found, first.NodeID)
	}
}

// TestPairClaimedWithoutCredentialRedelivers 覆盖补发的唯一允许条件：认领成功、
// 节点已建好但还没有凭证（实例在签发前挂掉）。直接构造这个库状态，不依赖
// 原连接的重查时序；重连必须拿到凭证，否则节点永远停在未激活状态。
func TestPairClaimedWithoutCredentialRedelivers(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	pairKey := "pair-key-crash-before-issue"
	created, err := env.store.CreateClaim(ctx, store.NodeClaim{
		PairKey:   pairKey,
		Code:      "AB234",
		NodeName:  "work-mac",
		OS:        "linux",
		Arch:      "amd64",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}
	node, err := env.store.BindClaimToUser(ctx, created.ID, pairTestUser, "work-mac")
	if err != nil {
		t.Fatalf("BindClaimToUser: %v", err)
	}
	stored, found, err := env.store.NodeByID(ctx, node.ID)
	if err != nil || !found {
		t.Fatalf("NodeByID(%q) = %v, %v", node.ID, found, err)
	}
	if stored.CredentialHash != "" {
		t.Fatalf("test setup: node already has a credential")
	}

	client, err := dialPair(t, env, pairAuthHeader(pairKey), newPairHello("work-mac", "pair", pairKey, ""))
	if err != nil {
		t.Fatalf("redelivery dial: %v", err)
	}
	result := readPairResult(t, client)
	if result.NodeID != node.ID || result.Credential == "" {
		t.Fatalf("pair.result = %+v, want node %q and a credential", result, node.ID)
	}

	rec, found, err := env.store.LookupNodeByCredential(ctx, authn.HashToken(result.Credential))
	if err != nil {
		t.Fatalf("LookupNodeByCredential: %v", err)
	}
	if !found || rec.ID != node.ID {
		t.Fatalf("credential resolves to %+v found %v, want node %q", rec, found, node.ID)
	}
}

// TestPairSecondConnectionReplacesOlderForSameClaim 固化 Important 1：同一个 claim
// 只认一条当前连接。后到的连接接管后，前一条只收到替换通知，拿不到 pair.result，
// 库里的 hash 必须正是后一条拿到的凭证。
func TestPairSecondConnectionReplacesOlderForSameClaim(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	pairKey := "pair-key-replace"
	first, err := dialPairMode(t, env, pairKey, "work-mac")
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	firstPending := readPairPending(t, first)

	// 两次 await 分别确认两条连接的订阅已经生效：第一条的 made 先被消费，
	// 第二次等待的必然是后到连接，这样下面 publish 不会落进订阅生效前的窗口。
	subject := mustPairSubject(t, firstPending.ClaimID)
	awaitCoreSubject(t, env.coreSub, subject)

	second, err := dialPairMode(t, env, pairKey, "work-mac")
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	secondPending := readPairPending(t, second)
	if secondPending.ClaimID != firstPending.ClaimID {
		t.Fatalf("second claim id = %q, want %q", secondPending.ClaimID, firstPending.ClaimID)
	}
	awaitCoreSubject(t, env.coreSub, subject)

	claim, node := bindPairClaim(t, env, pairKey, pairTestUser, "work-mac")
	if claim.ID != firstPending.ClaimID {
		t.Fatalf("claim id = %q, want %q", claim.ID, firstPending.ClaimID)
	}
	if !env.coreSub.publishCore(ctx, subject, nil) {
		t.Fatalf("no Core subscription for %q", subject)
	}

	result := readPairResult(t, second)
	if result.NodeID != node.ID || result.Credential == "" {
		t.Fatalf("pair.result = %+v, want node %q and a credential", result, node.ID)
	}

	// 第一条只能看到替换原因，任何 pair.result 都应让它读到帧而不是错误。
	requireCloseReason(t, first, pairConnReplacedReason)

	rec, found, err := env.store.NodeByID(ctx, node.ID)
	if err != nil {
		t.Fatalf("NodeByID: %v", err)
	}
	if !found {
		t.Fatalf("node %q disappeared", node.ID)
	}
	if rec.CredentialHash != authn.HashToken(result.Credential) {
		t.Fatalf("stored credential hash does not match the credential delivered to the second connection")
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

// TestPairClaimModeLimitsFailuresPerIP 覆盖猜码的 IP 预算：同一来源第 11 次
// 失败被拒，并使用固定原因，agent 可据此停止重试而不是无限轮询码空间。
func TestPairClaimModeLimitsFailuresPerIP(t *testing.T) {
	env := newGatewayEnv(t)

	for i := 0; i < claimAttemptsPerIP; i++ {
		client, err := dialPair(t, env, pairAuthHeader("claim-client"),
			newPairHello("work-mac", "claim", "claim-key-ip", "ZZZZZ"))
		if err != nil {
			t.Fatalf("claim attempt #%d: %v", i, err)
		}
		requireCloseReason(t, client, "claim code unavailable")
	}

	client, err := dialPair(t, env, pairAuthHeader("claim-client"),
		newPairHello("work-mac", "claim", "claim-key-ip", "ZZZZZ"))
	if err != nil {
		t.Fatalf("over-limit dial: %v", err)
	}
	requireCloseReason(t, client, claimAttemptsExceededReason)
}

// TestPairClaimModeLimitsFailuresPerPairKey 覆盖同一 agent 连续换码的预算。
// 抬高 IP 上限是为了隔离维度：这里只允许 pair_key 计数先到上限。
func TestPairClaimModeLimitsFailuresPerPairKey(t *testing.T) {
	env := newGatewayEnv(t)
	env.gateway.claimAttempts = newClaimAttemptLimiter(100, claimAttemptsPerKey, time.Minute)

	for i := 0; i < claimAttemptsPerKey; i++ {
		client, err := dialPair(t, env, pairAuthHeader("claim-client"),
			newPairHello("work-mac", "claim", "claim-key-fixed", "ZZZZZ"))
		if err != nil {
			t.Fatalf("claim attempt #%d: %v", i, err)
		}
		requireCloseReason(t, client, "claim code unavailable")
	}

	client, err := dialPair(t, env, pairAuthHeader("claim-client"),
		newPairHello("work-mac", "claim", "claim-key-fixed", "ZZZZZ"))
	if err != nil {
		t.Fatalf("over-limit dial: %v", err)
	}
	requireCloseReason(t, client, claimAttemptsExceededReason)
}

// TestPairClaimModeSuccessDoesNotConsumeIPBudget 证明成功不消耗 IP 配额：
// 一次成功之后，同一 IP 仍能完整用完失败预算，下一轮才被拒。若成功也计数，
// 第 10 次失败就会提前触发限流。
func TestPairClaimModeSuccessDoesNotConsumeIPBudget(t *testing.T) {
	env := newGatewayEnv(t)
	ctx := context.Background()

	if _, err := env.store.CreateClaim(ctx, store.NodeClaim{
		PairKey:   "api-issued-success",
		Code:      "AB234",
		NodeName:  "placeholder",
		OS:        "linux",
		Arch:      "amd64",
		ClaimedBy: pairTestUser,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	success, err := dialPair(t, env, pairAuthHeader("claim-client"),
		newPairHello("work-mac", "claim", "", "ab234"))
	if err != nil {
		t.Fatalf("dial successful claim: %v", err)
	}
	if result := readPairResult(t, success); result.Credential == "" {
		t.Fatal("successful claim carried an empty credential")
	}

	for i := 0; i < claimAttemptsPerIP; i++ {
		client, err := dialPair(t, env, pairAuthHeader("claim-client"),
			newPairHello("work-mac", "claim", fmt.Sprintf("claim-miss-%d", i), "ZZZZZ"))
		if err != nil {
			t.Fatalf("failed claim #%d: %v", i, err)
		}
		requireCloseReason(t, client, "claim code unavailable")
	}

	over, err := dialPair(t, env, pairAuthHeader("claim-client"),
		newPairHello("work-mac", "claim", "claim-miss-over", "ZZZZZ"))
	if err != nil {
		t.Fatalf("over-limit dial: %v", err)
	}
	requireCloseReason(t, over, claimAttemptsExceededReason)
}

// TestPendingPairIdleTimeoutClosesConnection 用缩短的空闲窗口验证：没有入站帧
// 的连接会被关闭，不会一直占着配额。
func TestPendingPairIdleTimeoutClosesConnection(t *testing.T) {
	oldIdle := pendingIdleTimeout
	pendingIdleTimeout = 200 * time.Millisecond
	t.Cleanup(func() { pendingIdleTimeout = oldIdle })

	env := newGatewayEnv(t)
	client, err := dialPairMode(t, env, "pair-key-idle", "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	readPairPending(t, client)

	start := time.Now()
	requireClosed(t, client)
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("idle connection closed after %v, want about %v", elapsed, pendingIdleTimeout)
	}
}

// TestPendingPairMaxLifetimeIgnoresHeartbeats 证明持续发帧不能延长绝对生存期：
// 心跳不断续上空闲窗口，连接仍必须在建立的绝对期限到达时关闭。
func TestPendingPairMaxLifetimeIgnoresHeartbeats(t *testing.T) {
	oldIdle, oldMax := pendingIdleTimeout, pendingMaxLifetime
	pendingIdleTimeout = time.Second
	pendingMaxLifetime = 500 * time.Millisecond
	t.Cleanup(func() {
		pendingIdleTimeout = oldIdle
		pendingMaxLifetime = oldMax
	})

	env := newGatewayEnv(t)
	client, err := dialPairMode(t, env, "pair-key-lifetime", "work-mac")
	if err != nil {
		t.Fatalf("dial pending connection: %v", err)
	}
	readPairPending(t, client)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				err := client.Write(ctx, protocol.Frame{Type: protocol.FrameHeartbeat})
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
	}()

	start := time.Now()
	requireClosed(t, client)
	elapsed := time.Since(start)
	if elapsed < 350*time.Millisecond {
		t.Fatalf("connection closed after %v, before the absolute lifetime", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("heartbeats extended the absolute lifetime: closed after %v", elapsed)
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
