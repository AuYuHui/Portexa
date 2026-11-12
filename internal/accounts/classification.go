package accounts

import (
	"context"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

type ClassificationInput struct {
	TradeRevision          int64  `json:"trade_revision"`
	ClassificationRevision int64  `json:"classification_revision"`
	Category               string `json:"category"`
	Reason                 string `json:"reason"`
}

func (s *Service) ClassifyFund(ctx context.Context, id string, ticket int32, input ClassificationInput) (storage.CashFlowClassification, error) {
	var result storage.CashFlowClassification
	reason := strings.TrimSpace(strings.ReplaceAll(input.Reason, "\r\n", "\n"))
	if ticket <= 0 || input.TradeRevision <= 0 || input.ClassificationRevision < 0 || reason == "" || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(reason) > 500 || strings.ContainsAny(reason, "\x00\r") {
		return result, &InputError{"请填写有效的流水版本和分类依据（最多500字符）"}
	}
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if account.CurrentSnapshotID == nil {
			return storage.ErrConflict
		}
		var snapshot storage.Snapshot
		if err := tx.First(&snapshot, "id=? AND account_id=?", *account.CurrentSnapshotID, id).Error; err != nil {
			return err
		}
		if snapshot.UnitRevision != account.UnitRevision {
			return storage.ErrConflict
		}
		var order storage.HistoryOrder
		found := fundingHistory(tx, account, snapshot).Where("o.ticket=?", ticket).Select("o.*").Limit(1).Scan(&order)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return storage.ErrNotFound
		}
		if order.Revision != input.TradeRevision {
			return storage.ErrConflict
		}
		if order.Command != 6 {
			return &InputError{"信用金与交易记录不能分类为出入金"}
		}
		sign := order.NetProfit.Sign()
		switch input.Category {
		case "deposit", "rebate", "profit_share_income", "transfer_in":
			if sign <= 0 {
				return &InputError{"该分类要求正向余额流水"}
			}
		case "withdrawal", "fee", "profit_share_expense", "transfer_out":
			if sign >= 0 {
				return &InputError{"该分类要求负向余额流水"}
			}
		case "adjustment", "capital_adjustment", "unclassified":
		default:
			return &InputError{"不支持的资金分类"}
		}
		var previous storage.CashFlowClassification
		if err := tx.Where("account_id=? AND ticket=?", id, ticket).Order("revision DESC").Limit(1).Find(&previous).Error; err != nil {
			return err
		}
		if previous.Revision != input.ClassificationRevision {
			return storage.ErrConflict
		}
		result = storage.CashFlowClassification{AccountID: id, Ticket: ticket, Revision: previous.Revision + 1,
			BasedTradeRevision: order.Revision, Category: input.Category, Reason: reason, CreatedAtMS: s.Now().UnixMilli()}
		result.Source = "manual"
		return tx.Create(&result).Error
	})
	return result, err
}
