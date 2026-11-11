package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

type Fetcher func(context.Context, mt4.Config, mt4.Options) (mt4.Result, error)

type Options struct {
	Interval        time.Duration // Optional process override; zero uses persisted collection settings.
	Timeout         time.Duration
	Workers         int
	Logger          *slog.Logger
	HistoryInterval time.Duration
}

type Engine struct {
	accounts          *accounts.Service
	fetch             Fetcher
	interval, timeout time.Duration
	sem               chan struct{}
	wake              chan struct{}
	mu                sync.Mutex
	active            map[string]context.CancelFunc
	wg                sync.WaitGroup
	fatal             chan error
	logger            *slog.Logger
	historySem        chan struct{}
	historyInterval   time.Duration
	historyActive     map[string]bool
	historyYielded    map[string]bool
	nextTimeRefresh   time.Time
}

func New(service *accounts.Service, fetch Fetcher, options Options) *Engine {
	if fetch == nil {
		fetch = mt4.Fetch
	}
	if options.Timeout <= 0 {
		options.Timeout = 20 * time.Second
	}
	if options.Workers <= 0 {
		options.Workers = 10
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	if options.HistoryInterval <= 0 {
		options.HistoryInterval = 5 * time.Minute
	}
	return &Engine{accounts: service, fetch: fetch, interval: options.Interval, timeout: options.Timeout,
		sem: make(chan struct{}, options.Workers), wake: make(chan struct{}, 1), active: map[string]context.CancelFunc{}, fatal: make(chan error, 1),
		logger: options.Logger.With("component", "syncer"), historySem: make(chan struct{}, 2), historyInterval: options.HistoryInterval,
		historyActive: map[string]bool{}, historyYielded: map[string]bool{}}
}

// Run is called once; cancellation drains every active Fetch before it returns.
func (e *Engine) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer func() { cancel(); e.wg.Wait() }()
	if err := e.accounts.Recover(ctx); err != nil {
		return err
	}
	e.nextTimeRefresh = time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		e.schedule(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-e.wake:
		case err := <-e.fatal:
			return err
		}
	}
}

func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) Cancel(id string) {
	e.mu.Lock()
	if cancel := e.active[id]; cancel != nil {
		cancel()
	}
	e.mu.Unlock()
	e.Wake()
}

func (e *Engine) schedule(ctx context.Context) {
	if !time.Now().Before(e.nextTimeRefresh) {
		if err := e.accounts.RefreshServerTimes(ctx); err != nil {
			if ctx.Err() == nil {
				e.stopForStorageFailure()
			}
			return
		}
		e.nextTimeRefresh = time.Now().Add(30 * time.Second)
	}
	e.scheduleSnapshots(ctx)
	e.scheduleClock(ctx)
	e.scheduleDaily(ctx)
	e.scheduleHistory(ctx)
}

func (e *Engine) scheduleSnapshots(ctx context.Context) {
	ids, err := e.accounts.Due(ctx, time.Now().UnixMilli(), 200)
	if err != nil {
		if ctx.Err() == nil {
			e.stopForStorageFailure()
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.mu.Lock()
		if _, exists := e.active[id]; exists {
			if e.historyActive[id] {
				e.historyYielded[id] = true
				e.active[id]()
			}
			e.mu.Unlock()
			continue
		}
		select {
		case e.sem <- struct{}{}:
		default:
			e.mu.Unlock()
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, e.timeout)
		e.active[id] = cancel
		e.wg.Add(1)
		e.mu.Unlock()
		go e.runOne(jobCtx, id)
	}
}

func (e *Engine) runOne(ctx context.Context, id string) {
	defer func() {
		e.mu.Lock()
		e.active[id]()
		delete(e.active, id)
		e.mu.Unlock()
		<-e.sem
		e.wg.Done()
	}()
	job, err := e.accounts.Claim(ctx, id)
	if err != nil {
		if accounts.IsBrokenGate(err) {
			e.logger.Warn("sync_gate_unavailable", "account_id", id)
			endCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := e.accounts.DisableBrokenGate(endCtx, id, job.Account.ConfigVersion); err != nil && !errors.Is(err, storage.ErrNotFound) {
				e.stopForStorageFailure()
			}
		} else if !errors.Is(err, accounts.ErrPaused) && !errors.Is(err, storage.ErrNotFound) && ctx.Err() == nil {
			e.stopForStorageFailure()
		}
		return
	}
	from := time.Now()
	logger := e.logger.With("account_id", id, "run_id", job.Run.ID)
	logger.Info("sync_started")
	result, err := e.fetch(ctx, job.Config, mt4.Options{})
	job.Config.Password = ""
	finished := time.Now()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		var snapshot storage.Snapshot
		var positions []storage.Position
		snapshot, positions, err = Convert(job, result, from.UnixMilli(), finished.UnixMilli())
		if err == nil {
			if e.interval > 0 {
				err = e.accounts.Publish(ctx, job, snapshot, positions, accounts.NextSnapshotAt(id, finished, e.interval).UnixMilli())
			} else {
				err = e.accounts.PublishScheduled(ctx, job, snapshot, positions)
			}
		}
	}
	if err != nil {
		failure := classify(err)
		level := slog.LevelWarn
		if failure.Canceled {
			level = slog.LevelInfo
		}
		logger.Log(ctx, level, "sync_failed", "code", failure.Code, "operation", failure.Operation,
			"permanent", failure.Permanent, "canceled", failure.Canceled, "duration_ms", time.Since(from).Milliseconds())
		endCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := e.accounts.Fail(endCtx, job, failure, finished.Add(accounts.RetryDelay(job.Account.FailureCount)).UnixMilli()); err != nil {
			e.stopForStorageFailure()
		}
	} else {
		logger.Info("sync_completed", "duration_ms", time.Since(from).Milliseconds())
	}
}

func (e *Engine) stopForStorageFailure() {
	// Do not leave an apparently running account stuck behind its durable lock.
	// The supervisor restarts the service; Recover marks unfinished runs interrupted.
	select {
	case e.fatal <- errors.New("快照调度无法持久化状态，已停止；请检查数据库并重启服务"):
		e.logger.Error("sync_storage_failed")
	default:
	}
}

func classify(err error) accounts.Failure {
	failure := accounts.Failure{Code: "snapshot_invalid", Operation: "snapshot"}
	var protocol *mt4.Error
	if errors.As(err, &protocol) {
		failure.Code, failure.Operation = string(protocol.Code), protocol.Operation
		if protocol.Code == mt4.ServerRejected {
			status := int(protocol.Status)
			failure.Status = &status
		}
		switch protocol.Code {
		case mt4.InvalidConfig, mt4.ReadOnlyRequired, mt4.UnsupportedBuild, mt4.UnsupportedProtocol, mt4.ServerRejected, mt4.ResourceLimit:
			failure.Permanent = true
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, storage.ErrConflict) || errors.Is(err, accounts.ErrEndpoint) {
		failure.Canceled = true
		failure.Code = "configuration_changed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Code = "timeout"
	}
	return failure
}
