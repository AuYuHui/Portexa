CREATE TABLE IF NOT EXISTS position_excursions (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    unit_revision INTEGER NOT NULL,
    native_currency TEXT NOT NULL,
    identifier INTEGER NOT NULL CHECK (identifier > 0),
    ticket INTEGER NOT NULL CHECK (ticket > 0),
    command INTEGER NOT NULL CHECK (command IN (0, 1)),
    open_time_wall INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    first_sample_ms INTEGER NOT NULL,
    last_sample_ms INTEGER NOT NULL,
    lowest TEXT NOT NULL,
    lowest_ms INTEGER NOT NULL,
    highest TEXT NOT NULL,
    highest_ms INTEGER NOT NULL,
    PRIMARY KEY (account_id, unit_revision, native_currency, identifier, ticket, command, open_time_wall)
) STRICT;

CREATE TABLE IF NOT EXISTS daily_position_excursions (
    report_id TEXT PRIMARY KEY REFERENCES daily_reports(id),
    summary TEXT NOT NULL
) STRICT;
