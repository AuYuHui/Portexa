package logging_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"portexa/internal/logging"
)

func archiveName(at time.Time) string {
	return "portexa-" + at.UTC().Format("2006-01-02T15-04-05.000") + ".jsonl"
}

func writeRecords(t *testing.T, path string, at time.Time) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var writer io.Writer = file
	if strings.HasSuffix(path, ".gz") {
		compressed := gzip.NewWriter(file)
		defer compressed.Close()
		writer = compressed
	}
	for _, date := range []time.Time{at, time.Now()} {
		if err := json.NewEncoder(writer).Encode(map[string]any{"time": date.UTC(), "msg": "synthetic"}); err != nil {
			t.Fatal(err)
		}
	}
}

func awaitCompression(t *testing.T, directory string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		files, err := filepath.Glob(filepath.Join(directory, "portexa-*.jsonl*"))
		if err != nil {
			t.Fatal(err)
		}
		complete := len(files) > 0
		for _, path := range files {
			complete = complete && strings.HasSuffix(path, ".gz")
		}
		if complete {
			return files
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("archives were not compressed")
	return nil
}

func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(path, ".gz") {
		compressed, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		defer compressed.Close()
		reader = compressed
	}
	var records []map[string]any
	size := 0
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		size += len(scanner.Bytes()) + 1
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		if _, err := time.Parse(time.RFC3339Nano, record["time"].(string)); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if size > 1024*1024 {
		t.Fatal("uncompressed log exceeded configured size", size)
	}
	return records
}

func TestSizeRotationCompressionAndConcurrentJSONL(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "logs")
	file, err := logging.Open(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	const workers, perWorker = 10, 17
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			for i := range perWorker {
				file.Logger.Info("synthetic_event", "id", fmt.Sprintf("%d-%d", worker, i), "text", strings.Repeat("a", 8192)+"\n第二行")
			}
		})
	}
	wg.Wait()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	files := append(awaitCompression(t, directory), filepath.Join(directory, "portexa.jsonl"))
	seen := map[any]bool{}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 || info.Size() > 1024*1024 {
			t.Fatal("invalid log mode or size", path, info, err)
		}
		for _, record := range readRecords(t, path) {
			if seen[record["id"]] || record["msg"] != "synthetic_event" || record["level"] != "INFO" {
				t.Fatal("duplicate or invalid event")
			}
			seen[record["id"]] = true
		}
	}
	if len(seen) != workers*perWorker {
		t.Fatal("rotation lost records", len(seen))
	}
	if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("log directory is not private", err)
	}
	if _, err := file.Write([]byte("after close")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed logger accepted a write", err)
	}
}

func TestRetentionUsesOldestRecordIncludingActiveAndGzip(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	expired := []string{
		"portexa.jsonl",
		archiveName(now.Add(-time.Hour)),
		archiveName(now.Add(-2*time.Hour)) + ".gz",
	}
	for _, name := range expired {
		writeRecords(t, filepath.Join(directory, name), old)
	}
	recent := archiveName(now.Add(-3*time.Hour)) + ".gz"
	writeRecords(t, filepath.Join(directory, recent), now.Add(-6*24*time.Hour))
	unmanaged := "portexa-not-an-archive.jsonl"
	writeRecords(t, filepath.Join(directory, unmanaged), old)
	file, err := logging.Open(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	file.Logger.Info("new_event")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range expired[1:] {
		if _, err := os.Stat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("expired archive retained", name, err)
		}
	}
	for _, name := range []string{recent, unmanaged} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal("recent or unrelated file removed", name, err)
		}
	}
	if records := readRecords(t, filepath.Join(directory, "portexa.jsonl")); len(records) != 1 || records[0]["msg"] != "new_event" {
		t.Fatal("expired active log was appended to")
	}
}

func TestLowVolumeFileRotatesByAgeAfterRestart(t *testing.T) {
	directory := t.TempDir()
	writeRecords(t, filepath.Join(directory, "portexa.jsonl"), time.Now().Add(-25*time.Hour))
	file, err := logging.Open(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	file.Logger.Info("after_restart")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archives := awaitCompression(t, directory)
	if len(archives) != 1 || len(readRecords(t, archives[0])) != 2 {
		t.Fatal("daily rotation did not preserve old file")
	}
	if records := readRecords(t, filepath.Join(directory, "portexa.jsonl")); len(records) != 1 || records[0]["msg"] != "after_restart" {
		t.Fatal("new records appended to previous day")
	}
}

func TestRejectInvalidSizeAndNonRegularLogPath(t *testing.T) {
	for _, size := range []int{0, -1, int(^uint(0) >> 1)} {
		if file, err := logging.Open(t.TempDir(), size); err == nil {
			_ = file.Close()
			t.Fatal("invalid size accepted", size)
		}
	}
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "portexa.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if file, err := logging.Open(directory, 1); err == nil {
		_ = file.Close()
		t.Fatal("directory accepted as active log")
	}
}

func TestIdleMaintenanceRotatesAndExpiresWithoutWrites(t *testing.T) {
	directory := t.TempDir()
	file, err := logging.Open(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	file.Logger.Info("before_idle")
	now := time.Now()
	if err := file.Maintain(now.Add(25 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	archives := awaitCompression(t, directory)
	if len(archives) != 1 || len(readRecords(t, archives[0])) != 1 {
		t.Fatal("idle log did not rotate")
	}
	if err := file.Maintain(now.Add(8 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, path := range archives {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("idle archive did not expire", err)
		}
	}
	// Also cover an active file expiring before the daily maintenance tick.
	file.Logger.Info("after_idle")
	if err := file.Maintain(now.Add(8 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	file.Logger.Info("after_expiry")
	if records := readRecords(t, filepath.Join(directory, "portexa.jsonl")); len(records) != 1 || records[0]["msg"] != "after_expiry" {
		t.Fatal("active log did not reopen after expiry")
	}
}
