package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/insmtx/Lework/backend/config"
	"github.com/ygpkg/yg-go/logs"
)

const aliyunSMSEndpoint = "https://dysmsapi.aliyuncs.com/"

type aliyunSMSSender struct {
	cfg        config.AliyunConfig
	httpClient *http.Client
}

func (s *aliyunSMSSender) Enabled() bool {
	return true
}

func (s *aliyunSMSSender) SendVerificationCode(ctx context.Context, phone string, code string) error {
	logs.InfoContextf(ctx, "Aliyun SMS SendSms request: phone=%s sign_name=%s template_code=%s region_id=%s",
		MaskPhone(phone), s.cfg.SignName, s.cfg.TemplateCode, s.cfg.RegionID)

	templateParam, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return fmt.Errorf("marshal sms template param: %w", err)
	}

	values := url.Values{}
	values.Set("Action", "SendSms")
	values.Set("Version", "2017-05-25")
	values.Set("RegionId", s.cfg.RegionID)
	values.Set("PhoneNumbers", phone)
	values.Set("SignName", s.cfg.SignName)
	values.Set("TemplateCode", s.cfg.TemplateCode)
	values.Set("TemplateParam", string(templateParam))
	values.Set("Format", "JSON")
	values.Set("AccessKeyId", s.cfg.AccessKeyID)
	values.Set("SignatureMethod", "HMAC-SHA1")
	values.Set("SignatureNonce", uuid.NewString())
	values.Set("SignatureVersion", "1.0")
	values.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))

	values.Set("Signature", aliyunSignature(values, s.cfg.AccessKeySecret))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aliyunSMSEndpoint+"?"+values.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create aliyun sms request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send aliyun sms request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read aliyun sms response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("aliyun sms status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse aliyun sms response: %w", err)
	}
	if result.Code != "OK" {
		logs.WarnContextf(ctx, "Aliyun SMS SendSms rejected: phone=%s code=%s message=%s",
			MaskPhone(phone), result.Code, result.Message)
		if isAliyunSMSRateLimited(result.Code, result.Message) {
			return fmt.Errorf("%w: %s", ErrRateLimited, result.Message)
		}
		return fmt.Errorf("aliyun sms failed: %s", result.Message)
	}
	logs.InfoContextf(ctx, "Aliyun SMS SendSms completed: phone=%s code=%s message=%s",
		MaskPhone(phone), result.Code, result.Message)
	return nil
}
