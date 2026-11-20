package accounts

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

type DealsPage struct {
	Items      []storage.HistoryOrder `json:"items"`
	NextCursor string                 `json:"next_cursor"`
}

func (s *Service) Deals(ctx context.Context, id, cursor string, limit int) (DealsPage, error) {
	page := DealsPage{Items: []storage.HistoryOrder{}}
	if limit < 1 || limit > 100 {
		return page, &InputError{"查询条数需要在1至100之间"}
	}
	wall, ticket, err := parseHistoryCursor(cursor, (1<<31-1)*1000+999)
	if err != nil {
		return page, err
	}
	err = s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if account.CurrentSnapshotID == nil {
			return nil
		}
		var snapshot storage.Snapshot
		if err := tx.First(&snapshot, "id=? AND account_id=?", *account.CurrentSnapshotID, id).Error; err != nil {
			return err
		}
		if snapshot.UnitRevision != account.UnitRevision {
			return nil
		}
		query := fundingHistory(tx, account, snapshot).
			Where("o.mt5_deal_type IS NOT NULL AND o.mt5_wall_time_ms IS NOT NULL")
		if cursor != "" {
			query = query.Where("(o.mt5_wall_time_ms,o.ticket)<(?,?)", wall, ticket)
		}
		if err := query.Select("o.*").Order("o.mt5_wall_time_ms DESC,o.ticket DESC").Limit(limit + 1).Scan(&page.Items).Error; err != nil {
			return err
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			last := page.Items[limit-1]
			page.NextCursor = fmt.Sprintf("%d:%d", *last.MT5WallTimeMS, last.Ticket)
		}
		return nil
	})
	return page, err
}

func parseHistoryCursor(cursor string, maxWall int64) (int64, int64, error) {
	if cursor == "" {
		return 0, 0, nil
	}
	stamp, id, ok := strings.Cut(cursor, ":")
	wall, wallErr := strconv.ParseInt(stamp, 10, 64)
	ticket, ticketErr := strconv.ParseInt(id, 10, 64)
	if !ok || wallErr != nil || wall < 0 || wall > maxWall || ticketErr != nil || ticket <= 0 || ticket > maxBrowserInteger {
		return 0, 0, &InputError{"分页游标无效"}
	}
	return wall, ticket, nil
}

func historyTicketSupported(tx *gorm.DB, serverID string, ticket int64) (bool, error) {
	if ticket <= 1<<31-1 {
		return true, nil
	}
	if ticket > maxBrowserInteger {
		return false, nil
	}
	var server storage.Server
	if err := tx.Select("protocol").First(&server, "id = ?", serverID).Error; err != nil {
		return false, err
	}
	return server.Protocol == "mt5", nil
}
