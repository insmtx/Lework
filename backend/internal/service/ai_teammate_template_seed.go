package service

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
)

const embeddedAITeammateTemplateAvatarDir = "assets/ai-teammate-template-avatar"

//go:embed assets/ai-teammate-template-avatar/*.png
var aiTeammateTemplateAvatarFS embed.FS

// SeedAITeammateTemplates initializes built-in AI teammate templates and uploads missing avatars.
func SeedAITeammateTemplates(ctx context.Context, database *gorm.DB, avatarDir string) error {
	if database == nil {
		return errors.New("database is required")
	}

	owner, err := resolveSeedOwner(ctx, database)
	if err != nil {
		return err
	}
	resolvedAvatarDir := resolveAITeammateTemplateAvatarDir(avatarDir)

	for _, template := range defaultAITeammateTemplates() {
		existing, err := infradb.GetAITeammateTemplateByCode(ctx, database, template.Code)
		if err != nil {
			return fmt.Errorf("find ai teammate template %s: %w", template.Code, err)
		}
		if existing == nil || strings.TrimSpace(existing.Avatar) == "" {
			avatar, err := uploadAITeammateTemplateAvatar(ctx, database, resolvedAvatarDir, owner, template.Code)
			if err != nil {
				return err
			}
			if avatar != "" {
				template.Avatar = avatar
			}
		}
		if err := infradb.UpsertAITeammateTemplate(ctx, database, template); err != nil {
			return fmt.Errorf("upsert ai teammate template %s: %w", template.Code, err)
		}
	}
	return nil
}

type seedOwner struct {
	orgID   uint
	ownerID uint
}

func resolveSeedOwner(ctx context.Context, database *gorm.DB) (seedOwner, error) {
	var org types.Organization
	err := database.WithContext(ctx).Where("code = ?", "default_org").First(&org).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return seedOwner{}, fmt.Errorf("find default org: %w", err)
		}
		if err := database.WithContext(ctx).Order("id ASC").First(&org).Error; err != nil {
			return seedOwner{}, fmt.Errorf("find seed org: %w", err)
		}
	}

	var user types.User
	err = database.WithContext(ctx).Where("email = ?", "admin@leros.local").First(&user).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return seedOwner{}, fmt.Errorf("find default user: %w", err)
		}
		if err := database.WithContext(ctx).Order("id ASC").First(&user).Error; err != nil {
			return seedOwner{}, fmt.Errorf("find seed user: %w", err)
		}
	}

	return seedOwner{orgID: org.ID, ownerID: user.ID}, nil
}

func uploadAITeammateTemplateAvatar(ctx context.Context, database *gorm.DB, avatarDir string, owner seedOwner, code string) (string, error) {
	data, source, err := readAITeammateTemplateAvatar(avatarDir, code)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			logs.Warnf("AI teammate template avatar missing, skip upload: %s", source)
			return "", nil
		}
		return "", fmt.Errorf("read ai teammate template avatar %s: %w", source, err)
	}

	mimeType := http.DetectContentType(data[:min(len(data), 512)])
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = mediaType
	}
	filename := code + ".png"
	file, err := filestore.Upload(ctx, database, filestore.UploadParams{
		Data:         data,
		Filename:     filename,
		OriginalName: filename,
		MimeType:     mimeType,
		OrgID:        owner.orgID,
		OwnerID:      owner.ownerID,
		ObjectKey:    fmt.Sprintf("%s/%d/ai-teammate-template/%s", filestore.PurposeAvatar, owner.orgID, filename),
		Purpose:      filestore.PurposeAvatar,
		Size:         int64(len(data)),
	})
	if err != nil {
		return "", fmt.Errorf("upload ai teammate template avatar %s: %w", code, err)
	}
	logs.Infof("AI teammate template avatar uploaded (code=%s, public_id=%s)", code, file.PublicID)
	return file.PublicID, nil
}

func readAITeammateTemplateAvatar(avatarDir string, code string) ([]byte, string, error) {
	filename := code + ".png"
	if avatarDir != "" {
		path := filepath.Join(avatarDir, filename)
		data, err := os.ReadFile(path)
		return data, path, err
	}

	path := filepath.Join(embeddedAITeammateTemplateAvatarDir, filename)
	data, err := aiTeammateTemplateAvatarFS.ReadFile(path)
	return data, path, err
}

func resolveAITeammateTemplateAvatarDir(avatarDir string) string {
	if dir := strings.TrimSpace(avatarDir); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("LEROS_AI_TEAMMATE_TEMPLATE_AVATAR_DIR")); dir != "" {
		return dir
	}
	return ""
}

func defaultAITeammateTemplates() []*types.AITeammateTemplate {
	return []*types.AITeammateTemplate{
		{
			Code:         "bid-strategist",
			Name:         "投标策略师",
			Description:  "拆解招标文件、梳理响应要点、生成投标策略和标书内容建议。",
			Provider:     "Lework",
			SystemPrompt: "你是投标策略师，擅长阅读招标文件、识别评分点、整理响应策略，并协助用户生成专业、合规、可落地的投标材料。",
			Expertise:    types.SkillStringList{"招标文件解读", "评分点拆解", "投标策略", "标书写作"},
			Category:     "bidding",
			Tags:         types.SkillStringList{"招投标", "标书", "策略"},
			SortOrder:    10,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "contract-review-expert",
			Name:         "合同审查专家",
			Description:  "识别合同风险、提炼关键条款、输出修改建议和谈判关注点。",
			Provider:     "Lework",
			SystemPrompt: "你是合同审查专家，专注于合同条款风险识别、权责边界分析、合规审查和修改建议输出。你的回复应严谨、清晰，并提示用户在重大事项上咨询专业律师。",
			Expertise:    types.SkillStringList{"合同审查", "风险识别", "条款修改", "合规建议"},
			Category:     "legal",
			Tags:         types.SkillStringList{"合同", "法务", "合规"},
			SortOrder:    20,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "data-analysis-expert",
			Name:         "数据分析专家",
			Description:  "围绕业务问题拆指标、看趋势、做对比，并输出分析结论和行动建议。",
			Provider:     "Lework",
			SystemPrompt: "你是数据分析专家，擅长将业务问题转化为分析框架，解释数据趋势、异常和相关性，并给出可执行的业务建议。",
			Expertise:    types.SkillStringList{"指标拆解", "趋势分析", "经营分析", "可视化建议"},
			Category:     "data",
			Tags:         types.SkillStringList{"数据", "分析", "报表"},
			SortOrder:    30,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "document-generation-expert",
			Name:         "文档生成专家",
			Description:  "根据目标和材料生成报告、方案、纪要、制度等结构化文档。",
			Provider:     "Lework",
			SystemPrompt: "你是文档生成专家，擅长把零散信息整理为结构清晰、表达正式、可直接修改使用的业务文档。",
			Expertise:    types.SkillStringList{"报告写作", "方案撰写", "会议纪要", "制度文档"},
			Category:     "office",
			Tags:         types.SkillStringList{"文档", "写作", "办公"},
			SortOrder:    40,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "ai-ppt-expert",
			Name:         "AI PPT 专家",
			Description:  "协助梳理演示逻辑、生成页面大纲、优化标题文案和讲述节奏。",
			Provider:     "Lework",
			SystemPrompt: "你是 AI PPT 专家，擅长把复杂内容转化为清晰的演示结构，优化页面标题、信息层级、故事线和汇报表达。",
			Expertise:    types.SkillStringList{"演示结构", "页面大纲", "汇报文案", "故事线"},
			Category:     "office",
			Tags:         types.SkillStringList{"PPT", "汇报", "演示"},
			SortOrder:    50,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "recruiting-expert",
			Name:         "招聘专家",
			Description:  "辅助岗位画像、JD 编写、简历筛选、面试题设计和候选人评估。",
			Provider:     "Lework",
			SystemPrompt: "你是招聘专家，擅长理解岗位需求、设计招聘流程、优化 JD、筛选简历并生成结构化面试问题和评估建议。",
			Expertise:    types.SkillStringList{"岗位画像", "JD 优化", "简历筛选", "面试评估"},
			Category:     "hr",
			Tags:         types.SkillStringList{"招聘", "人力资源", "面试"},
			SortOrder:    60,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
		{
			Code:         "stock-investment-expert",
			Name:         "股票投资专家",
			Description:  "整理公司基本面、行业信息和风险因素，辅助形成投资研究框架。",
			Provider:     "Lework",
			SystemPrompt: "你是股票投资专家，擅长从公开信息中整理公司基本面、行业趋势、估值线索和风险因素。你的内容仅供研究参考，不构成投资建议。",
			Expertise:    types.SkillStringList{"基本面研究", "行业分析", "风险提示", "投资框架"},
			Category:     "finance",
			Tags:         types.SkillStringList{"股票", "投资", "研究"},
			SortOrder:    70,
			Status:       string(contract.AITeammateTemplateStatusActive),
			IsSystem:     true,
		},
	}
}
