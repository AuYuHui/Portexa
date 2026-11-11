package logging

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const backupTimeFormat = "2006-01-02T15-04-05.000"

func managed(name string) bool {
	if name == filename {
		return true
	}
	name = strings.TrimSuffix(name, ".gz")
	if !strings.HasPrefix(name, "portexa-") || !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	_, err := time.Parse(backupTimeFormat, strings.TrimSuffix(strings.TrimPrefix(name, "portexa-"), ".jsonl"))
	return err == nil
}

// firstTime reads only the first record, including from compressed archives.
// Whole files expire at their oldest record; recent boundary records may be
// removed early, but rotation timestamps cannot extend old records' retention.
func firstTime(path string) (time.Time, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, nil // The compression worker may have renamed it.
	}
	if err != nil {
		return time.Time{}, err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return time.Time{}, err
		}
		defer compressed.Close()
		reader = compressed
	}
	var record struct {
		Time time.Time `json:"time"`
	}
	err = json.NewDecoder(io.LimitReader(reader, 64*1024)).Decode(&record)
	if errors.Is(err, io.EOF) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if record.Time.IsZero() {
		return time.Time{}, errors.New("日志首条记录缺少时间")
	}
	return record.Time, nil
}

func (f *File) prune(now time.Time) error {
	directory := filepath.Dir(f.rolling.Filename)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !managed(entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if strings.HasSuffix(path, ".gz") {
			if _, err := os.Stat(strings.TrimSuffix(path, ".gz")); err == nil {
				continue // A gzip file is complete only once its source is removed.
			}
		}
		first, err := firstTime(path)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if first.IsZero() || first.After(now.Add(-retention)) {
			continue
		}
		if path == f.rolling.Filename {
			if err = f.rolling.Close(); err != nil {
				result = errors.Join(result, err)
				continue
			}
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
			continue
		}
		if path == f.rolling.Filename {
			f.nextRotation = now.Add(24 * time.Hour)
		}
		// Remove an in-progress compressed copy of an expired raw archive too.
		if !strings.HasSuffix(path, ".gz") {
			if err = os.Remove(path + ".gz"); err != nil && !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}
