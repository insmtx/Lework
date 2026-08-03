package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/integration/feishu"
	"github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

const (
	maxFeedbackContentRunes    = 300
	maxFeedbackAttachmentCount = 9
)

var (
	ErrFeedbackNotConfigured     = errors.New("feedback service is not configured")
	ErrFeedbackNotAuthenticated  = errors.New("not authenticated")
	ErrFeedbackTypeRequired      = errors.New("feedback type is required")
	ErrFeedbackInvalidType       = errors.New("invalid feedback type")
	ErrFeedbackContentRequired   = errors.New("feedback content is required")
	ErrFeedbackContentTooLong    = errors.New("feedback content exceeds limit")
	ErrFeedbackTooManyFiles      = errors.New("too many attachments")
	ErrFeedbackAttachmentMissing = errors.New("attachment not found")
	ErrFeedbackSubmitFailed      = errors.New("submit feedback failed")
	ErrFeedbackFeishuPermission  = errors.New("飞书应用缺少多维表格编辑权限，请在「问题反馈」表中通过「…」→「添加文档应用」授权该应用")
)

var feedbackTypeLabels = map[string]string{
	"problem":    "BUG",
	"suggestion": "优化",
	"experience": "体验",
	"other":      "其他",
}

type SubmitFeedbackRequest struct {
	OrgID         uint
	Uin           uint
	Type          string
	Content       string
	AttachmentIDs []string
	Client        SubmitFeedbackClientInfo
}

type SubmitFeedbackClientInfo struct {
	Platform string
	Version  string
}

type SubmitFeedbackResult struct {
	Status string `json:"status"`
}

type feedbackJob struct {
	orgID          uint
	uin            uint
	typeLabel      string
	content        string
	attachmentIDs  []string
	version        string
	submitterName  string
	submitterPhone string
}

type FeedbackService struct {
	db           *gorm.DB
	files        contract.FileService
	feishu       *feishu.Client
	modelInvoker modelrouter.Invoker
	userRepo     account.UserRepository
}

func NewFeedbackService(db *gorm.DB, files contract.FileService, cfg *config.FeishuConfig, modelInvoker modelrouter.Invoker, userRepo account.UserRepository) *FeedbackService {
	svc := &FeedbackService{db: db, files: files, modelInvoker: modelInvoker, userRepo: userRepo}
	if cfg != nil && cfg.Enabled {
		svc.feishu = feishu.NewClient(cfg.AppID, cfg.AppSecret, cfg.AppToken, cfg.TableID)
	}
	return svc
}

func (s *FeedbackService) SubmitFeedback(ctx context.Context, req *SubmitFeedbackRequest) (*SubmitFeedbackResult, error) {
	if s == nil || s.feishu == nil {
		return nil, ErrFeedbackNotConfigured
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	if req.OrgID == 0 || req.Uin == 0 {
		return nil, ErrFeedbackNotAuthenticated
	}

	job, err := s.validateFeedbackJob(ctx, req)
	if err != nil {
		return nil, err
	}

	go s.processFeedbackAsync(context.WithoutCancel(ctx), job)

	return &SubmitFeedbackResult{Status: "accepted"}, nil
}

func (s *FeedbackService) validateFeedbackJob(ctx context.Context, req *SubmitFeedbackRequest) (*feedbackJob, error) {
	feedbackType := strings.TrimSpace(req.Type)
	content := strings.TrimSpace(req.Content)
	if feedbackType == "" {
		return nil, ErrFeedbackTypeRequired
	}
	typeLabel, ok := feedbackTypeLabels[feedbackType]
	if !ok {
		return nil, ErrFeedbackInvalidType
	}
	if content == "" {
		return nil, ErrFeedbackContentRequired
	}
	if utf8.RuneCountInString(content) > maxFeedbackContentRunes {
		return nil, fmt.Errorf("%w: %d", ErrFeedbackContentTooLong, maxFeedbackContentRunes)
	}
	if len(req.AttachmentIDs) > maxFeedbackAttachmentCount {
		return nil, ErrFeedbackTooManyFiles
	}

	user, err := s.userRepo.GetUserByUin(ctx, req.Uin)
	if err != nil || user == nil {
		return nil, ErrFeedbackSubmitFailed
	}

	for _, publicID := range req.AttachmentIDs {
		publicID = strings.TrimSpace(publicID)
		if publicID == "" {
			continue
		}
		if _, err := infradb.GetFileUploadByPublicID(ctx, s.db, req.OrgID, publicID); err != nil {
			return nil, ErrFeedbackAttachmentMissing
		}
	}

	submitterName, submitterPhone := resolveFeedbackSubmitter(user.Name, user.Phone)
	return &feedbackJob{
		orgID:          req.OrgID,
		uin:            req.Uin,
		typeLabel:      typeLabel,
		content:        content,
		attachmentIDs:  append([]string(nil), req.AttachmentIDs...),
		version:        strings.TrimSpace(req.Client.Version),
		submitterName:  submitterName,
		submitterPhone: submitterPhone,
	}, nil
}

func (s *FeedbackService) processFeedbackAsync(ctx context.Context, job *feedbackJob) {
	if s == nil || job == nil {
		return
	}

	summary := summarizeFeedbackBestEffort(ctx, s.db, s.modelInvoker, job.orgID, job.typeLabel, job.content, job.uin)

	attachmentTokens, err := s.uploadAttachments(ctx, job.orgID, job.attachmentIDs)
	if err != nil {
		logs.ErrorContextf(ctx, "feedback async upload attachments failed: %v", err)
		return
	}

	fields := buildFeedbackRecordFields(
		job.typeLabel,
		job.content,
		job.submitterName,
		job.submitterPhone,
		job.version,
		summary,
		attachmentTokens,
	)

	logs.InfoContextf(ctx, "feedback async create record: title=%q submitter=%q phone=%q version=%q attachments=%d",
		fields["问题名称"], fields["提交人"], fields["手机号"], fields["版本号"], len(attachmentTokens))

	recordID, err := s.feishu.CreateFeedbackRecord(ctx, fields)
	if err != nil {
		logs.ErrorContextf(ctx, "feedback async create feishu record failed: %v", err)
		return
	}

	logs.InfoContextf(ctx, "feedback async submitted record_id=%s", recordID)
}

func (s *FeedbackService) uploadAttachments(ctx context.Context, orgID uint, attachmentIDs []string) ([]string, error) {
	if len(attachmentIDs) == 0 {
		return nil, nil
	}

	tokens := make([]string, 0, len(attachmentIDs))
	for _, publicID := range attachmentIDs {
		publicID = strings.TrimSpace(publicID)
		if publicID == "" {
			continue
		}

		if _, err := infradb.GetFileUploadByPublicID(ctx, s.db, orgID, publicID); err != nil {
			return nil, ErrFeedbackAttachmentMissing
		}

		reader, info, err := s.files.DownloadFile(ctx, orgID, publicID)
		if err != nil {
			return nil, ErrFeedbackSubmitFailed
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, ErrFeedbackSubmitFailed
		}
		if closeErr != nil {
			logs.WarnContextf(ctx, "close attachment reader failed: %v", closeErr)
		}

		filename := info.FileName
		if filename == "" {
			filename = publicID
		}
		isImage := strings.HasPrefix(strings.ToLower(info.MimeType), "image/")
		token, err := s.feishu.UploadAttachment(ctx, filename, data, isImage)
		if err != nil {
			if isFeishuPermissionError(err) {
				return nil, ErrFeedbackFeishuPermission
			}
			logs.WarnContextf(ctx, "upload attachment to feishu failed public_id=%s: %v, skipping attachment", publicID, err)
			continue
		}
		tokens = append(tokens, token)
	}

	return tokens, nil
}

func isFeishuPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "1061004") ||
		strings.Contains(msg, "91403") ||
		strings.Contains(msg, "1254302") ||
		strings.Contains(msg, "permission denied")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	count := 0
	for index := range value {
		if count == limit {
			return value[:index] + "…"
		}
		count++
	}
	return value
}
