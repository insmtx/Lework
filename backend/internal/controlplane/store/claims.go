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
//   - claimed  → gateway 走幂等重投，重新下发凭证；
//   - expired  → gateway 关连接，让 agent 换一个 pair_key 重新开始。
//
// 状态判断留给 gateway，存储层不对已有行做任何「修正」。码唯一性检查只作用于
// 新建行：复用路径上即便候选码已被占用也不影响返回结果，否则重连会因为一个
// 根本不会用到的码而失败。
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
	if strings.TrimSpace(c.ID) == "" {
		c.ID = uuid.NewString()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	// 先按 pair_key 探一次已有行：冲突复用路径不能受码唯一性检查影响，
	// 而下面的 ON CONFLICT 只能告诉我们「插没插进去」，说不出插入前是什么状态。
	existing, ok, err := s.ClaimByPairKey(ctx, pairKey)
	if err != nil {
		return NodeClaim{}, err
	}
	if ok {
		return existing, nil
	}

	taken, err := s.claimCodeInUse(ctx, code)
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
		// 探查询与插入之间，另一个实例先插了同一 pair_key：以先落库的那行为准。
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
func (s *Store) claimCodeInUse(ctx context.Context, code string) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).
		Model(&NodeClaim{}).
		Where("code = ? AND status = ? AND expires_at > ?", code, ClaimStatusPending, time.Now().UTC()).
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

// ClaimByPairKey 按 agent 的 pair_key 查认领记录，不看状态与过期时间。
//
// gateway 需要 claimed 行做幂等重投（认领成功但凭证还没落地时 agent 重连），
// 也需要 expired 行来关连接，所以这里刻意不过滤状态；「能不能用」由调用方
// 按 Status 判断。
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
	return rows[0], true, nil
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
		return NodeClaim{}, false, err
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
func claimClaimTx(db *gorm.DB, claimID, userID string, now time.Time) (bool, error) {
	res := db.Model(&NodeClaim{}).
		Where("id = ? AND status = ? AND expires_at > ?", claimID, ClaimStatusPending, now).
		UpdateColumns(NodeClaim{Status: ClaimStatusClaimed, ClaimedBy: userID})
	if res.Error != nil {
		return false, fmt.Errorf("claim claim %q: %w", claimID, res.Error)
	}
	return res.RowsAffected == 1, nil
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
// 它存在是为了幂等重投——认领成功但实例在生成凭证前挂掉时，agent 用同一个
// pair_key 重连，新实例补发凭证后再调一次这里。重复调用不报错。
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
		Where("id = ? AND status = ?", claimID, ClaimStatusClaimed).
		UpdateColumn("node_id", nodeID)
	if res.Error != nil {
		return fmt.Errorf("mark claim %q delivered: %w", claimID, res.Error)
	}
	if res.RowsAffected == 0 {
		if _, found, err := claimByID(db, claimID); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("mark claim %q delivered: %w", claimID, ErrClaimNotFound)
		}
		return fmt.Errorf("mark claim %q delivered: %w", claimID, ErrClaimNotClaimed)
	}
	return nil
}

// BumpClaimAttempts 原子地累加码尝试次数，超过 max 就把认领标记为过期。
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
