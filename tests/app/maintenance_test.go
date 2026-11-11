package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portexa/tests/testutil"
)

func TestDailyBackupRetentionAndFailureKeepsPreviousCopies(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint("corrupt=", corrupt), func(t *testing.T) {
			f := testutil.New(t, false)
			root := t.TempDir()
			baseline := filepath.Join(root, "baseline.sqlite")
			if err := f.Store.Backup(context.Background(), baseline); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(baseline)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "backups")
			if err = os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			for i := 1; i <= 12; i++ {
				path := filepath.Join(dir, "portexa-day-"+now.AddDate(0, 0, -i).Format("2006-01-02")+".sqlite")
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for i := 1; i <= 8; i++ {
				year, week := now.AddDate(0, 0, -7*i).ISOWeek()
				path := filepath.Join(dir, fmt.Sprintf("portexa-week-%04d-W%02d.sqlite", year, week))
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if corrupt {
				if err = os.WriteFile(filepath.Join(dir, "portexa-day-"+now.Format("2006-01-02")+".sqlite"), []byte("synthetic corrupt copy"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); f.Store.Maintain(ctx, dir, func(error) {}) }()
			eventually(t, func() bool { status := f.Store.Maintenance(); return status.BackupFailed || status.LastBackupAtMS > 0 })
			cancel()
			<-done
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			daily, weekly := 0, 0
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "portexa-day-") {
					daily++
				}
				if strings.HasPrefix(entry.Name(), "portexa-week-") {
					weekly++
				}
			}
			if corrupt {
				if !f.Store.Maintenance().BackupFailed || daily != 13 || weekly != 8 {
					t.Fatal("failed backup removed previous copies", daily, weekly)
				}
			} else if daily != 7 || weekly != 4 {
				t.Fatal("wrong retention", daily, weekly)
			}
		})
	}
}
