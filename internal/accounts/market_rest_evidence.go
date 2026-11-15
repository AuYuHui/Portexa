package accounts

import (
	"context"
	"fmt"
	"time"

	"portexa/internal/analytics"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

func restPairPossible(a, b storage.Snapshot) bool {
	if a.RestWindowID != b.RestWindowID {
		return false
	}
	if !a.RestVerified || !b.RestVerified || a.RestPolicyRevision == 0 || a.RestPolicyRevision != b.RestPolicyRevision || a.StateFingerprint == "" || a.StateFingerprint != b.StateFingerprint || a.AccountID != b.AccountID || a.UnitRevision != b.UnitRevision || a.NativeCurrency != b.NativeCurrency || a.CollectionIntervalSeconds == nil {
		return false
	}
	if b.ObservedFromMS < a.ObservedToMS || b.ObservedToMS-a.ObservedToMS > int64(*a.CollectionIntervalSeconds)*1000+marketrest.Grace.Milliseconds() || a.ObservedToMS-a.ObservedFromMS > analytics.MaxAge || b.ObservedToMS-b.ObservedFromMS > analytics.MaxAge {
		return false
	}
	if a.RestWindowID != "" {
		return true
	}
	_, end, closed := marketrest.Window(time.UnixMilli(a.ObservedFromMS - 999))
	return closed && b.ObservedToMS < end.UnixMilli()
}

// restBridge requires both immutable snapshot evidence and independently
// complete ledger/time evidence. The template alone never fills a valuation.
func (s *Service) restBridge(ctx context.Context, source *analyticsSource, a, b storage.Snapshot, unit string) (bool, error) {
	if !restPairPossible(a, b) {
		return false, nil
	}
	if a.RestWindowID != "" {
		if source.restWindows == nil {
			source.restWindows = map[string]storage.MarketRestWindow{}
		}
		w, ok := source.restWindows[a.RestWindowID]
		if !ok {
			if err := s.Store.Read.WithContext(ctx).First(&w, "id=? AND account_id=? AND policy_revision=?", a.RestWindowID, a.AccountID, a.RestPolicyRevision).Error; err != nil {
				return false, err
			}
			source.restWindows[w.ID] = w
			fmt.Fprintf(source.hash, "rest-window:%s:%d:%d:%d:%s|", w.ID, w.FromMS, w.UntilMS, w.EffectiveAtMS, w.ModelIDsJSON)
		}
		if a.ObservedFromMS-999 < max(w.FromMS, w.EffectiveAtMS) || b.ObservedToMS >= w.UntilMS {
			return false, nil
		}
	}
	if source.restPolicies == nil {
		source.restPolicies = map[int64]storage.MarketRestPolicy{}
	}
	policy, ok := source.restPolicies[a.RestPolicyRevision]
	if !ok {
		var err error
		policy, err = readRestPolicy(s.Store.Read.WithContext(ctx), a.RestPolicyRevision)
		if err != nil {
			return false, err
		}
		source.restPolicies[a.RestPolicyRevision] = policy
		fmt.Fprintf(source.hash, "rest-policy:%d:%s:%d:%s|", policy.Revision, policy.Template, policy.EffectiveAtMS, policy.ExcludedAccountsJSON)
	}
	if !restPolicyIncludes(policy, a.AccountID, a.ObservedFromMS-999) || len(source.input.LedgerReasons) > 0 {
		return false, nil
	}
	from, to := a.ObservedFromMS-999, b.ObservedToMS+1
	if !source.covered(from, to) {
		return false, nil
	}
	p := source.valuation(&a, a.ObservedToMS, unit)
	p.TimeMS = b.ObservedToMS
	if analytics.SampleReason(p, source.input.Events) != "" {
		return false, nil
	}
	for _, issue := range source.input.ScopedReasons {
		if issue.FromMS < to && issue.ToMS > from {
			return false, nil
		}
	}
	ranges := eventWallRanges(source.entries, from, to)
	for _, wall := range source.unresolvedWalls {
		if wallCouldAffect(wall, ranges) {
			return false, nil
		}
	}
	return true, nil
}
