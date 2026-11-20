-- A partially filled MT5 order can share its ticket with the new position.
CREATE TABLE positions_mt5_orders (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    snapshot_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    command INTEGER NOT NULL CHECK (command BETWEEN 0 AND 7),
    digits INTEGER NOT NULL,
    volume_hundredths INTEGER NOT NULL,
    open_time_wall INTEGER NOT NULL,
    close_time_wall INTEGER NOT NULL,
    open_price TEXT NOT NULL,
    current_price TEXT NOT NULL,
    sl TEXT NOT NULL,
    tp TEXT NOT NULL,
    profit TEXT NOT NULL,
    commission TEXT NOT NULL,
    swap TEXT NOT NULL,
    taxes TEXT NOT NULL,
    exact_lots TEXT,
    trigger_price TEXT,
    PRIMARY KEY (account_id, ticket, command),
    FOREIGN KEY (account_id, snapshot_id)
        REFERENCES account_snapshots(account_id, id)
) STRICT;

INSERT INTO positions_mt5_orders (
    account_id, ticket, snapshot_id, symbol, command, digits, volume_hundredths, open_time_wall, close_time_wall,
    open_price, current_price, sl, tp, profit, commission, swap, taxes, exact_lots
) SELECT
    account_id, ticket, snapshot_id, symbol, command, digits, volume_hundredths, open_time_wall, close_time_wall,
    open_price, current_price, sl, tp, profit, commission, swap, taxes, exact_lots
FROM positions;
DROP TABLE positions;
ALTER TABLE positions_mt5_orders RENAME TO positions;
CREATE INDEX positions_open_time ON positions(account_id, open_time_wall, ticket);

-- Nullable metadata keeps MT4 source digests and API responses stable.
ALTER TABLE history_order_revisions ADD COLUMN mt5_deal_type INTEGER;
ALTER TABLE history_order_revisions ADD COLUMN mt5_entry INTEGER;
ALTER TABLE history_order_revisions ADD COLUMN mt5_order_ticket INTEGER;
ALTER TABLE history_order_revisions ADD COLUMN mt5_position_id INTEGER;
ALTER TABLE history_order_revisions ADD COLUMN mt5_wall_time_ms INTEGER;
ALTER TABLE history_order_revisions ADD COLUMN exact_lots TEXT;
ALTER TABLE history_order_revisions ADD COLUMN fee TEXT;
CREATE INDEX history_mt5_deal_page ON history_order_revisions
    (account_id, unit_revision, native_currency, mt5_wall_time_ms DESC, ticket DESC)
    WHERE mt5_deal_type IS NOT NULL;
