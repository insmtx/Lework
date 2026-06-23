package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/insmtx/Leros/backend/config"
	"github.com/ygpkg/yg-go/logs"
)

const aliyunSMSEndpoint = "https://dysmsapi.aliyuncs.com/"

type smsSender interface {
	SendVerificationCode(ctx context.Context, phone string, code string) error
	Enabled() bool
}

type noopSMSSender struct{}

func (noopSMSSender) SendVerificationCode(ctx context.Context, phone string, code string) error {
	logs.InfoContextf(ctx, "SMS verification code test mode: phone=%s code=%s", maskPhone(phone), code)
	return nil
}

func (noopSMSSender) Enabled() bool {
	return false
}

type aliyunSMSSender struct {
	cfg        config.AliyunConfig
	httpClient *http.Client
}

func newSMSSender(cfg *config.AliyunConfig) smsSender {
	if cfg == nil || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" ||
		strings.TrimSpace(cfg.SignName) == "" || strings.TrimSpace(cfg.TemplateCode) == "" {
		return noopSMSSender{}
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

func (s *aliyunSMSSender) Enabled() bool {
	return true
}

func (s *aliyunSMSSender) SendVerificationCode(ctx context.Context, phone string, code string) error {
	logs.InfoContextf(ctx, "Aliyun SMS SendSms request: phone=%s sign_name=%s template_code=%s region_id=%s",
		maskPhone(phone), s.cfg.SignName, s.cfg.TemplateCode, s.cfg.RegionID)

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
			maskPhone(phone), result.Code, result.Message)
		return fmt.Errorf("aliyun sms failed: %s", result.Message)
	}
	logs.InfoContextf(ctx, "Aliyun SMS SendSms completed: phone=%s code=%s message=%s",
		maskPhone(phone), result.Code, result.Message)
	return nil
}

func aliyunSignature(values url.Values, accessKeySecret string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "Signature" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	encoded := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded = append(encoded, aliyunPercentEncode(key)+"="+aliyunPercentEncode(values.Get(key)))
	}
	stringToSign := "GET&%2F&" + aliyunPercentEncode(strings.Join(encoded, "&"))
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func aliyunPercentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}
