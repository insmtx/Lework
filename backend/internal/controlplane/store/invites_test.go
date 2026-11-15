package store

import (
	"context"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
)

func TestConsumeInviteIsOneShotAndExpires(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	inv, err := s.CreateInvite(ctx, Invite{CodeHash: authn.HashToken("code1"), CreatedBy: "admin", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if ok, _ := s.ConsumeInvite(ctx, inv.CodeHash, "u1"); !ok {
		t.Fatal("first consume failed")
	}
	if ok, _ := s.ConsumeInvite(ctx, inv.CodeHash, "u2"); ok {
		t.Fatal("invite reused")
	}
	expired, _ := s.CreateInvite(ctx, Invite{CodeHash: authn.HashToken("code2"), CreatedBy: "admin", ExpiresAt: time.Now().Add(-time.Minute)})
	if ok, _ := s.ConsumeInvite(ctx, expired.CodeHash, "u1"); ok {
		t.Fatal("expired invite accepted")
	}
}

// 消费成功必须落库 used_by/used_at，且新建邀请码不能带着调用方给的「已用」
// 状态进来——否则一条旧记录被重新插入就等于凭空多出一个可用邀请码。
func TestConsumeInviteRecordsUserAndTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := authn.HashToken("code3")

	inv, err := s.CreateInvite(ctx, Invite{CodeHash: hash, CreatedBy: "admin", ExpiresAt: time.Now().Add(time.Hour), UsedBy: "stale"})
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if inv.ID == "" {
		t.Fatal("empty invite id")
	}
	if inv.UsedBy != "" || inv.UsedAt != nil {
		t.Fatalf("new invite is already used: %+v", inv)
	}

	if ok, err := s.ConsumeInvite(ctx, hash, "u9"); err != nil || !ok {
		t.Fatalf("ConsumeInvite = %v, %v", ok, err)
	}

	var stored Invite
	if err := s.db.WithContext(ctx).Where("code_hash = ?", hash).Take(&stored).Error; err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if stored.UsedBy != "u9" || stored.UsedAt == nil {
		t.Fatalf("invite not marked as used: %+v", stored)
	}
}
