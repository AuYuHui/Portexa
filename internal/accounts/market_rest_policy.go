package accounts

import (
	"encoding/json"
	"slices"
	"sort"

	"gorm.io/gorm"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func readRestPolicy(tx *gorm.DB, revision int64) (storage.MarketRestPolicy, error) {
	var p storage.MarketRestPolicy
	err := tx.First(&p, "revision=?", revision).Error
	return p, err
}

func restExclusions(policy storage.MarketRestPolicy) ([]string, error) {
	ids := []string{}
	err := json.Unmarshal([]byte(policy.ExcludedAccountsJSON), &ids)
	return ids, err
}

func restPolicyIncludes(policy storage.MarketRestPolicy, id string, at int64) bool {
	if policy.Template != marketrest.Template || at < policy.EffectiveAtMS {
		return false
	}
	excluded, err := restExclusions(policy)
	return err == nil && !slices.Contains(excluded, id)
}

func updateRestPolicy(tx *gorm.DB, settings *storage.CollectionSettings, input CollectionInput, now int64) (bool, error) {
	template, ids := settings.MarketRestTemplate, slices.Clone(settings.MarketRestExcludedAccountIDs)
	if input.MarketRestTemplate != nil {
		template = *input.MarketRestTemplate
	}
	if template != marketrest.Off && template != marketrest.Template {
		return false, &InputError{"不支持的休市模板"}
	}
	if input.MarketRestExcludedAccountIDs != nil {
		ids = slices.Clone(*input.MarketRestExcludedAccountIDs)
		if len(ids) > 200 {
			return false, &InputError{"休市例外账户过多"}
		}
		sort.Strings(ids)
		if len(slices.Compact(slices.Clone(ids))) != len(ids) {
			return false, &InputError{"休市例外账户重复"}
		}
		if len(ids) > 0 {
			var count int64
			if err := tx.Model(&storage.Account{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
				return false, err
			}
			if count != int64(len(ids)) {
				return false, &InputError{"休市例外账户不存在"}
			}
		}
	}
	if template == settings.MarketRestTemplate && slices.Equal(ids, settings.MarketRestExcludedAccountIDs) {
		return false, nil
	}
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return false, err
	}
	var revision int64
	if err = tx.Model(&storage.MarketRestPolicy{}).Select("COALESCE(MAX(revision),0)+1").Scan(&revision).Error; err != nil {
		return false, err
	}
	policy := storage.MarketRestPolicy{Revision: revision, Template: template, EffectiveAtMS: now, ExcludedAccountsJSON: string(raw)}
	if err = tx.Create(&policy).Error; err != nil {
		return false, err
	}
	settings.MarketRestPolicyRevision, settings.MarketRestTemplate, settings.MarketRestExcludedAccountIDs = revision, template, ids
	return true, nil
}
