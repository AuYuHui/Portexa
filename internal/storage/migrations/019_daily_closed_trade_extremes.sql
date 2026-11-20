CREATE TABLE IF NOT EXISTS daily_closed_trade_extremes (
    report_id TEXT PRIMARY KEY REFERENCES daily_reports(id),
    summary TEXT NOT NULL
) STRICT;
