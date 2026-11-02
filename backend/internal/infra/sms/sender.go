package sms

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/Lework/backend/config"
	"github.com/ygpkg/yg-go/logs"
)

// SmsSender abstracts verification code SMS delivery.
// The built-in implementation is aliyunSMSSender; when AliyunConfig is
// missing required fields NewSender returns NoopSMSSender.
type SmsSender interface {
	SendVerificationCode(ctx context.Context, phone string, code string) error
	Enabled() bool
}

// NoopSMSSender is a test-mode SmsSender that logs the code instead of sending
// it. Enabled() returns false so callers can fall back to a fixed code.
type NoopSMSSender struct{}

func (NoopSMSSender) SendVerificationCode(ctx context.Context, phone string, code string) error {
	logs.InfoContextf(ctx, "SMS verification code test mode: phone=%s code=%s", MaskPhone(phone), code)
	return nil
}

func (NoopSMSSender) Enabled() bool {
	return false
}

// NewSender returns an SmsSender based on AliyunConfig. Returns NoopSMSSender
// when cfg is nil or any required field (AccessKeyID/AccessKeySecret/
// SignName/TemplateCode) is blank.
func NewSender(cfg *config.AliyunConfig) SmsSender {
	if cfg == nil || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" ||
		strings.TrimSpace(cfg.SignName) == "" || strings.TrimSpace(cfg.TemplateCode) == "" {
		return NoopSMSSender{}
	}
	next := *cfg
	if strings.TrimSpace(next.RegionID) == "" {
		next.RegionID = "cn-hangzhou"
	}
	return &aliyunSMSSender{
		cfg:        next,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// MaskPhone masks the middle digits of a phone number for logging.
func MaskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
