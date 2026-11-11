// history_publish benchmarks atomic publication, not staging or MT4 throughput.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"portexa/tests/historymodel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	document := flag.String("schema", "docs/product-data-model.md", "SQL 原型文档")
	iterations := flag.Int("iterations", 20, "每场景发布次数，P95 建议不少于20")
	flag.Parse()
	if *iterations < 1 || *iterations > 100 {
		return fmt.Errorf("iterations must be 1..100")
	}
	dir, err := os.MkdirTemp("", "portexa-history-performance-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	fmt.Printf("环境：%s %s/%s，时间 %s，FULL/WAL，外键与触发器开启，暂存每批500，发布1秒上下文。\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, time.Now().UTC().Format(time.RFC3339))
	fmt.Println("| 场景 | 修订数 | 次数 | 发布 P95(ms) | 最大(ms) | 100ms / 1s 目标 |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | --- |")
	for _, size := range []int{1000, 5000, 20000} {
		for _, scenario := range []string{"首次发布", "更新版本"} {
			db, err := historymodel.Open(filepath.Join(dir, fmt.Sprintf("%d-%s.sqlite", size, scenario)), *document)
			if err != nil {
				return err
			}
			if err = historymodel.Seed(db); err != nil {
				db.Close()
				return err
			}
			var version string
			db.QueryRow("SELECT sqlite_version()").Scan(&version)
			if size == 1000 && scenario == "首次发布" {
				fmt.Println("<!-- SQLite", version, "-->")
			}
			var samples []float64
			for iteration := 0; iteration < *iterations; iteration++ {
				runID := fmt.Sprintf("run-%d", iteration)
				if err = stage(db, size, iteration, scenario == "首次发布", runID); err != nil {
					db.Close()
					return err
				}
				elapsed, err := publish(db, runID)
				if err != nil {
					db.Close()
					return fmt.Errorf("%s %d: publication rolled back: %w", scenario, size, err)
				}
				samples = append(samples, float64(elapsed)/float64(time.Millisecond))
				var published int
				if err = db.QueryRow("SELECT COUNT(*) FROM current_trades WHERE source_run_id = ?", runID).Scan(&published); err != nil || published != size {
					db.Close()
					return fmt.Errorf("publication count mismatch: %d/%d: %w", published, size, err)
				}
			}
			sort.Float64s(samples)
			p95 := samples[int(math.Ceil(float64(len(samples))*.95))-1]
			maximum := samples[len(samples)-1]
			result := "通过"
			if p95 > 100 || maximum > 1000 {
				result = "需拆分独立覆盖区间"
			}
			fmt.Printf("| %s | %d | %d | %.2f | %.2f | %s |\n", scenario, size, len(samples), p95, maximum, result)
			var integrity string
			if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
				db.Close()
				return fmt.Errorf("integrity check failed: %s: %w", integrity, err)
			}
			rows, err := db.Query("PRAGMA foreign_key_check")
			if err != nil {
				db.Close()
				return err
			}
			invalid := rows.Next()
			rowErr := rows.Err()
			rows.Close()
			if invalid || rowErr != nil {
				db.Close()
				return fmt.Errorf("foreign key check failed: %w", rowErr)
			}
			if err = db.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func stage(db *sql.DB, size, iteration int, fresh bool, run string) error {
	_, err := db.Exec(`INSERT INTO history_coverage(id,account_id,start_wall,end_wall,state,created_at_ms) VALUES(?,'a',?,?,'pending',0)`, run, iteration*1000, iteration*1000+999)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO sync_runs(id,account_id,server_id,endpoint_id,credential_version,kind,state,coverage_id,due_at_ms,not_before_ms,attempt) VALUES(?,'a','s','e',1,'history_backfill','running',?,0,0,1)`, run, run)
	if err != nil {
		return err
	}
	for from := 0; from < size; from += 500 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		identity, err := tx.Prepare(`INSERT OR IGNORE INTO trades(id,account_id,ticket) VALUES(?,'a',?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		revision, err := tx.Prepare(`INSERT INTO trade_revisions(trade_id,account_id,revision,source_run_id,source_sha256,native_currency,unit_revision,symbol,command,digits,volume_hundredths,open_time_wall,close_time_wall,open_price,close_price,sl,tp,profit,commission,swap,taxes,recorded_at_ms) VALUES(?,'a',?,?,zeroblob(32),'USD',1,'SYNTH',0,5,100,1700000000,1700001000,'1.125','1.126','0','0','10.01','-0.1','0','0',0)`)
		if err != nil {
			identity.Close()
			tx.Rollback()
			return err
		}
		for row := from; row < min(from+500, size); row++ {
			ticket := row + 1
			version := iteration + 1
			if fresh {
				ticket += iteration * size
				version = 1
			}
			id := fmt.Sprintf("trade-%09d", ticket)
			if _, err = identity.Exec(id, ticket); err != nil {
				break
			}
			if _, err = revision.Exec(id, version, run); err != nil {
				break
			}
		}
		identity.Close()
		revision.Close()
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func publish(db *sql.DB, run string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE sync_runs SET state='succeeded',finished_at_ms=1 WHERE id=?", run); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE trades SET current_revision=(SELECT r.revision FROM trade_revisions r WHERE r.trade_id=trades.id AND r.source_run_id=?) WHERE id IN (SELECT trade_id FROM trade_revisions WHERE source_run_id=?)`, run, run); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE history_coverage SET state='complete',checked_at_ms=1,last_success_run_id=? WHERE id=?", run, run); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}
