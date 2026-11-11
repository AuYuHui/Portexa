package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	sqlite "modernc.org/sqlite"
)

// Backup uses SQLite's online API, so committed WAL contents are included.
// The destination is new, verified, and atomically published. Keys are separate.
func (s *Store) Backup(ctx context.Context, destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if abs == s.path {
		return errors.New("backup must be separate from the database")
	}
	if _, err = os.Lstat(abs); !errors.Is(err, os.ErrNotExist) {
		return errors.New("backup destination must not exist")
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".backup-*.sqlite")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err = tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	connection, err := s.readSQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	err = connection.Raw(func(raw any) error {
		capable, ok := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite online backup is unavailable")
		}
		u := url.URL{Scheme: "file", Path: tmpPath}
		backup, err := capable.NewBackup(u.String())
		if err != nil {
			return err
		}
		for {
			if err = ctx.Err(); err != nil {
				return errors.Join(err, backup.Finish())
			}
			more, err := backup.Step(128)
			if err != nil {
				return errors.Join(err, backup.Finish())
			}
			if !more {
				return backup.Finish()
			}
		}
	})
	if err != nil {
		return err
	}
	if err = ValidateDatabase(ctx, tmpPath); err != nil {
		return err
	}
	file, err := os.OpenFile(tmpPath, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = errors.Join(file.Sync(), file.Close())
	if err != nil {
		return err
	}
	// Link refuses an existing destination even if another backup raced us.
	if err = os.Link(tmpPath, abs); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(abs))
}

func ValidateDatabase(ctx context.Context, path string) error {
	db, err := openSQL(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var application, version int
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&application); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if application != ApplicationID || version < 1 || version > SchemaVersion {
		return errors.New("backup schema is not supported")
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return errors.New("backup integrity check failed")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup foreign key check failed")
	}
	return rows.Err()
}

// RestoreNew creates a new destination only. The CLI additionally holds the
// process lock; an existing database is never silently overwritten.
func RestoreNew(ctx context.Context, source, destination string) error {
	if err := ValidateDatabase(ctx, source); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("restore destination must not exist; use a new data directory")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, "database.sqlite")
	store, err := Open(tempPath)
	if err != nil {
		return err
	}
	conn, err := store.writeSQL.Conn(ctx)
	if err != nil {
		store.Close()
		return err
	}
	err = conn.Raw(func(raw any) error {
		capable := raw.(interface {
			NewRestore(string) (*sqlite.Backup, error)
		})
		u := url.URL{Scheme: "file", Path: source}
		backup, err := capable.NewRestore(u.String())
		if err != nil {
			return err
		}
		for {
			if err = ctx.Err(); err != nil {
				return errors.Join(err, backup.Finish())
			}
			more, err := backup.Step(128)
			if err != nil {
				return errors.Join(err, backup.Finish())
			}
			if !more {
				return backup.Finish()
			}
		}
	})
	conn.Close()
	if err == nil {
		err = migrate(store.writeSQL)
	}
	if err == nil {
		err = store.Write(ctx, High, func(tx *gorm.DB) error {
			now := time.Now().UnixMilli()
			if err := tx.Model(&Account{}).Where("1 = 1").Updates(map[string]any{"sync_enabled": false, "config_version": gorm.Expr("config_version + 1")}).Error; err != nil {
				return err
			}
			if err := tx.Model(&Session{}).Where("revoked_at_ms IS NULL").Update("revoked_at_ms", now).Error; err != nil {
				return err
			}
			if err := tx.Model(&SyncRun{}).Where("state IN ('running', 'queued')").Updates(map[string]any{"state": "interrupted", "finished_at_ms": now, "completion_reason": "restored"}).Error; err != nil {
				return err
			}
			if err := tx.Model(&HistoryRun{}).Where("state='running'").Updates(map[string]any{
				"state": "interrupted", "finished_at_ms": gorm.Expr("MAX(?,started_at_ms)", now), "completion_reason": "restored",
			}).Error; err != nil {
				return err
			}
			if err := tx.Model(&HistoryCoverage{}).Where("state='running'").Updates(map[string]any{
				"state": "pending", "next_attempt_at_ms": 0,
			}).Error; err != nil {
				return err
			}
			if err := DiscardUnpublishedHistory(tx); err != nil {
				return err
			}
			if err := tx.Model(&DailyReport{}).Where("state = 'running'").Updates(map[string]any{
				"state": "interrupted", "finished_at_ms": now, "completion_reason": "restored",
			}).Error; err != nil {
				return err
			}
			return tx.Model(&ClockRun{}).Where("state = 'running'").Updates(map[string]any{
				"state": "interrupted", "finished_at_ms": now, "completion_reason": "restored",
			}).Error
		})
	}
	err = errors.Join(err, store.Close())
	if err != nil {
		return err
	}
	if err = ValidateDatabase(ctx, tempPath); err != nil {
		return err
	}
	if err = os.Link(tempPath, destination); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (s *Store) Checkpoint(ctx context.Context) error {
	return s.write(ctx, Low, func(db *gorm.DB) error {
		var busy, total, checkpointed int
		err := db.Raw("PRAGMA wal_checkpoint(PASSIVE)").Row().Scan(&busy, &total, &checkpointed)
		s.maintenanceMu.Lock()
		s.maintenance.CheckpointBusy = busy != 0
		s.maintenance.CheckpointPendingPages = max(total-checkpointed, 0)
		s.maintenance.CheckpointAtMS = time.Now().UnixMilli()
		s.maintenanceMu.Unlock()
		return err
	}, true)
}

type Maintenance struct {
	LastBackupAtMS         int64 `json:"last_backup_at_ms"`
	BackupFailed           bool  `json:"backup_failed"`
	CheckpointAtMS         int64 `json:"checkpoint_at_ms"`
	CheckpointBusy         bool  `json:"checkpoint_busy"`
	CheckpointPendingPages int   `json:"checkpoint_pending_pages"`
	WALBytes               int64 `json:"wal_bytes"`
	WALWarning             bool  `json:"wal_warning"`
}

func (s *Store) Maintenance() Maintenance {
	s.maintenanceMu.RLock()
	status := s.maintenance
	s.maintenanceMu.RUnlock()
	if info, err := os.Stat(s.path + "-wal"); err == nil {
		status.WALBytes = info.Size()
	}
	status.WALWarning = status.WALBytes > 64*1024*1024 && status.CheckpointPendingPages > 0
	return status
}

// Maintain does not delete snapshots: archival verification precedes retention.
func (s *Store) Maintain(ctx context.Context, backupDir string, report func(error)) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	lastDay := ""
	for {
		day := time.Now().UTC().Format("2006-01-02")
		if day != lastDay {
			backupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := s.dailyBackup(backupCtx, backupDir, time.Now().UTC())
			cancel()
			s.maintenanceMu.Lock()
			s.maintenance.BackupFailed = err != nil
			if err == nil {
				lastDay = day
				s.maintenance.LastBackupAtMS = time.Now().UnixMilli()
			}
			s.maintenanceMu.Unlock()
			if err != nil && ctx.Err() == nil {
				report(err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Checkpoint(ctx); err != nil && ctx.Err() == nil {
				report(err)
			}
			_ = s.Write(ctx, Low, func(tx *gorm.DB) error {
				before := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
				return tx.Exec("DELETE FROM sessions WHERE token_sha256 IN (SELECT token_sha256 FROM sessions WHERE expires_at_ms < ? OR revoked_at_ms < ? LIMIT 1000)", before, before).Error
			})
		}
	}
}

func (s *Store) dailyBackup(ctx context.Context, dir string, now time.Time) error {
	year, week := now.ISOWeek()
	for _, name := range []string{"portexa-day-" + now.Format("2006-01-02") + ".sqlite", fmt.Sprintf("portexa-week-%04d-W%02d.sqlite", year, week)} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			if err = ValidateDatabase(ctx, path); err != nil {
				return err
			}
		} else if err = s.Backup(ctx, path); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	known := regexp.MustCompile(`^portexa-(day-\d{4}-\d{2}-\d{2}|week-\d{4}-W\d{2})\.sqlite$`)
	for _, kind := range []string{"day", "week"} {
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() && known.MatchString(entry.Name()) && strings.HasPrefix(entry.Name(), "portexa-"+kind+"-") {
				names = append(names, entry.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		keep := 7
		if kind == "week" {
			keep = 4
		}
		for _, name := range names[min(keep, len(names)):] {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
