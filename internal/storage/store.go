// Package storage owns the SQLite schema and the single-writer boundary.
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_phase1.sql
var initialSchema string

//go:embed migrations/002_server_directory.sql
var directorySchema string

//go:embed migrations/003_daily_reports.sql
var dailySchema string

//go:embed migrations/004_trade_clock.sql
var clockSchema string

//go:embed migrations/005_account_statistics.sql
var statisticsSchema string

//go:embed migrations/006_history_comments.sql
var historyCommentsSchema string

//go:embed migrations/007_collection_settings.sql
var collectionSchema string

//go:embed migrations/008_asset_trends.sql
var assetTrendsSchema string

var ErrNotFound = gorm.ErrRecordNotFound
var ErrConflict = errors.New("state conflict")
var ErrDenied = errors.New("operation denied")
var ErrClosed = errors.New("store closed")

const ApplicationID = 0x50545841
const SchemaVersion = 8

type Priority int

const (
	High Priority = iota
	Low
)

type writeRequest struct {
	ctx           context.Context
	fn            func(*gorm.DB) error
	done          chan error
	noTransaction bool
}

type Store struct {
	Read          *gorm.DB
	writer        *gorm.DB
	readSQL       *sql.DB
	writeSQL      *sql.DB
	path          string
	version       string
	high          chan writeRequest
	low           chan writeRequest
	stop          chan struct{}
	closed        atomic.Bool
	wg            sync.WaitGroup
	maintenanceMu sync.RWMutex
	maintenance   Maintenance
}

func NewID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("cryptographic random source unavailable")
	}
	return hex.EncodeToString(id[:])
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(abs, 0600); err != nil {
		return nil, err
	}
	wsql, err := openSQL(abs, false)
	if err != nil {
		return nil, err
	}
	s := &Store{writeSQL: wsql, path: abs, high: make(chan writeRequest, 128), low: make(chan writeRequest, 128), stop: make(chan struct{})}
	success := false
	defer func() {
		if !success {
			wsql.Close()
			if s.readSQL != nil {
				s.readSQL.Close()
			}
		}
	}()
	if err = wsql.QueryRow("SELECT sqlite_version()").Scan(&s.version); err != nil {
		return nil, err
	}
	if !SupportedVersion(s.version) {
		return nil, errors.New("SQLite 3.51.3 or newer is required")
	}
	var mode string
	if err = wsql.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		return nil, errors.New("could not enable SQLite WAL")
	}
	if err = migrate(wsql); err != nil {
		return nil, err
	}
	s.writer, err = wrapGORM(wsql)
	if err != nil {
		return nil, err
	}
	s.readSQL, err = openSQL(abs, true)
	if err != nil {
		return nil, err
	}
	s.Read, err = wrapGORM(s.readSQL)
	if err != nil {
		return nil, err
	}
	s.wg.Add(1)
	go s.writeLoop()
	success = true
	return s, nil
}

func openSQL(path string, readOnly bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(1000)")
	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "synchronous(FULL)")
		q.Add("_pragma", "wal_autocheckpoint(1000)")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if readOnly {
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func wrapGORM(db *sql.DB) (*gorm.DB, error) {
	return gorm.Open(gormsqlite.New(gormsqlite.Config{Conn: db}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
}

func SupportedVersion(version string) bool {
	var major, minor, patch int
	if _, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return false
	}
	return major > 3 || (major == 3 && (minor > 51 || (minor == 51 && patch >= 3)))
}

func migrate(db *sql.DB) error {
	var application, version int
	if err := db.QueryRow("PRAGMA application_id").Scan(&application); err != nil {
		return err
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == SchemaVersion && application == ApplicationID {
		return nil
	}
	if (version == 0 && application != 0) || (version > 0 && application != ApplicationID) || version < 0 || version > SchemaVersion {
		return errors.New("unsupported database schema")
	}
	if version == 0 {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count); err != nil || count != 0 {
			return errors.New("refusing to initialize a nonempty database")
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		if _, err = tx.Exec(initialSchema); err != nil {
			return err
		}
	}
	if version < 2 {
		if _, err = tx.Exec(directorySchema); err != nil {
			return err
		}
	}
	if version < 3 {
		if _, err = tx.Exec(dailySchema); err != nil {
			return err
		}
	}
	if version < 4 {
		if _, err = tx.Exec(clockSchema); err != nil {
			return err
		}
	}
	if version < 5 {
		if _, err = tx.Exec(statisticsSchema); err != nil {
			return err
		}
	}
	if version < 6 {
		if _, err = tx.Exec(historyCommentsSchema); err != nil {
			return err
		}
	}
	if version < 7 {
		if _, err = tx.Exec(collectionSchema); err != nil {
			return err
		}
	}
	if version < 8 {
		if _, err = tx.Exec(assetTrendsSchema); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA application_id=%d", ApplicationID)); err != nil {
		return err
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Version() string { return s.version }

func (s *Store) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.stop)
	s.wg.Wait()
	readErr := s.readSQL.Close()
	_, checkpointErr := s.writeSQL.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(readErr, checkpointErr, s.writeSQL.Close())
}

func (s *Store) Write(ctx context.Context, priority Priority, fn func(*gorm.DB) error) error {
	return s.write(ctx, priority, fn, false)
}

func (s *Store) write(ctx context.Context, priority Priority, fn func(*gorm.DB) error, noTransaction bool) error {
	if s.closed.Load() {
		return ErrClosed
	}
	req := writeRequest{ctx: ctx, fn: fn, done: make(chan error, 1), noTransaction: noTransaction}
	queue := s.high
	if priority == Low {
		queue = s.low
	}
	select {
	case queue <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return ErrClosed
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return ErrClosed
	}
}

func (s *Store) writeLoop() {
	defer s.wg.Done()
	for {
		var req writeRequest
		select {
		case <-s.stop:
			return
		case req = <-s.high:
		default:
			select {
			case <-s.stop:
				return
			case req = <-s.high:
			case req = <-s.low:
			}
		}
		ctx, cancel := context.WithTimeout(req.ctx, time.Second)
		err := ctx.Err()
		if err == nil {
			if req.noTransaction {
				err = req.fn(s.writer.WithContext(ctx))
			} else {
				err = s.writer.WithContext(ctx).Transaction(req.fn)
			}
		}
		cancel()
		req.done <- err
	}
}
