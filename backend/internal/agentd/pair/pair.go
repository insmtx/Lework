// Package pair 实现节点配对的终端侧：发 hello、把短码交给调用方展示，并在
// 账号认领后取回控制面一次性下发的节点凭证。
//
// 凭证明文只以 Run 的返回值离开本包，不写日志、不进错误文本。配对信息用一份
// 0600 的 claim 文件持久化，供 `lework agent claim` 在终端刷屏后重印短码；
// 它包含短码（这是「终端不在了也能重印」的功能本身），但不包含凭证。
package pair

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/agentd/runner"
)

const (
	// claimFileName 是待定配对信息在凭证文件同目录下的固定文件名。
	claimFileName = "node.claim"

	// pairMode 让控制面建/复用一条待定认领（扫码主路径）。
	pairMode = "pair"

	// claimMode 让 agent 出示账号事先出的码（`lework agent --code`）。
	claimMode = "claim"

	// helloTimeout 限制握手帧的写阻塞：控制面黑洞时不能把启动卡死。
	helloTimeout = 10 * time.Second

	// heartbeatWriteTimeout 限制单次心跳写的阻塞时间。
	heartbeatWriteTimeout = 10 * time.Second
)

// 待定连接上的固定关闭原因。它们是控制面与 agent 之间的可编程契约：
// agent 据此决定「换一张新码重试」还是「停手让运维处理」。
var (
	// ErrPairingExpired 表示这张待定认领已过期，必须换一个 pair_key。
	ErrPairingExpired = errors.New("pairing expired")
	// ErrCredentialAlreadyIssued 表示节点已有凭证，绝不重投。
	ErrCredentialAlreadyIssued = errors.New("credential already issued")
	// ErrClaimCodeUnavailable 表示 `--code` 出示的码不存在、过期或已使用。
	ErrClaimCodeUnavailable = errors.New("claim code unavailable")
	// ErrClaimAttemptsExceeded 表示猜码失败次数超出控制面的预算。
	ErrClaimAttemptsExceeded = errors.New("claim code attempts exceeded")
	// ErrPairConnReplaced 表示同一认领已被更新的连接接管。
	ErrPairConnReplaced = errors.New("pair connection replaced")
	// ErrClaimWrite 表示 claim 文件写不下来。目录不可写会让后续的凭证落盘
	// 同样失败，因此调用方必须把它当成致命错误而不是可重试的断线。
	ErrClaimWrite = errors.New("write claim file")
)

// heartbeatInterval 是待定连接上发送空帧的间隔。
//
// 控制面 60 秒收不到入站帧就关闭待定连接；15 秒留出三次重试余量。做成变量
// 只为测试能缩短等待，生产代码不得在运行时改写它。
var heartbeatInterval = 15 * time.Second

// Conn 是配对连接的最小契约：一个读者、一个写者。
// *transport.Client 满足它；测试用脚本化假连接替换。
type Conn interface {
	Write(ctx context.Context, f protocol.Frame) error
	Read(ctx context.Context) (protocol.Frame, error)
}

// Options 描述一次配对尝试。
type Options struct {
	// NodeName/AgentVersion/WorkspaceRoots 上报给 hello，供浏览器确认页显示
	// 「正在认领的是哪台机器」。
	NodeName       string
	AgentVersion   string
	WorkspaceRoots []string
	// PairKey 标识这条待定连接；空值表示生成一个。
	PairKey string
	// Code 非空时切换为 mode=claim（`lework agent --code`）；空值为 mode=pair。
	Code string
	// ClaimPath 非空时写入可重印的 claim 文件。
	ClaimPath string
	// ControlPlaneURL 是 ws(s) 地址，用于拼出浏览器可打开的链接。
	ControlPlaneURL string
	// OnPending 在每次 pair.pending 时收到短码与
	// `lework://node/pair?...` 链接；可以为 nil。
	OnPending func(code, link string)
}

// Claim 是 `lework agent claim` 重印配对信息所需的最小状态。
//
// 它确实包含短码：终端刷屏后重印短码就是这条命令的用途。凭证不在其中。
type Claim struct {
	Link            string    `json:"link"`
	PairKey         string    `json:"pair_key"`
	Code            string    `json:"code"`
	ControlPlaneURL string    `json:"control_plane_url"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// Run 完成一次配对尝试，返回节点凭证明文。
//
// 它先写 hello，再同时收发：一个协程按 heartbeatInterval 发空帧维持待定连接
// （写入永远只有这一个协程，hello 在它启动前已完成），主协程读帧。返回前会
// 等心跳协程退出，因此调用方可以在 Run 返回后立刻 Close。
//
// 只有 pair.pending / pair.result 会被解码；未知帧类型（未来控制面新增的帧、
// 心跳、事件）一律忽略，不能让一次协议扩展打断配对。任何读错误都按控制面的
// 关闭原因归类后返回；断线重连由调用方负责。
func Run(ctx context.Context, conn Conn, opts Options) (string, error) {
	if conn == nil {
		return "", errors.New("agentd: pair: connection is nil")
	}

	pairKey := strings.TrimSpace(opts.PairKey)
	if pairKey == "" {
		generated, err := NewPairKey()
		if err != nil {
			return "", fmt.Errorf("agentd: pair: %w", err)
		}
		pairKey = generated
	}

	mode := pairMode
	code := strings.TrimSpace(opts.Code)
	if code != "" {
		mode = claimMode
	}

	params, err := json.Marshal(struct {
		Mode           string   `json:"mode"`
		PairKey        string   `json:"pair_key"`
		Code           string   `json:"code,omitempty"`
		NodeName       string   `json:"node_name"`
		AgentVersion   string   `json:"agent_version"`
		OS             string   `json:"os"`
		Arch           string   `json:"arch"`
		Capabilities   []string `json:"capabilities"`
		WorkspaceRoots []string `json:"workspace_roots"`
	}{
		Mode:           mode,
		PairKey:        pairKey,
		Code:           code,
		NodeName:       opts.NodeName,
		AgentVersion:   opts.AgentVersion,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Capabilities:   runner.Capabilities(),
		WorkspaceRoots: append([]string{}, opts.WorkspaceRoots...),
	})
	if err != nil {
		return "", fmt.Errorf("agentd: pair: encode hello params: %w", err)
	}

	helloCtx, cancelHello := context.WithTimeout(ctx, helloTimeout)
	err = conn.Write(helloCtx, protocol.Frame{
		Type:   protocol.FrameHello,
		ID:     fmt.Sprintf("pair-%d", time.Now().UnixNano()),
		Params: params,
	})
	cancelHello()
	if err != nil {
		return "", fmt.Errorf("agentd: pair: send hello: %w", err)
	}

	// 心跳与读循环共享这个上下文：心跳写失败时取消它，读循环立刻返回。
	// 间隔在心跳协程启动时读取一次，避免测试改写包级变量时与主协程竞争。返回前
	// 等心跳协程退出，把「调用方可以在 Run 之后 Close」写成显式契约。
	runCtx, cancelRun := context.WithCancel(ctx)
	writeErrs := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		heartbeatLoop(runCtx, conn, effectiveHeartbeatInterval(), writeErrs, cancelRun)
	}()
	defer func() {
		cancelRun()
		<-heartbeatDone
	}()

	for {
		frame, readErr := conn.Read(runCtx)
		if readErr != nil {
			if writeErr := takeError(writeErrs); writeErr != nil {
				return "", fmt.Errorf("agentd: pair: heartbeat: %w", writeErr)
			}
			return "", fmt.Errorf("agentd: pair: %w", classify(readErr))
		}

		// 不调用 Frame.Validate：它会对未知帧类型报错，而配对客户端应当忽略
		// 自己不认识的东西。只对两个配对帧检查载荷并解码。
		switch frame.Type {
		case protocol.FramePairPending:
			// --code 是单人认领，控制面不会对 claim 模式发 pending；即使未来发了
			// 也必须忽略，否则会打印一张与本次 --code 无关的码并写 claim 文件。
			if strings.TrimSpace(opts.Code) != "" {
				continue
			}
			if len(frame.Result) == 0 {
				return "", fmt.Errorf("agentd: pair: %s frame requires result", frame.Type)
			}
			if err := handlePending(frame, opts, pairKey); err != nil {
				return "", err
			}
		case protocol.FramePairResult:
			if len(frame.Result) == 0 {
				return "", fmt.Errorf("agentd: pair: %s frame requires result", frame.Type)
			}
			return handleResult(frame, opts.ClaimPath)
		}
		// 其余帧（心跳、事件、请求、未来的新类型）在待定连接上没有意义，忽略。
	}
}

// NewPairKey 生成一个待定连接标识：32 字节随机数的无填充 base64url。
//
// 它是连接标识而不是凭证：拿到它最多接管/复用自己的待定连接，不能认领节点。
func NewPairKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate pair key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// agentConnectPath 是节点拨号的固定后缀；子路径部署（反向代理挂在 /lework）
// 需要保留它前面的前缀，浏览器才知道控制面装在哪里。
const agentConnectPath = "/api/agent/connect"

// Link 把 ws(s) 控制面地址转成浏览器可打开的配对链接。
//
// 浏览器只知道 http(s) 源。标准部署的 URL 以 /api/agent/connect 结尾，这时
// 去掉该后缀、保留前缀（/lework/api/agent/connect → https://host/lework）；
// 其他路径形状保持既有行为，只保留 host。query 一律丢弃，短码与控制面地址
// 都由 url.Values 转义——它们是外部输入，手拼字符串迟早会漏转义。
func Link(controlPlaneURL, code string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(controlPlaneURL))
	if err != nil {
		return "", fmt.Errorf("parse control plane url: %w", err)
	}

	var scheme string
	switch endpoint.Scheme {
	case "ws", "http":
		scheme = "http"
	case "wss", "https":
		scheme = "https"
	default:
		return "", fmt.Errorf("control plane url scheme %q must be ws or wss", endpoint.Scheme)
	}
	if endpoint.Host == "" {
		return "", errors.New("control plane url has no host")
	}

	prefix := ""
	if strings.HasSuffix(endpoint.Path, agentConnectPath) {
		prefix = strings.TrimSuffix(endpoint.Path, agentConnectPath)
	}
	if prefix == "/" {
		prefix = ""
	}

	query := url.Values{}
	query.Set("url", scheme+"://"+endpoint.Host+prefix)
	query.Set("code", code)
	return "lework://node/pair?" + query.Encode(), nil
}

// ClaimPath 返回与凭证文件同目录的 claim 文件路径。
//
// 没有凭证路径（例如组合启动模式尚未解析默认值）时退回相对文件名。
func ClaimPath(credentialPath string) string {
	path := strings.TrimSpace(credentialPath)
	if path == "" {
		return claimFileName
	}
	return filepath.Join(filepath.Dir(path), claimFileName)
}

// ReadClaim 读取 claim 文件。
//
// 文件缺失返回 (zero,false,nil)；JSON 损坏才返回错误；已过期返回
// (claim,false,nil)，让调用方能说清「过期了，重新跑 lework agent」而不是
// 把一张已经无效的码当成可用。
func ReadClaim(path string) (Claim, bool, error) {
	if strings.TrimSpace(path) == "" {
		return Claim{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Claim{}, false, nil
		}
		return Claim{}, false, fmt.Errorf("read node claim %s: %w", path, err)
	}

	var claim Claim
	if err := json.Unmarshal(data, &claim); err != nil {
		return Claim{}, false, fmt.Errorf("parse node claim %s: %w", path, err)
	}
	// 旧控制面不发 expires_at 时零值表示「未知」，不能当成 0001 年就已过期：
	// 那会让每一张新码都显示过期、claim 命令永远不可用。
	if !claim.ExpiresAt.IsZero() && time.Now().After(claim.ExpiresAt) {
		return claim, false, nil
	}
	return claim, true, nil
}

// effectiveHeartbeatInterval 把零值/负值归一到 15 秒，供心跳协程启动时读取。
func effectiveHeartbeatInterval() time.Duration {
	if heartbeatInterval <= 0 {
		return 15 * time.Second
	}
	return heartbeatInterval
}

// heartbeatLoop 是待定连接上唯一的写者：hello 已在它启动前写完。
//
// 任何写失败都投递给读循环并取消上下文：半开的待定连接既等不到凭证，还会
// 占着控制面的配额，继续等下去只会更晚暴露问题。
func heartbeatLoop(
	ctx context.Context,
	conn Conn,
	interval time.Duration,
	errs chan<- error,
	cancel context.CancelFunc,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeCtx, cancelWrite := context.WithTimeout(ctx, heartbeatWriteTimeout)
			err := conn.Write(writeCtx, protocol.Frame{Type: protocol.FrameHeartbeat})
			cancelWrite()
			if err != nil {
				select {
				case errs <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

// takeError 非阻塞地取走心跳协程的错误。
func takeError(errs <-chan error) error {
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

// handlePending 解析 pair.pending：写 claim 文件，然后交给调用方展示。
func handlePending(frame protocol.Frame, opts Options, pairKey string) error {
	var payload struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(frame.Result, &payload); err != nil {
		return fmt.Errorf("agentd: pair: decode pair.pending: %w", err)
	}

	link, err := Link(opts.ControlPlaneURL, payload.Code)
	if err != nil {
		return fmt.Errorf("agentd: pair: build pairing link: %w", err)
	}

	if claimPath := strings.TrimSpace(opts.ClaimPath); claimPath != "" {
		claim := Claim{
			Link:            link,
			PairKey:         pairKey,
			Code:            payload.Code,
			ControlPlaneURL: strings.TrimSpace(opts.ControlPlaneURL),
			ExpiresAt:       payload.ExpiresAt,
		}
		if err := writeClaim(claimPath, claim); err != nil {
			// 同时保留哨兵与底层错误：调用方要据此判定致命退出，运维要知道
			// 是哪个目录/权限出了问题。
			return fmt.Errorf("agentd: pair: %w: %w", ErrClaimWrite, err)
		}
	}

	if opts.OnPending != nil {
		opts.OnPending(payload.Code, link)
	}
	return nil
}

// handleResult 解析 pair.result 并清掉 claim 文件：凭证已经到手，重印短码
// 只会误导用户再认领一次。
func handleResult(frame protocol.Frame, claimPath string) (string, error) {
	var payload struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(frame.Result, &payload); err != nil {
		return "", fmt.Errorf("agentd: pair: decode pair.result: %w", err)
	}
	if strings.TrimSpace(payload.Credential) == "" {
		// 不引用载荷本身：pair.result 里唯一的秘密字段就是凭证。
		return "", errors.New("agentd: pair: pair.result carries an empty credential")
	}
	removeClaim(claimPath)
	return payload.Credential, nil
}

// writeClaim 原子写入 claim 文件：临时文件 + rename，权限 0600、目录 0700。
//
// claim 文件含短码，属于「终端不在了也能重印」的功能数据；写失败必须显式
// 暴露，否则用户会以为 `lework agent claim` 能救场，实际文件根本没落盘。
func writeClaim(path string, claim Claim) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create claim directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".node.claim-*")
	if err != nil {
		return fmt.Errorf("create claim temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if err := tmp.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("chmod claim temp file: %w", err))
	}
	raw, err := json.Marshal(claim)
	if err != nil {
		return cleanup(fmt.Errorf("encode claim: %w", err))
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		return cleanup(fmt.Errorf("write claim temp file: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync claim temp file: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return cleanup(fmt.Errorf("close claim temp file: %w", err))
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace node claim %s: %w", path, err)
	}
	// 与 credential.Save 对称：掉电时 rename 可能还没落盘，目录 fsync 是
	// best-effort，失败不改变保存结果（部分平台不支持）。
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

// removeClaim 尽力删除 claim 文件：残留文件带过期时间，最坏只是一次过期
// 提示，不值得让配对成功因为删除失败而失败。
func removeClaim(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

// classify 把控制面的关闭原因映射成可判断的哨兵。
//
// 只映射 agent 需要区别对待的五种；其余（pairing unavailable、
// credential delivery failed 等）保持原错误，走通用重试提示。空原因与非
// WebSocket 错误同样不映射：它们说明不了配对处于哪一步。
func classify(err error) error {
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		return err
	}
	switch strings.TrimSpace(closeErr.Text) {
	case "pairing expired":
		return ErrPairingExpired
	case "credential already issued":
		return ErrCredentialAlreadyIssued
	case "claim code unavailable":
		return ErrClaimCodeUnavailable
	case "claim code attempts exceeded":
		return ErrClaimAttemptsExceeded
	case "pair connection replaced":
		return ErrPairConnReplaced
	default:
		return err
	}
}
