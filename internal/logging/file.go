// Package logging writes private JSONL runtime logs with bounded file retention.
package logging

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const DefaultMaxSizeMB = 100
const retention = 7 * 24 * time.Hour
const filename = "portexa.jsonl"

type File struct {
	Logger *slog.Logger

	mu           sync.Mutex
	rolling      *lumberjack.Logger
	diagnostics  *slog.Logger
	nextRotation time.Time
	closed       bool
	writeFailed  bool
	stop         chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	closeErr     error
}

// Open must be called while holding the application's data-directory lock.
// Size is in MiB. Retention is fixed at seven days and checked every minute.
func Open(directory string, maxSizeMB int) (*File, error) {
	if maxSizeMB <= 0 || int64(maxSizeMB) > (1<<63-1)/(1024*1024) {
		return nil, errors.New("日志文件大小上限必须是有效的正整数 MiB")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, filename)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("日志路径必须为普通文件")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f := &File{
		rolling: &lumberjack.Logger{
			Filename: path, MaxSize: maxSizeMB, MaxAge: 7, Compress: true,
		},
		diagnostics: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		stop:        make(chan struct{}), done: make(chan struct{}),
	}
	now := time.Now()
	if err := f.prune(now); err != nil {
		return nil, err
	}
	// Open eagerly so an unwritable log destination fails before serving traffic.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = errors.Join(file.Chmod(0600), file.Close()); err != nil {
		return nil, err
	}
	first, err := firstTime(path)
	if err != nil {
		return nil, err
	}
	if first.IsZero() {
		first = now
	}
	f.nextRotation = first.Add(24 * time.Hour)
	if err = f.rotateDaily(now); err != nil {
		_ = f.rolling.Close()
		return nil, err
	}
	if _, err = f.rolling.Write(nil); err != nil {
		_ = f.rolling.Close()
		return nil, err
	}
	f.Logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey {
				attr.Value = slog.TimeValue(attr.Value.Time().UTC())
			}
			return attr
		},
	}))
	go f.maintain()
	return f, nil
}

func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	if err := f.rotateDaily(time.Now()); err != nil {
		f.reportWriteError(err)
		return 0, err
	}
	n, err := f.rolling.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	f.reportWriteError(err)
	return n, err
}

func (f *File) reportWriteError(err error) {
	if err != nil && !f.writeFailed {
		// Do not reflect a failed record or filesystem path into diagnostics.
		f.diagnostics.Error("log_write_failed")
	}
	f.writeFailed = err != nil
}

func (f *File) rotateDaily(now time.Time) error {
	if now.Before(f.nextRotation) {
		return nil
	}
	info, err := os.Stat(f.rolling.Filename)
	if errors.Is(err, os.ErrNotExist) {
		f.nextRotation = now.Add(24 * time.Hour)
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() > 0 {
		if err = f.rolling.Rotate(); err != nil {
			return err
		}
	}
	f.nextRotation = now.Add(24 * time.Hour)
	return nil
}

func (f *File) maintain() {
	defer close(f.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case now := <-ticker.C:
			if err := f.Maintain(now); err != nil {
				f.diagnostics.Error("log_maintenance_failed")
			}
		}
	}
}

// Maintain also allows a supervisor to request rotation and expiry immediately.
// Open already schedules this once per minute, even without application writes.
func (f *File) Maintain(now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	return errors.Join(f.prune(now), f.rotateDaily(now))
}

func (f *File) Close() error {
	f.closeOnce.Do(func() {
		close(f.stop)
		<-f.done
		f.mu.Lock()
		defer f.mu.Unlock()
		f.closed = true
		f.closeErr = f.rolling.Close()
	})
	return f.closeErr
}
