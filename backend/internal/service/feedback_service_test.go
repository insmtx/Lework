package service

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

type feedbackUserRepositoryStub struct {
	account.UserRepository

	user       *account.UserInfo
	requestUin uint
}

func (s *feedbackUserRepositoryStub) GetUserByID(context.Context, uint) (*account.UserInfo, error) {
	return nil, errors.New("feedback must not query a user by database ID")
}

func (s *feedbackUserRepositoryStub) GetUserByUin(_ context.Context, uin uint) (*account.UserInfo, error) {
	s.requestUin = uin
	return s.user, nil
}

func TestValidateFeedbackJobResolvesSubmitterByUin(t *testing.T) {
	const uin = uint(9001)
	userRepo := &feedbackUserRepositoryStub{
		user: &account.UserInfo{
			ID:    42,
			Uin:   uin,
			Name:  "反馈用户",
			Phone: "13800000000",
		},
	}
	svc := &FeedbackService{userRepo: userRepo}

	job, err := svc.validateFeedbackJob(context.Background(), &SubmitFeedbackRequest{
		OrgID:   1,
		Uin:     uin,
		Type:    "problem",
		Content: "提交反馈失败",
	})
	if err != nil {
		t.Fatalf("validateFeedbackJob() error = %v", err)
	}
	if userRepo.requestUin != uin {
		t.Fatalf("GetUserByUin() uin = %d, want %d", userRepo.requestUin, uin)
	}
	if job.uin != uin {
		t.Fatalf("feedback job uin = %d, want %d", job.uin, uin)
	}
}
