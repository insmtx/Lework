package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrClaimNotFound 表示认领记录不存在（码已失效、实例换库、用户重复提交）。
//
// 导出成哨兵错误而不是让调用方比对文本：接口层要据此回 404，gateway 要据此
// 关连接；字符串匹配会随文案调整悄悄失效。
var ErrClaimNotFound = errors.New("claim not found")

// ErrClaimTaken 表示认领记录已被别的账号抢到（或已过期）。
//
// 过期与「被别人抢走」共用这个值：对参与者来说两者都是「这张码不能再用了」，
// 区分它们只会泄露另一个账号的操作时间。
var ErrClaimTaken = errors.New("claim already claimed")

// ErrClaimNotClaimed 表示认领记录还没有被任何账号认领，凭证因此无处可发。
var ErrClaimNotClaimed = errors.New("claim is not claimed yet")

// ErrClaimCodeTaken 表示这个 5 位码已经被另一条待定认领占用。
//
// 码列只有普通索引（并发下仍可能重复），这条检查是应用层的尽力而为：它保证
// 正常流程里不会出现两张有效待定认领共用一个码，从而避免参与者认错机器。
var ErrClaimCodeTaken = errors.New("claim code already in use")

// maxNodeNameCandidates 是认领时尝试的展示名数量：原名加 -2 … -20。
//
// 上限存在的意义是让「同一个 owner 反复重装」不至于无限扫表；超过就报错，
// 由用户自己清理旧节点，而不是静默生成 work-mac-137。
const maxNodeNameCandidates = 20

// normalizeClaimCode 把短码收敛成展示与比较的唯一形式（大写、去首尾空格）。
//
// 用户会照着屏幕输小写、带空格，码本身是大小写不敏感的字母数字串；不归一化
// 就会出现「屏幕上写着 AB234，输 ab234 却说码无效」。
func normalizeClaimCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// CreateClaim 新建一次配对记录，并在 pair_key 重复时原样返回已有记录。
//
// 重连复用是核心契约：agent 断线后用同一个 pair_key 重连，必须拿回同一行
// （同一个 ID、同一个码、同一个状态），否则终端上的码会随网络抖动失效。
// 因此 pair_key 走唯一索引 + ON CONFLICT DO NOTHING，冲突时只读不写：
//
//   - pending  → gateway 重发 pair.pending，码继续有效；
//   - claimed  → 节点尚未拿到凭证时 gateway 补发一次；已有凭证则拒绝，
//     不能让 pair_key 变成轮换合法凭证的能力；
//   - expired  → gateway 关连接，让 agent 换一个 pair_key 重新开始。
//
// 状态判断留给 gateway，存储层不对已有行做任何「修正」——唯一的例外是 TTL
// 已到的 pending 行会按 expired 返回（见 ClaimByPairKey），因为从来没有人把
// TTL 到期落库成 expired。
//
// 插入只有一条语句：冲突分支靠 RowsAffected 判定并回读已有行，不先查后写，
// 所以「同时两个实例建同一条认领」也只会留下一行。码唯一性检查只针对真正
// 会插入的新行（排除同一个 pair_key），复用路径因此不会因为候选码被占而失败；
// 若 agent 重连时携带了一个已被别的认领占用的码，仍然会得到 ErrClaimCodeTaken，
// 此时它应当重发第一次拿到的那个码。
func (s *Store) CreateClaim(ctx context.Context, c NodeClaim) (NodeClaim, error) {
	pairKey := strings.TrimSpace(c.PairKey)
	if pairKey == "" {
		return NodeClaim{}, errors.New("claim pair key is required")
	}
	code := normalizeClaimCode(c.Code)
	if code == "" {
		return NodeClaim{}, errors.New("claim code is required")
	}
	if c.ExpiresAt.IsZero() {
		return NodeClaim{}, errors.New("claim expires at is required")
	}

	c.PairKey = pairKey
	c.Code = code
	c.ExpiresAt = c.ExpiresAt.UTC()
	c.Status = strings.TrimSpace(c.Status)
	if c.Status == "" {
		c.Status = ClaimStatusPending
	}
	// 状态是网关三态分派的唯一依据，落一个拼错的值进去会让认领永远卡住。
	switch c.Status {
	case ClaimStatusPending, ClaimStatusClaimed, ClaimStatusExpired:
	default:
		return NodeClaim{}, fmt.Errorf("unsupported claim status %q", c.Status)
	}
	if strings.TrimSpace(c.ID) == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	taken, err := s.claimCodeInUse(ctx, pairKey, code)
	if err != nil {
		return NodeClaim{}, err
	}
	if taken {
		// 错误文本只带 claim id：码是短暂有效的授权凭据，不进日志与错误。
		return NodeClaim{}, fmt.Errorf("create claim %q: %w", c.ID, ErrClaimCodeTaken)
	}

	res := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "pair_key"}},
		DoNothing: true,
	}).Create(&c)
	if res.Error != nil {
		return NodeClaim{}, fmt.Errorf("create claim %q: %w", c.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		// 这个 pair_key 已经有一行：重连复用，原样返回先落库的那条。
		existing, ok, err := s.ClaimByPairKey(ctx, pairKey)
		if err != nil {
			return NodeClaim{}, err
		}
		if ok {
			return existing, nil
		}
		return NodeClaim{}, fmt.Errorf("create claim %q: pair key conflict without existing row", c.ID)
	}
	return c, nil
}

// claimCodeInUse 判断 code 是否已被另一条待定且未过期的认领占用。
//
// 只把 pending + 未过期算作占用：已认领的码已经作废，已过期的码即便还有人
// 拿着它也认领不了任何东西，继续占用只会缩小 25 bit 的码空间。
//
// pairKey 用于排除「自己这条认领」：重连时 agent 会重发同一个码，若不排除，
// 复用路径会被自己的行判成码冲突。
func (s *Store) claimCodeInUse(ctx context.Context, pairKey, code string) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).
		Model(&NodeClaim{}).
		Where("code = ? AND status = ? AND expires_at > ? AND pair_key <> ?",
			code, ClaimStatusPending, time.Now().UTC(), pairKey).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("check claim code in use: %w", err)
	}
	return n > 0, nil
}

// ClaimByCode 按短码查一条待定且未过期的认领，用于确认页展示机器信息。
//
// now 由调用方传入而不是取 time.Now：确认页与限流共用同一个时间基准，测试也
// 能精确覆盖「刚好过期」的边界。已认领与已过期的码一律查不到——那正是
// 「这张码不能再用」的正确表现。
//
// 理论上码列可能重复（并发插入、码空间碰撞），此时返回最新创建的一行：
// 后建的认领才是用户眼前那次配对，返回旧行会让人确认到另一台机器。
func (s *Store) ClaimByCode(ctx context.Context, code string, now time.Time) (NodeClaim, bool, error) {
	code = normalizeClaimCode(code)
	if code == "" {
		return NodeClaim{}, false, nil
	}

	var rows []NodeClaim
	err := s.db.WithContext(ctx).
		Where("code = ? AND status = ? AND expires_at > ?", code, ClaimStatusPending, now.UTC()).
		Order("created_at DESC, id DESC").
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return NodeClaim{}, false, fmt.Errorf("lookup claim by code: %w", err)
	}
	if len(rows) == 0 {
		return NodeClaim{}, false, nil
	}
	return rows[0], true, nil
}

// ExpirePendingClaim 把一条仍在 pending 的认领标记为过期。
//
// 只影响 pending：已被账号认领的行正处于「等凭证投递」阶段，把它改成
// expired 会让合法 agent 拿不到凭证。断连路径与过期路径重复调用无害，
// 因此不需要调用方先查状态。NodeClaim 没有 updated_at 列，只改 status。
func (s *Store) ExpirePendingClaim(ctx context.Context, claimID string) error {
	if strings.TrimSpace(claimID) == "" {
		return nil
	}
	err := s.db.WithContext(ctx).
		Model(&NodeClaim{}).
		Where("id = ? AND status = ?", claimID, ClaimStatusPending).
		UpdateColumn("status", ClaimStatusExpired).Error
	if err != nil {
		return fmt.Errorf("expire pending claim %q: %w", claimID, err)
	}
	return nil
}

// ClaimByPairKey 按 agent 的 pair_key 查认领记录，返回调用方视角的状态。
//
// gateway 按三态分派：pending 重发 pair.pending、claimed 只在节点还没有凭证时
// 补发一次、expired 关连接让 agent 换一个 pair_key。因此这里不能按状态过滤，
// 也不能只在 SQL 里判断过期：
//
//   - TTL 已到的 pending 行按 expired 返回。库里没有任何路径会把「时间到了」
//     落库成 expired（唯一写 expired 的是 BumpClaimAttempts 的尝试次数超限），
//     若原样返回 pending，网关就会对着一条早该作废的认领永远重发同一个码。
//     这只是返回值上的归一化，**不写库**，原始状态仍可在库里查到。
//   - claimed 行无论是否过 TTL 都保持 claimed：认领成功、实例在发凭证前挂掉，
//     agent 可能几分钟后才重连，补发不能被 TTL 掐断。是否允许补发由 gateway
//     检查节点的 credential_hash 决定：已有凭证的节点不会再被轮换。
func (s *Store) ClaimByPairKey(ctx context.Context, pairKey string) (NodeClaim, bool, error) {
	pairKey = strings.TrimSpace(pairKey)
	if pairKey == "" {
		return NodeClaim{}, false, nil
	}

	var rows []NodeClaim
	err := s.db.WithContext(ctx).
		Where("pair_key = ?", pairKey).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return NodeClaim{}, false, fmt.Errorf("lookup claim by pair key: %w", err)
	}
	if len(rows) == 0 {
		return NodeClaim{}, false, nil
	}

	claim := rows[0]
	if claim.Status == ClaimStatusPending && time.Now().UTC().After(claim.ExpiresAt) {
		claim.Status = ClaimStatusExpired
	}
	return claim, true, nil
}

// ClaimClaimForUser 原子地把一条待定认领转给 userID，是纯状态迁移原语。
//
// 判断与写入合并成单条 UPDATE：先查后写会让两个账号同时认领同一台机器都成功，
// 而「谁先确认谁得到」正是这条码的全部意义。RowsAffected 为 0 表示这次没抢到
// （不存在、已被认领或已过期），不是错误——调用方据此回 409/410，SQLite 与
// PostgreSQL 的行为一致。
//
// 返回抢到之后的整行（含 claimed_by），调用方不必再查一次。建节点走
// BindClaimToUser：它把这次迁移和建节点放进同一个事务。
func (s *Store) ClaimClaimForUser(ctx context.Context, claimID, userID string) (NodeClaim, bool, error) {
	if strings.TrimSpace(claimID) == "" {
		return NodeClaim{}, false, errors.New("claim id is required")
	}
	if strings.TrimSpace(userID) == "" {
		return NodeClaim{}, false, errors.New("claim user id is required")
	}

	db := s.db.WithContext(ctx)
	ok, err := claimClaimTx(db, claimID, userID, time.Now().UTC())
	if err != nil {
		return NodeClaim{}, false, fmt.Errorf("transition claim %q: %w", claimID, err)
	}
	if !ok {
		return NodeClaim{}, false, nil
	}

	claim, found, err := claimByID(db, claimID)
	if err != nil {
		return NodeClaim{}, false, err
	}
	if !found {
		return NodeClaim{}, false, fmt.Errorf("claim claim %q: %w", claimID, ErrClaimNotFound)
	}
	return claim, true, nil
}

// claimClaimTx 是 pending → claimed 的唯一实现，供 ClaimClaimForUser 与
// BindClaimToUser 共用：两处若各写一份 WHERE 条件，迟早有一处漏掉过期判断，
// 而那种洞只有并发认领时才会暴露。
//
// 错误原样返回，由调用方补上下文：事务里的调用方会在外层包上 bind，再包一次
// claim id 就会得到「bind claim X: claim claim X: ...」这种没人读得下去的信息。
func claimClaimTx(db *gorm.DB, claimID, userID string, now time.Time) (bool, error) {
	res := db.Model(&NodeClaim{}).
		Where("id = ? AND status = ? AND expires_at > ?", claimID, ClaimStatusPending, now).
		UpdateColumns(NodeClaim{Status: ClaimStatusClaimed, ClaimedBy: userID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ClaimByID 按主键读一条认领记录，供网关在收到认领通知后重新确认状态。
//
// 投递路径按 id 读而不是按 pair_key 读：通知里携带的是 claim id，按 id 才能
// 校验它确实是本连接那条认领，也才能在同一 pair_key 被复用时避免误读。
func (s *Store) ClaimByID(ctx context.Context, id string) (NodeClaim, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return NodeClaim{}, false, nil
	}
	return claimByID(s.db.WithContext(ctx), id)
}

// claimByID 按主键读一行认领记录；db 既可以是 *gorm.DB 也可以是事务句柄。
//
// 用 Find 而非 First：未命中是正常分支（码被清理、实例换库），不该产生
// record not found 日志。
func claimByID(db *gorm.DB, id string) (NodeClaim, bool, error) {
	var rows []NodeClaim
	if err := db.Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return NodeClaim{}, false, fmt.Errorf("lookup claim %q: %w", id, err)
	}
	if len(rows) == 0 {
		return NodeClaim{}, false, nil
	}
	return rows[0], true, nil
}

// BindClaimToUser 认领一条待定记录，并在同一个事务里建节点与 owner 归属行。
//
// 三步必须原子：迁移状态、建 nodes、写 node_members。中间任何一步失败都会让
// 用户看到一个「认领了但节点列表里没有」的空窗；把事务放在存储层是因为 API
// 层拿不到数据库句柄，也不该为了这个接口学事务边界。
//
// 失败语义沿用 ClaimClaimForUser：行不存在 → ErrClaimNotFound（404），存在但
// 抢不到（已被认领或已过期）→ ErrClaimTaken（409）。
//
// 名字冲突按设计 §6 自动加后缀（name-2 … name-20）：重装系统会自然产生同名
// 新节点，拒绝认领会把用户卡在配对流程里。注意 nodes.name 不是唯一索引，
// 同一 owner 的两次并发认领可能挑中同一个候选名——这个后缀循环只是尽力而为，
// 不是唯一性保证；真正的唯一性没有意义（名字只是展示字段）。
func (s *Store) BindClaimToUser(
	ctx context.Context,
	claimID, userID, nodeName string,
) (Node, error) {
	if strings.TrimSpace(claimID) == "" {
		return Node{}, errors.New("claim id is required")
	}
	if strings.TrimSpace(userID) == "" {
		return Node{}, errors.New("claim user id is required")
	}

	now := time.Now().UTC()
	var created Node
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ok, err := claimClaimTx(tx, claimID, userID, now)
		if err != nil {
			return err
		}
		if !ok {
			// 0 行有两种原因，分开报错才能让调用方回 404 还是 409。
			if _, found, err := claimByID(tx, claimID); err != nil {
				return err
			} else if !found {
				return ErrClaimNotFound
			}
			return ErrClaimTaken
		}

		claim, found, err := claimByID(tx, claimID)
		if err != nil {
			return err
		}
		if !found {
			return ErrClaimNotFound
		}

		name, err := freeNodeName(tx, userID, claimNodeName(claim, nodeName))
		if err != nil {
			return err
		}

		id, err := newNodeID()
		if err != nil {
			return err
		}
		node := Node{
			ID:           id,
			Name:         name,
			OS:           claim.OS,
			Arch:         claim.Arch,
			AgentVersion: claim.AgentVersion,
			ClaimedAt:    now,
		}
		if err := tx.Create(&node).Error; err != nil {
			return fmt.Errorf("create node %q: %w", node.ID, err)
		}

		member := NodeMember{
			NodeID:    node.ID,
			UserID:    userID,
			Role:      NodeRoleOwner,
			GrantedBy: userID,
			CreatedAt: now,
		}
		if err := tx.Create(&member).Error; err != nil {
			return fmt.Errorf("create owner row for node %q: %w", node.ID, err)
		}

		// node_id 在同一事务里写回：gateway 之后即使挂掉，重连也能从这一行
		// 知道节点已经建好，不必猜。
		if err := tx.Model(&NodeClaim{}).
			Where("id = ?", claimID).
			UpdateColumn("node_id", node.ID).Error; err != nil {
			return fmt.Errorf("record node %q on claim %q: %w", node.ID, claimID, err)
		}
		created = node
		return nil
	})
	if err != nil {
		return Node{}, fmt.Errorf("bind claim %q: %w", claimID, err)
	}
	return created, nil
}

// claimNodeName 决定新节点的展示名：显式传入 > 认领记录里的机器名 > "node"。
func claimNodeName(claim NodeClaim, override string) string {
	if name := strings.TrimSpace(override); name != "" {
		return name
	}
	if name := strings.TrimSpace(claim.NodeName); name != "" {
		return name
	}
	return "node"
}

// freeNodeName 在 owner 名下为候选名找一个空位。
//
// 只按 owner 去重：name 是展示字段，别人账号里的同名节点与本账号无关，占用
// 后缀只会让「我的节点」列表出现 work-mac-2 这种莫名其妙的编号。
func freeNodeName(tx *gorm.DB, userID, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "node"
	}

	for i := 1; i <= maxNodeNameCandidates; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}

		var n int64
		err := tx.Model(&Node{}).
			Joins("JOIN node_members ON node_members.node_id = nodes.id").
			Where("node_members.user_id = ? AND nodes.name = ?", userID, candidate).
			Count(&n).Error
		if err != nil {
			return "", fmt.Errorf("check node name %q: %w", candidate, err)
		}
		if n == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free node name for %q", base)
}

// MarkClaimDelivered 记录「凭证已经交给 agent」这一步的簿记。
//
// 正常路径上它是空操作：BindClaimToUser 已经在自己的事务里写好了 node_id。
// 它存在是为了补发——认领成功但实例在生成凭证前挂掉时，agent 用同一个
// pair_key 重连，新实例补发凭证后再调一次这里。重复调用不报错。
//
// 调用方必须在 pair.result 写出成功之后调用：先记账后写帧会让「库里说已投递」
// 与「agent 实际没收到」分叉。簿记失败只影响补发判断，不影响已经写出的凭证。
//
// 补发只认已经绑定的那个节点：node_id 为空时写入，已等于入参时视为成功，
// 是别的节点则返回 ErrClaimTaken 且不改动。两个实例并发补发时，后到的实例
// 可能刚建了自己的节点，静默改绑会让先落库的节点变成没人认领的孤儿。
//
// 只有 claimed 行可以标记：pending 行还没人认领，没有凭证可发（
// ErrClaimNotClaimed）；行不存在说明配对已经被清理（ErrClaimNotFound）。
func (s *Store) MarkClaimDelivered(ctx context.Context, claimID, nodeID string) error {
	if strings.TrimSpace(claimID) == "" {
		return errors.New("claim id is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}

	db := s.db.WithContext(ctx)
	res := db.Model(&NodeClaim{}).
		Where("id = ? AND status = ? AND (node_id = '' OR node_id = ?)",
			claimID, ClaimStatusClaimed, nodeID).
		UpdateColumn("node_id", nodeID)
	if res.Error != nil {
		return fmt.Errorf("mark claim %q delivered: %w", claimID, res.Error)
	}
	if res.RowsAffected == 0 {
		claim, found, err := claimByID(db, claimID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("mark claim %q delivered: %w", claimID, ErrClaimNotFound)
		}
		if claim.Status != ClaimStatusClaimed {
			return fmt.Errorf("mark claim %q delivered: %w", claimID, ErrClaimNotClaimed)
		}
		return fmt.Errorf("mark claim %q delivered: %w", claimID, ErrClaimTaken)
	}
	return nil
}

// UpdateClaimMachine 把待定连接 hello 里的机器信息写回认领记录。
//
// 只允许 pending 行：claimed 行已经被账号认领并建好节点，晚到的连接不能借
// 「更新机器信息」改写一台已归属的机器；expired 行同理。0 行时区分不存在
// （ErrClaimNotFound）与「已被别人认领/已过期」（ErrClaimTaken），调用方据此
// 决定关连接还是重读状态。
//
// 只触碰 node_name/os/arch/agent_version：码、pair_key、状态、归属与 node_id
// 都是身份或授权字段，不能从这条路径写入。
//
// 用显式列 map 而不是结构体：GORM 的结构体更新会跳过零值，agent 从 linux
// 重连成空 os/arch 时旧信息会残留，确认页因此在描述另一台机器。
func (s *Store) UpdateClaimMachine(ctx context.Context, claimID string, meta NodeClaim) error {
	if strings.TrimSpace(claimID) == "" {
		return errors.New("claim id is required")
	}

	db := s.db.WithContext(ctx)
	res := db.Model(&NodeClaim{}).
		Where("id = ? AND status = ?", claimID, ClaimStatusPending).
		Updates(map[string]any{
			"node_name":     meta.NodeName,
			"os":            meta.OS,
			"arch":          meta.Arch,
			"agent_version": meta.AgentVersion,
		})
	if res.Error != nil {
		return fmt.Errorf("update claim %q machine info: %w", claimID, res.Error)
	}
	if res.RowsAffected == 0 {
		_, found, err := claimByID(db, claimID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("update claim %q machine info: %w", claimID, ErrClaimNotFound)
		}
		return fmt.Errorf("update claim %q machine info: %w", claimID, ErrClaimTaken)
	}
	return nil
}

// BumpClaimAttempts 原子地累加「同一条认领」的绑定重试次数，超过 max 就把
// 认领标记为过期。
//
// 它服务的是同一个 claim 反复绑定失败的重试计数，不是猜码限流：mode=claim
// 的按 IP / pair_key 失败限流在 gateway 内存里完成（见 agentgw.claimAttempts），
// 浏览器端的猜码限流由 Task 16 负责。本方法因此没有生产调用方，保留为存储层
// 的原子计数原语。
//
// 累加与作废必须是同一条 UPDATE（用 CASE 表达），否则两次并发尝试可能各自
// 读到「还没到上限」而同时放行，限流就形同虚设。语句只匹配 pending 行：
// 已认领/已过期的行不动，返回 (false, nil) 而不是错误——「这张码已经不能再用」
// 正是调用方想要的结论。
//
// 返回 true 表示这次尝试仍在上限内（认领还可以继续用），false 表示已作废。
func (s *Store) BumpClaimAttempts(ctx context.Context, claimID string, max int) (bool, error) {
	if strings.TrimSpace(claimID) == "" {
		return false, errors.New("claim id is required")
	}
	if max <= 0 {
		return false, errors.New("claim attempt limit must be positive")
	}

	db := s.db.WithContext(ctx)
	res := db.Model(&NodeClaim{}).
		Where("id = ? AND status = ?", claimID, ClaimStatusPending).
		Updates(map[string]any{
			"attempts": gorm.Expr("attempts + 1"),
			"status": gorm.Expr(
				"CASE WHEN attempts + 1 > ? THEN ? ELSE status END", max, ClaimStatusExpired),
		})
	if res.Error != nil {
		return false, fmt.Errorf("bump claim %q attempts: %w", claimID, res.Error)
	}
	if res.RowsAffected == 0 {
		if _, found, err := claimByID(db, claimID); err != nil {
			return false, err
		} else if !found {
			return false, fmt.Errorf("bump claim %q attempts: %w", claimID, ErrClaimNotFound)
		}
		return false, nil
	}

	claim, found, err := claimByID(db, claimID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("bump claim %q attempts: %w", claimID, ErrClaimNotFound)
	}
	return claim.Status == ClaimStatusPending && claim.Attempts <= max, nil
}
