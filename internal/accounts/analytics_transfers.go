package accounts

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"portexa/internal/analytics"
	"portexa/internal/storage"
)

type TransferInput struct {
	OutAccountID string `json:"out_account_id"`
	OutTicket    int32  `json:"out_ticket"`
	OutRevision  int64  `json:"out_revision"`
	InAccountID  string `json:"in_account_id"`
	InTicket     int32  `json:"in_ticket"`
	InRevision   int64  `json:"in_revision"`
	Reason       string `json:"reason"`
}

type transferEvidence struct {
	event        analytics.Event
	key, ruleset string
}

func (s *Service) transferEndpoint(ctx context.Context, tx *gorm.DB, id string, ticket int32, revision int64) (*transferEvidence, error) {
	tx = tx.WithContext(ctx)
	var account storage.Account
	if err := tx.First(&account, "id=?", id).Error; err != nil {
		if err == storage.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if account.USDScale == nil {
		return nil, nil
	}
	var order storage.HistoryOrder
	found := currentHistory(tx, id).Where("o.ticket=? AND o.revision=?", ticket, revision).Select("o.*").Limit(1).Scan(&order)
	if found.Error != nil {
		return nil, found.Error
	}
	if found.RowsAffected == 0 || order.Command != 6 || order.UnitRevision != account.UnitRevision {
		return nil, nil
	}
	entry, err := readFundEntry(tx, order)
	if err != nil {
		return nil, err
	}
	if entry.NeedsReview || entry.Classification == nil {
		return nil, nil
	}
	c := entry.Classification
	if c.Category != "transfer_in" && c.Category != "transfer_out" {
		return nil, nil
	}
	key := fmt.Sprintf("manual:%d", c.Revision)
	if c.Source == "automatic" {
		key = "automatic:" + c.RuleVersion
	}
	var server storage.Server
	if err := tx.First(&server, "id=?", account.ServerID).Error; err != nil {
		return nil, err
	}
	if server.ActiveTimeRulesetID == nil {
		return nil, nil
	}
	var rules []serverTimeRule
	if err := tx.Where("server_id=? AND verified_at_ms IS NOT NULL", account.ServerID).Order("revision").Find(&rules).Error; err != nil {
		return nil, err
	}
	stamp, ok := trendEventUTC(order.EventTimeWall, trendTimeEntries(rules, 0, 2147483647_000))
	if !ok {
		return nil, nil
	}
	return &transferEvidence{event: analytics.Event{TimeMS: stamp, AccountID: id, Ticket: ticket, Revision: revision, Kind: c.Category,
		Amount: order.NetProfit.Mul(*account.USDScale), Classification: key, SourceRunID: order.SourceRunID}, key: key, ruleset: *server.ActiveTimeRulesetID}, nil
}

func (s *Service) MatchAnalyticsTransfer(ctx context.Context, input TransferInput) (storage.TransferMatch, error) {
	var match storage.TransferMatch
	reason := strings.TrimSpace(input.Reason)
	if !clean(reason, 1, 500) || input.OutAccountID == input.InAccountID || input.OutTicket <= 0 || input.InTicket <= 0 || input.OutRevision <= 0 || input.InRevision <= 0 {
		return match, &InputError{"请选择不同账户的转出与转入流水，并填写核对依据"}
	}
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		out, err := s.transferEndpoint(ctx, tx, input.OutAccountID, input.OutTicket, input.OutRevision)
		if err != nil {
			return err
		}
		in, err := s.transferEndpoint(ctx, tx, input.InAccountID, input.InTicket, input.InRevision)
		if err != nil {
			return err
		}
		if out == nil || in == nil {
			return storage.ErrConflict
		}
		if out.event.Kind != "transfer_out" || in.event.Kind != "transfer_in" || out.event.Amount.Sign() >= 0 || in.event.Amount.Sign() <= 0 ||
			!out.event.Amount.Abs().Equal(in.event.Amount) || out.event.TimeMS > in.event.TimeMS {
			return &InputError{"转账方向、美元换算金额或到账顺序不匹配，费用需单独拆清"}
		}
		match = storage.TransferMatch{ID: storage.NewID(), OutAccountID: input.OutAccountID, OutTicket: input.OutTicket, OutRevision: input.OutRevision,
			InAccountID: input.InAccountID, InTicket: input.InTicket, InRevision: input.InRevision, OutClassification: out.key, InClassification: in.key,
			OutRuleset: out.ruleset, InRuleset: in.ruleset, OutMS: out.event.TimeMS, InMS: in.event.TimeMS, AmountUSD: in.event.Amount, Reason: reason, CreatedAtMS: s.Now().UnixMilli()}
		return tx.Create(&match).Error
	})
	return match, err
}
