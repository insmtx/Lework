package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

type skillTranslationSource struct {
	sourceType  string
	sourceID    uint
	revision    *types.PluginRevision
	name        string
	description string
	content     string
}

type skillTranslationKey struct {
	sourceType string
	sourceID   uint
	revisionID uint
}

// SkillDisplayTranslationService translates display-only Skill metadata and documents.
// It is safe to share across plugin, marketplace, and project services.
type SkillDisplayTranslationService struct {
	db         *gorm.DB
	translator SkillDescriptionTranslator
}

// NewSkillDisplayTranslationService creates the process-shared Skill display translation service.
func NewSkillDisplayTranslationService(database *gorm.DB) *SkillDisplayTranslationService {
	return newSkillDisplayTranslationService(database, NewDefaultSkillDescriptionTranslator(database))
}

func newSkillDisplayTranslationService(
	database *gorm.DB,
	translator SkillDescriptionTranslator,
) *SkillDisplayTranslationService {
	return &SkillDisplayTranslationService{db: database, translator: translator}
}

func (s *SkillDisplayTranslationService) translateMetadata(
	ctx context.Context,
	orgID uint,
	sources []skillTranslationSource,
) map[skillTranslationKey]TranslatedSkillText {
	result := make(map[skillTranslationKey]TranslatedSkillText)
	if s == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata use=false reason=service_unavailable", orgID)
		return result
	}
	if s.db == nil || s.translator == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata use=false reason=translator_unavailable", orgID)
		return result
	}
	if orgID == 0 {
		logs.WarnContextf(ctx, "Skill display translation not used: phase=metadata use=false reason=organization_missing")
		return result
	}
	if len(sources) == 0 {
		logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=metadata use=false reason=no_sources", orgID)
		return result
	}

	sourceIDsByType := make(map[string][]uint)
	seenSourceIDs := make(map[string]map[uint]struct{})
	for _, source := range sources {
		if source.sourceType == "" || source.sourceID == 0 || source.revision == nil || source.revision.ID == 0 {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=invalid_source",
				orgID, source.sourceType, source.sourceID, revisionID(source.revision))
			continue
		}
		if seenSourceIDs[source.sourceType] == nil {
			seenSourceIDs[source.sourceType] = make(map[uint]struct{})
		}
		if _, exists := seenSourceIDs[source.sourceType][source.sourceID]; !exists {
			seenSourceIDs[source.sourceType][source.sourceID] = struct{}{}
			sourceIDsByType[source.sourceType] = append(sourceIDsByType[source.sourceType], source.sourceID)
		}
	}

	cacheByKey := make(map[skillTranslationKey]types.PluginTranslation)
	for sourceType, sourceIDs := range sourceIDsByType {
		cached, err := infradb.ListPluginTranslations(ctx, s.db, orgID, sourceType, sourceIDs, translationLocale)
		if err != nil {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s use=false reason=cache_read_failed: %v",
				orgID, sourceType, err)
			return result
		}
		for _, translation := range cached {
			cacheByKey[skillTranslationKey{
				sourceType: translation.SourceType,
				sourceID:   translation.SourceID,
				revisionID: translation.PluginRevisionID,
			}] = translation
		}
	}

	pending := make([]TranslateItem, 0, len(sources))
	pendingSources := make(map[string]skillTranslationSource, len(sources))
	seenTranslationKeys := make(map[skillTranslationKey]struct{}, len(sources))
	for _, source := range sources {
		if source.sourceType == "" || source.sourceID == 0 || source.revision == nil || source.revision.ID == 0 {
			continue
		}
		key := skillTranslationKey{
			sourceType: source.sourceType,
			sourceID:   source.sourceID,
			revisionID: source.revision.ID,
		}
		if _, seen := seenTranslationKeys[key]; seen {
			continue
		}
		seenTranslationKeys[key] = struct{}{}
		cache, cacheOK := cacheByKey[key]
		sourceHash := skillMetadataHash(source.name, source.description)
		if cacheOK {
			if cache.MetadataSourceHash != sourceHash {
				logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=source_hash_changed",
					orgID, source.sourceType, source.sourceID, source.revision.ID)
			} else {
				cachedText, complete := validatedMetadataTranslation(source.name, source.description, TranslatedSkillText{
					DisplayName: cache.TranslatedName,
					Description: cache.TranslatedDescription,
				})
				if complete {
					logs.DebugContextf(ctx, "Skill display translation cache used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=true",
						orgID, source.sourceType, source.sourceID, source.revision.ID)
					result[key] = cachedText
					continue
				}
				logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=cached_result_incomplete",
					orgID, source.sourceType, source.sourceID, source.revision.ID)
			}
		}
		if !displayNameNeedsChineseTranslation(source.name) && !descriptionNeedsChineseTranslation(source.description) {
			logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=source_already_fully_chinese",
				orgID, source.sourceType, source.sourceID, source.revision.ID)
			continue
		}

		translationKey := skillTranslationKeyString(source.sourceType, source.sourceID, source.revision.ID)
		pending = append(pending, TranslateItem{
			SkillID:     translationKey,
			Name:        source.name,
			Description: source.description,
		})
		pendingSources[translationKey] = source
	}
	if len(pending) == 0 {
		return result
	}

	translations, translateErr := s.translator.Translate(ctx, orgID, pending)
	for key, source := range pendingSources {
		translated, exists := translations[key]
		if !exists {
			if translateErr != nil {
				logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=translator_error: %v",
					orgID, source.sourceType, source.sourceID, source.revision.ID, translateErr)
			} else {
				logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=result_missing",
					orgID, source.sourceType, source.sourceID, source.revision.ID)
			}
			continue
		}
		validated, complete := validatedMetadataTranslation(source.name, source.description, translated)
		translationKey := skillTranslationKey{
			sourceType: source.sourceType,
			sourceID:   source.sourceID,
			revisionID: source.revision.ID,
		}
		if !complete {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=false reason=result_incomplete",
				orgID, source.sourceType, source.sourceID, source.revision.ID)
			continue
		}
		result[translationKey] = validated
		if err := infradb.UpsertPluginTranslationMetadata(ctx, s.db, &types.PluginTranslation{
			OrgID:                 orgID,
			SourceType:            source.sourceType,
			SourceID:              source.sourceID,
			PluginRevisionID:      source.revision.ID,
			SourceRevision:        source.revision.Revision,
			Locale:                translationLocale,
			MetadataSourceHash:    skillMetadataHash(source.name, source.description),
			TranslatedName:        validated.DisplayName,
			TranslatedDescription: validated.Description,
			CreatedAt:             time.Now(),
			UpdatedAt:             time.Now(),
		}); err != nil {
			logs.WarnContextf(ctx, "Skill display translation applied but cache not saved: org=%d phase=metadata source_type=%s source_id=%d revision_id=%d use=true reason=cache_write_failed: %v",
				orgID, source.sourceType, source.sourceID, source.revision.ID, err)
		}
	}
	return result
}

func (s *SkillDisplayTranslationService) translateDocumentBody(
	ctx context.Context,
	orgID uint,
	source skillTranslationSource,
) (string, error) {
	if s == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document use=false reason=service_unavailable", orgID)
		return "", nil
	}
	if s.db == nil || s.translator == nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document use=false reason=translator_unavailable", orgID)
		return "", nil
	}
	if orgID == 0 || source.sourceID == 0 || source.revision == nil || source.revision.ID == 0 || strings.TrimSpace(source.content) == "" {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=invalid_source",
			orgID, source.sourceType, source.sourceID, revisionID(source.revision))
		return "", nil
	}
	if !skillDocumentNeedsChineseTranslation(source.content) {
		logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=source_already_fully_chinese",
			orgID, source.sourceType, source.sourceID, source.revision.ID)
		return "", nil
	}

	cached, err := infradb.ListPluginTranslations(ctx, s.db, orgID, source.sourceType, []uint{source.sourceID}, translationLocale)
	if err != nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=cache_read_failed: %v",
			orgID, source.sourceType, source.sourceID, source.revision.ID, err)
		return "", err
	}
	documentHash := skillDocumentHash(source.content)
	matchedCache := false
	for _, translation := range cached {
		if translation.PluginRevisionID != source.revision.ID || translation.SkillMDSourceHash != documentHash {
			continue
		}
		matchedCache = true
		if strings.TrimSpace(translation.TranslatedSkillMD) == "" {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=cached_result_empty",
				orgID, source.sourceType, source.sourceID, source.revision.ID)
			continue
		}
		body, validateErr := translatedSkillDocumentBody(translation.TranslatedSkillMD, source.content)
		if validateErr == nil {
			logs.DebugContextf(ctx, "Skill display translation cache used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=true",
				orgID, source.sourceType, source.sourceID, source.revision.ID)
			return body, nil
		}
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=cached_result_invalid: %v",
			orgID, source.sourceType, source.sourceID, source.revision.ID, validateErr)
	}
	if len(cached) > 0 && !matchedCache {
		logs.InfoContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=source_hash_or_revision_changed",
			orgID, source.sourceType, source.sourceID, source.revision.ID)
	}

	translationKey := skillTranslationKeyString(source.sourceType, source.sourceID, source.revision.ID)
	translations, translateErr := s.translator.TranslateDocument(ctx, orgID, []TranslateDocumentItem{{
		SkillID: translationKey, Content: source.content,
	}})
	translatedContent := strings.TrimSpace(translations[translationKey])
	if translatedContent == "" {
		if translateErr != nil {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=translator_error: %v",
				orgID, source.sourceType, source.sourceID, source.revision.ID, translateErr)
		} else {
			logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=result_missing",
				orgID, source.sourceType, source.sourceID, source.revision.ID)
		}
		return "", translateErr
	}
	body, err := translatedSkillDocumentBody(translatedContent, source.content)
	if err != nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=result_invalid: %v",
			orgID, source.sourceType, source.sourceID, source.revision.ID, err)
		return "", err
	}
	if err := infradb.UpsertPluginTranslationDocument(ctx, s.db, &types.PluginTranslation{
		OrgID:             orgID,
		SourceType:        source.sourceType,
		SourceID:          source.sourceID,
		PluginRevisionID:  source.revision.ID,
		SourceRevision:    source.revision.Revision,
		Locale:            translationLocale,
		SkillMDSourceHash: documentHash,
		TranslatedSkillMD: translatedContent,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}); err != nil {
		logs.WarnContextf(ctx, "Skill display translation not used: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=false reason=cache_write_failed: %v",
			orgID, source.sourceType, source.sourceID, source.revision.ID, err)
		return "", err
	}
	logs.DebugContextf(ctx, "Skill display translation applied: org=%d phase=document source_type=%s source_id=%d revision_id=%d use=true",
		orgID, source.sourceType, source.sourceID, source.revision.ID)
	return body, nil
}

func validatedMetadataTranslation(
	name string,
	description string,
	translated TranslatedSkillText,
) (TranslatedSkillText, bool) {
	result := TranslatedSkillText{}
	nameNeedsTranslation := displayNameNeedsChineseTranslation(name)
	descriptionNeedsTranslation := descriptionNeedsChineseTranslation(description)
	if nameNeedsTranslation && strings.TrimSpace(translated.DisplayName) != "" {
		result.DisplayName = strings.TrimSpace(translated.DisplayName)
	}
	if descriptionNeedsTranslation && strings.TrimSpace(translated.Description) != "" {
		result.Description = strings.TrimSpace(translated.Description)
	}
	complete := (!nameNeedsTranslation || result.DisplayName != "") &&
		(!descriptionNeedsTranslation || result.Description != "")
	return result, complete
}

func displayNameSatisfiesThreshold(value string) bool {
	return utils.CJKRatioMarkdown(strings.TrimSpace(value)) >= displayNameChineseThreshold
}

func descriptionSatisfiesThreshold(value string) bool {
	return utils.CJKRatioMarkdown(strings.TrimSpace(value)) >= cjkTranslationThreshold
}

func translatedSkillDocumentBody(translated, original string) (string, error) {
	translatedManifest, translatedBody, err := skillcatalog.ParseDocument([]byte(translated))
	if err != nil {
		return "", fmt.Errorf("parse translated SKILL.md: %w", err)
	}
	originalManifest, _, err := skillcatalog.ParseDocument([]byte(original))
	if err != nil {
		return "", fmt.Errorf("parse source SKILL.md: %w", err)
	}
	if translatedManifest != nil && originalManifest != nil && translatedManifest.Name != originalManifest.Name {
		return "", fmt.Errorf("translated SKILL.md changed frontmatter name")
	}
	if skillDocumentChineseRatio(translated) < cjkTranslationThreshold {
		return "", fmt.Errorf("translated SKILL.md remains below Chinese threshold")
	}
	return translatedBody, nil
}

func displayNameNeedsChineseTranslation(value string) bool {
	return strings.TrimSpace(value) != "" && !displayNameSatisfiesThreshold(value)
}

func descriptionNeedsChineseTranslation(value string) bool {
	return strings.TrimSpace(value) != "" && !descriptionSatisfiesThreshold(value)
}

func revisionID(revision *types.PluginRevision) uint {
	if revision == nil {
		return 0
	}
	return revision.ID
}

func skillDocumentNeedsChineseTranslation(content string) bool {
	return strings.TrimSpace(content) != "" && skillDocumentChineseRatio(content) < cjkTranslationThreshold
}

func skillDocumentChineseRatio(content string) float64 {
	manifest, body, err := skillcatalog.ParseDocument([]byte(content))
	if err != nil {
		return utils.CJKRatioMarkdown(content)
	}
	naturalLanguage := body
	if manifest != nil {
		naturalLanguage += "\n" + manifest.Description
	}
	return utils.CJKRatioMarkdown(naturalLanguage)
}

func skillMetadataHash(name, description string) string {
	hash := sha256.Sum256([]byte(name + "\x00" + description))
	return hex.EncodeToString(hash[:])
}

func skillDocumentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func skillTranslationKeyString(sourceType string, sourceID, revisionID uint) string {
	return fmt.Sprintf("%s:%d:%d", sourceType, sourceID, revisionID)
}

func applyTranslatedMetadata(view *contract.PluginView, translated TranslatedSkillText) {
	if view == nil {
		return
	}
	if translated.DisplayName != "" {
		view.DisplayName = translated.DisplayName
	}
	if translated.Description != "" {
		view.Description = translated.Description
	}
}

func applyTranslatedMarketplaceMetadata(view *contract.OfficialPluginMarketplaceItemView, translated TranslatedSkillText) {
	if view == nil {
		return
	}
	if translated.DisplayName != "" {
		view.DisplayName = translated.DisplayName
	}
	if translated.Description != "" {
		view.Description = translated.Description
	}
}

func applyTranslatedProjectMetadata(view *contract.ProjectPlugin, translated TranslatedSkillText) {
	if view == nil {
		return
	}
	if translated.DisplayName != "" {
		view.DisplayName = translated.DisplayName
	}
	if translated.Description != "" {
		view.Description = translated.Description
	}
}
