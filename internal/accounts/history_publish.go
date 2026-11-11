package accounts

import (
	"bytes"
	"context"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

func currentHistory(tx *gorm.DB, id string) *gorm.DB {
	return tx.Table("history_order_revisions AS o").
		Joins("JOIN history_order_heads AS h ON h.account_id=o.account_id AND h.ticket=o.ticket AND h.revision=o.revision").
		Where("o.account_id=?", id)
}

// Prepare revisions outside the short publication transaction. The durable
// account lock prevents another history writer until this run terminates.
func (s *Service) historyRevisions(ctx context.Context, job HistoryJob, orders []storage.HistoryOrder) ([]storage.HistoryOrder, error) {
	if len(orders) > HistoryPublishLimit {
		return nil, &InputError{"历史窗口超过发布预算"}
	}
	changed := make([]storage.HistoryOrder, 0, len(orders))
	seen := make(map[int32]bool, len(orders))
	for start := 0; start < len(orders); start += 500 {
		batch := orders[start:min(start+500, len(orders))]
		tickets := make([]int32, 0, len(batch))
		for _, order := range batch {
			tickets = append(tickets, order.Ticket)
		}
		var previous []storage.HistoryOrder
		if err := currentHistory(s.Store.Read.WithContext(ctx), job.Account.ID).Where("o.ticket IN ?", tickets).
			Select("o.*").Scan(&previous).Error; err != nil {
			return nil, err
		}
		byTicket := make(map[int32]int, len(previous))
		for i, order := range previous {
			byTicket[order.Ticket] = i
		}
		for _, order := range batch {
			if order.AccountID != job.Account.ID || order.SourceRunID != job.Run.ID || order.UnitRevision != job.Account.UnitRevision ||
				order.NativeCurrency != job.Run.NativeCurrency || order.Ticket <= 0 || seen[order.Ticket] || len(order.SourceHash) != 32 ||
				order.EventTimeWall < job.Coverage.StartWall || order.EventTimeWall >= job.Coverage.EndWall {
				return nil, &InputError{"历史记录不属于当前采集窗口"}
			}
			seen[order.Ticket] = true
			order.Revision = 1
			if index, ok := byTicket[order.Ticket]; ok {
				old := previous[index]
				if bytes.Equal(old.SourceHash, order.SourceHash) || old.Comment == "" && order.Comment == "" && sameHistoryFields(old, order) {
					continue
				}
				order.Revision = old.Revision + 1
			}
			if order.Command == 6 {
				order.AutomaticCategory, order.AutomaticReason = automaticFundCategory(order.Comment, order.NetProfit, order.Commission, order.Swap, order.Taxes)
				order.AutomaticRuleVersion = FundingRuleVersion
			}
			changed = append(changed, order)
		}
	}
	return changed, nil
}

// Before comments were available the source digest had a smaller shape. A
// digest format upgrade must not invalidate manual corrections on unchanged
// legacy rows; compare every retained source field in that case.
func sameHistoryFields(a, b storage.HistoryOrder) bool {
	return a.Ticket == b.Ticket && a.UnitRevision == b.UnitRevision && a.NativeCurrency == b.NativeCurrency && a.Command == b.Command &&
		a.Symbol == b.Symbol && a.Digits == b.Digits && a.VolumeHundredths == b.VolumeHundredths &&
		a.OpenTimeWall == b.OpenTimeWall && a.CloseTimeWall == b.CloseTimeWall && a.EventTimeWall == b.EventTimeWall &&
		a.OpenPrice.Equal(b.OpenPrice) && a.ClosePrice.Equal(b.ClosePrice) && a.SL.Equal(b.SL) && a.TP.Equal(b.TP) &&
		a.Profit.Equal(b.Profit) && a.Commission.Equal(b.Commission) && a.Swap.Equal(b.Swap) && a.Taxes.Equal(b.Taxes) && a.NetProfit.Equal(b.NetProfit)
}

func (s *Service) stageHistory(ctx context.Context, job HistoryJob, orders []storage.HistoryOrder) error {
	for start := 0; start < len(orders); start += 100 {
		batch := orders[start:min(start+100, len(orders))]
		if err := s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
			var run storage.HistoryRun
			if err := tx.First(&run, "id=? AND state='running'", job.Run.ID).Error; err != nil {
				return err
			}
			return tx.Create(&batch).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) PublishHistory(ctx context.Context, job HistoryJob, orders []storage.HistoryOrder, finished int64) error {
	if finished < job.Run.StartedAtMS {
		return storage.ErrConflict
	}
	changed, err := s.historyRevisions(ctx, job, orders)
	if err != nil {
		return err
	}
	if err = s.stageHistory(ctx, job, changed); err != nil {
		return err
	}
	return s.Store.Write(ctx, storage.Low, func(tx *gorm.DB) error {
		current, valid, err := historyConfiguration(tx, job)
		if err != nil {
			return err
		}
		if !valid || !current.SyncEnabled || current.CurrentSnapshotID == nil {
			return storage.ErrConflict
		}
		if _, err = s.validEndpoint(tx, job.Run.EndpointID, job.Run.ServerID); err != nil {
			return err
		}
		var snapshot storage.Snapshot
		if err = tx.First(&snapshot, "id=? AND account_id=?", *current.CurrentSnapshotID, current.ID).Error; err != nil {
			return err
		}
		if snapshot.NativeCurrency != job.Run.NativeCurrency || snapshot.UnitRevision != job.Run.UnitRevision {
			return storage.ErrConflict
		}
		var run storage.HistoryRun
		if err = tx.First(&run, "id=? AND state='running'", job.Run.ID).Error; err != nil {
			return err
		}
		if len(changed) > 0 {
			if err = tx.Exec(`INSERT INTO history_order_heads(account_id,ticket,revision)
				SELECT account_id,ticket,revision FROM history_order_revisions WHERE source_run_id=?
				ON CONFLICT(account_id,ticket) DO UPDATE SET revision=excluded.revision`, job.Run.ID).Error; err != nil {
				return err
			}
		}
		if err = tx.Model(&run).Updates(map[string]any{"state": "succeeded", "finished_at_ms": finished, "completion_reason": "complete"}).Error; err != nil {
			return err
		}
		refresh := 24 * time.Hour
		if job.Coverage.StartWall >= job.Day.Start {
			refresh = 5 * time.Minute
		}
		if err = tx.Model(&job.Coverage).Updates(map[string]any{"state": "complete", "checked_at_ms": finished, "failure_count": 0,
			"next_attempt_at_ms": finished + refresh.Milliseconds()}).Error; err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"next_history_sync_at_ms": finished + 1000, "history_sync_paused": false}).Error
	})
}
