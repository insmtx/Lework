package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// NodeRoleOwner 是认领节点的账号：能共享、撤销他人访问，也能删除节点。
	NodeRoleOwner = "owner"
	// NodeRoleMember 是接受共享的账号：只能使用节点，不能改变归属。
	NodeRoleMember = "member"
)

// ErrNodeNotFound 表示认领目标节点不存在。
//
// 导出成哨兵错误而不是让调用方比对错误文本：接口层要据此回 404，字符串匹配
// 会随文案调整悄悄失效，最终把「节点不存在」变成 500。
var ErrNodeNotFound = errors.New("node not found")

// ErrNodeAlreadyClaimed 表示节点已经属于某个账号，认领必须改走共享。
var ErrNodeAlreadyClaimed = errors.New("node already claimed")

// AddNodeMember 写入一条归属行；已存在时忽略而不是报错。
//
// 共享按钮会被重复点击、两台控制面实例也可能同时处理同一请求，把「已经有了」
// 当失败只会制造无意义的 409。DoNothing 同时保证重复添加不会把 owner 降级成
// member——已有行的角色只能通过显式的角色变更修改，不在这个接口的职责里。
func (s *Store) AddNodeMember(ctx context.Context, m NodeMember) error {
	if strings.TrimSpace(m.NodeID) == "" {
		return errors.New("member node id is required")
	}
	if strings.TrimSpace(m.UserID) == "" {
		return errors.New("member user id is required")
	}

	m.Role = strings.TrimSpace(m.Role)
	if m.Role == "" {
		// 省略角色按最小权限处理：调用方忘了填角色不该意外造出 owner。
		m.Role = NodeRoleMember
	}
	if m.Role != NodeRoleOwner && m.Role != NodeRoleMember {
		return fmt.Errorf("unsupported node role %q", m.Role)
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&m).Error; err != nil {
		return fmt.Errorf("add member %q to node %q: %w", m.UserID, m.NodeID, err)
	}
	return nil
}

// RemoveNodeMember 删除一条归属行；行不存在时不报错。
//
// 撤销访问会被重复触发（界面重试、多实例同时处理），「已经没有权限」正是调用方
// 期望的结果，因此幂等。
func (s *Store) RemoveNodeMember(ctx context.Context, nodeID, userID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("member node id is required")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("member user id is required")
	}
	err := s.db.WithContext(ctx).
		Where("node_id = ? AND user_id = ?", nodeID, userID).
		Delete(&NodeMember{}).Error
	if err != nil {
		return fmt.Errorf("remove member %q from node %q: %w", userID, nodeID, err)
	}
	return nil
}

// NodeRole 返回某账号在某节点上的角色；没有归属行时 ok 为 false 且不视为错误。
//
// 授权层靠它区分 owner / member；「不是成员」是正常分支，绝不能用一个默认
// 角色代替，否则任何登录用户都能看到别人的节点。
func (s *Store) NodeRole(ctx context.Context, nodeID, userID string) (string, bool, error) {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(userID) == "" {
		return "", false, nil
	}

	// 用 Find 而非 First：未命中是正常分支，不应产生 record not found 日志。
	var rows []NodeMember
	err := s.db.WithContext(ctx).
		Where("node_id = ? AND user_id = ?", nodeID, userID).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return "", false, fmt.Errorf("lookup role of user %q on node %q: %w", userID, nodeID, err)
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Role, true, nil
}

// ListNodesForUser 返回某账号有归属行的全部节点，按名称升序。
//
// 传入空 userID 时返回空列表而不是全表：把「没有用户」当成「所有用户」会让
// 未登录请求意外看到全部节点，这种失败方向无法接受。
func (s *Store) ListNodesForUser(ctx context.Context, userID string) ([]Node, error) {
	nodes := make([]Node, 0)
	if strings.TrimSpace(userID) == "" {
		return nodes, nil
	}

	err := s.db.WithContext(ctx).
		Model(&Node{}).
		Joins("JOIN node_members ON node_members.node_id = nodes.id").
		Where("node_members.user_id = ?", userID).
		Order("nodes.name ASC, nodes.id ASC").
		Find(&nodes).Error
	if err != nil {
		return nil, fmt.Errorf("list nodes for user %q: %w", userID, err)
	}
	return nodes, nil
}

// ListUnclaimedNodes 返回没有任何归属行的节点，按名称升序。
//
// 这些是升级前的历史节点：enrollment token 直通登记时还没有归属表。管理员
// 在界面上把它们一键认领到某个账号，普通账号永远看不到它们。
func (s *Store) ListUnclaimedNodes(ctx context.Context) ([]Node, error) {
	nodes := make([]Node, 0)
	err := s.db.WithContext(ctx).
		Model(&Node{}).
		Where("NOT EXISTS (SELECT 1 FROM node_members WHERE node_members.node_id = nodes.id)").
		Order("name ASC, id ASC").
		Find(&nodes).Error
	if err != nil {
		return nil, fmt.Errorf("list unclaimed nodes: %w", err)
	}
	return nodes, nil
}

// ClaimNodeToUser 把无归属节点认领给 userID，写入 owner 行并记录认领时间。
//
// 只允许「没有任何成员行」的节点被认领：这是认领码流程与管理员认领历史节点
// 的共用入口，一旦节点已属于某人，第二个人就必须走共享而不是认领。
//
// 并发保护落在事务里：第一条 UPDATE 以「不存在成员行」为条件更新 claimed_at，
// PostgreSQL 会因此锁住该行、SQLite 会串行化写事务；随后在锁内再数一次成员行
// 并插入 owner（READ COMMITTED 下这次计数能看到前一个事务刚提交的行）。
// 后到的认领因此失败，而不是造出两个 owner。
//
// 两种预期失败用 ErrNodeNotFound / ErrNodeAlreadyClaimed 表达，调用方可用
// errors.Is 区分 404 与 409，不必解析错误文本。
func (s *Store) ClaimNodeToUser(ctx context.Context, nodeID, userID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("claim node id is required")
	}
	if strings.TrimSpace(userID) == "" {
		return errors.New("claim user id is required")
	}

	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Node{}).
			Where("id = ?", nodeID).
			Where("NOT EXISTS (SELECT 1 FROM node_members WHERE node_members.node_id = nodes.id)").
			Update("claimed_at", now)
		if res.Error != nil {
			return fmt.Errorf("lock for claim: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			// 0 行有两种原因，分开报错才能让调用方回 404 还是 409。
			var nodeCount int64
			if err := tx.Model(&Node{}).Where("id = ?", nodeID).Count(&nodeCount).Error; err != nil {
				return fmt.Errorf("check node after claim: %w", err)
			}
			if nodeCount == 0 {
				return ErrNodeNotFound
			}
			return ErrNodeAlreadyClaimed
		}

		// 拿到行锁后再数一次成员行：并发认领时后到的事务可能在上面的条件求值
		// 之前就通过了检查，这条查询在 READ COMMITTED 下看到最新快照，能兜住
		// 前一个事务刚提交的 owner 行，避免同一个节点出现两个 owner。
		var memberCount int64
		if err := tx.Model(&NodeMember{}).Where("node_id = ?", nodeID).Count(&memberCount).Error; err != nil {
			return fmt.Errorf("recheck node members: %w", err)
		}
		if memberCount > 0 {
			return ErrNodeAlreadyClaimed
		}

		member := NodeMember{
			NodeID:    nodeID,
			UserID:    userID,
			Role:      NodeRoleOwner,
			GrantedBy: userID,
			CreatedAt: now,
		}
		if err := tx.Create(&member).Error; err != nil {
			return fmt.Errorf("create owner row: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("claim node %q for user %q: %w", nodeID, userID, err)
	}
	return nil
}

// LookupNodeByCredential 按凭证哈希解析节点身份；空哈希一律视为未命中。
//
// 未激活节点的 credential_hash 是空串，如果这里把空串当成普通查询值，任意
// 空 Bearer 就会命中第一条未激活节点。空哈希是「没有凭证」而不是「凭证为空」，
// 因此必须先拦掉再查库。
func (s *Store) LookupNodeByCredential(ctx context.Context, tokenHash string) (Node, bool, error) {
	hash := strings.TrimSpace(tokenHash)
	if hash == "" {
		return Node{}, false, nil
	}

	var rows []Node
	err := s.db.WithContext(ctx).
		Where("credential_hash = ?", hash).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return Node{}, false, fmt.Errorf("lookup node by credential: %w", err)
	}
	if len(rows) == 0 {
		return Node{}, false, nil
	}
	return rows[0], true, nil
}

// NodeByID 按主键读节点记录，供凭证下发前确认节点是否已经激活。
//
// 用 Find 而不是 First：节点在等待期间被清理是正常分支，不该产生
// record not found 日志。未命中以 found=false 返回，由调用方决定关闭连接。
func (s *Store) NodeByID(ctx context.Context, id string) (Node, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Node{}, false, nil
	}

	var rows []Node
	err := s.db.WithContext(ctx).
		Where("id = ?", id).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return Node{}, false, fmt.Errorf("lookup node %q: %w", id, err)
	}
	if len(rows) == 0 {
		return Node{}, false, nil
	}
	return rows[0], true, nil
}

// SetNodeCredential 把节点凭证的哈希写进节点记录，节点不存在时返回错误。
//
// 入参是哈希而不是明文：明文只存在于签发它的那个局部变量里，任何日志、错误
// 文本与数据库都不得看到它。已有凭证会被覆盖（最后写入者生效），因为「读到空
// 凭证 → 生成 → 写回」在多个控制面实例上可能各生成一份，此时 agent 手上拿到的
// 那份必须正好是后写进库的那份。
func (s *Store) SetNodeCredential(ctx context.Context, nodeID, tokenHash string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}
	hash := strings.TrimSpace(tokenHash)
	if hash == "" {
		return errors.New("node credential hash is required")
	}

	res := s.db.WithContext(ctx).
		Model(&Node{}).
		Where("id = ?", nodeID).
		Update("credential_hash", hash)
	if res.Error != nil {
		return fmt.Errorf("set credential for node %q: %w", nodeID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("set credential for node %q: node not found", nodeID)
	}
	return nil
}

// UpsertLegacyNode 是旧 enrollment token 路径的登记入口：按展示名复用未认领节点。
//
// 复用条件是「同名且没有任何成员行」，不再要求节点还没有凭证：enrollment token
// 本身就是部署级管理员凭据，持有它的人本来就能注册节点；旧二进制读不懂
// welcome.credential，只能每次都用令牌登记，若还要求「没有凭证」，它每次重连
// 都会留下一条新的同名节点，而已被认领的旧节点也会因为成员行永不上线。按名复用
// 因此是安全的，唯一仍然拒绝的是已经被某个账号认领/共享的节点：共享令牌不得
// 接管他人归属，这种情况一律新建一条未认领记录，让同名节点共存（名字只用于展示）。
//
// 复用已有行时保留它的 credential_hash、claimed_at 与 created_at：调用方随后会
// 重新签发凭据（每次令牌登记都轮换），这里提前清空会让正在使用的凭证在新哈希
// 写入前短暂失效。新建时仍强制 credential_hash 为空，身份只能由
// SetNodeCredential 绑定；调用方传入的 ID 同样会被忽略，避免覆盖别的行。
func (s *Store) UpsertLegacyNode(ctx context.Context, n Node) (Node, error) {
	if strings.TrimSpace(n.Name) == "" {
		return Node{}, errors.New("node name is required")
	}

	// 用 Find 而非 First 查询：未命中是正常分支，不应产生 record not found 日志。
	// 同名未认领节点可能不止一条，固定取最早创建的那条，让重复登记结果稳定。
	var existing []Node
	err := s.db.WithContext(ctx).
		Where("name = ?", n.Name).
		Where("NOT EXISTS (SELECT 1 FROM node_members WHERE node_members.node_id = nodes.id)").
		Order("created_at ASC, id ASC").
		Limit(1).
		Find(&existing).Error
	if err != nil {
		return Node{}, fmt.Errorf("lookup legacy node %q: %w", n.Name, err)
	}

	n.CredentialHash = ""
	if len(existing) > 0 {
		n.ID = existing[0].ID
		n.CredentialHash = existing[0].CredentialHash
		n.ClaimedAt = existing[0].ClaimedAt
		n.CreatedAt = existing[0].CreatedAt
	} else {
		id, idErr := newNodeID()
		if idErr != nil {
			return Node{}, idErr
		}
		n.ID = id
	}

	if err := s.db.WithContext(ctx).Save(&n).Error; err != nil {
		return Node{}, fmt.Errorf("save legacy node %q: %w", n.Name, err)
	}
	return n, nil
}

// UpdateNodeMeta 只刷新节点的展示字段（名字、系统、架构、版本、能力、工作区）。
//
// 凭证路径下 hello 里的 node_name 不参与身份判定，只用于展示，所以这里能改名字；
// 但凭证、认领时间与最后心跳不属于展示信息，必须原样保留，否则一条 hello 就能
// 悄悄改掉节点身份或让它在界面上「永久在线」。空名字表示这一轮没上报展示名，
// 保留原值。
func (s *Store) UpdateNodeMeta(ctx context.Context, nodeID string, n Node) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}

	fields := []string{"os", "arch", "agent_version", "capabilities", "workspace_roots", "updated_at"}
	meta := Node{
		OS:             n.OS,
		Arch:           n.Arch,
		AgentVersion:   n.AgentVersion,
		Capabilities:   n.Capabilities,
		WorkspaceRoots: n.WorkspaceRoots,
	}
	if name := strings.TrimSpace(n.Name); name != "" {
		fields = append(fields, "name")
		meta.Name = name
	}

	// Select + struct：选中字段即使是零值也写入，未选中的列（credential_hash、
	// claimed_at、last_seen、created_at）不会被这条语句碰到。
	res := s.db.WithContext(ctx).
		Model(&Node{}).
		Where("id = ?", nodeID).
		Select(fields).
		Updates(meta)
	if res.Error != nil {
		return fmt.Errorf("update meta for node %q: %w", nodeID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("update meta for node %q: node not found", nodeID)
	}
	return nil
}
