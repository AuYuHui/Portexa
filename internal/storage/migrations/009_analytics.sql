-- Extend the append-only classification vocabulary without changing old facts.
CREATE TABLE cash_flow_classifications_v9 (
    account_id TEXT NOT NULL,
    ticket INTEGER NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    based_trade_revision INTEGER NOT NULL,
    category TEXT NOT NULL CHECK (category IN
        ('deposit','withdrawal','rebate','fee','profit_share_income','profit_share_expense',
         'transfer_in','transfer_out','adjustment','capital_adjustment','unclassified')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 500),
    created_at_ms INTEGER NOT NULL,
    PRIMARY KEY (account_id,ticket,revision),
    FOREIGN KEY (account_id,ticket,based_trade_revision)
        REFERENCES history_order_revisions(account_id,ticket,revision)
) STRICT;
INSERT INTO cash_flow_classifications_v9 SELECT * FROM cash_flow_classifications;
DROP TABLE cash_flow_classifications;
ALTER TABLE cash_flow_classifications_v9 RENAME TO cash_flow_classifications;
CREATE TRIGGER cash_flow_classification_immutable BEFORE UPDATE ON cash_flow_classifications
BEGIN SELECT RAISE(ABORT,'append a classification revision'); END;

-- Fence reads across bounded pages without holding a long SQLite transaction.
CREATE TABLE analytics_revision (
    id INTEGER PRIMARY KEY CHECK (id=1),
    revision INTEGER NOT NULL CHECK (revision >= 0)
) STRICT;
INSERT INTO analytics_revision VALUES (1,0);
CREATE TRIGGER analytics_history_insert AFTER INSERT ON history_order_heads
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_history_update AFTER UPDATE ON history_order_heads
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_classification AFTER INSERT ON cash_flow_classifications
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_coverage AFTER UPDATE OF checked_at_ms ON history_coverage
WHEN NEW.checked_at_ms IS NOT OLD.checked_at_ms
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_calendar AFTER INSERT ON server_time_rule_sets
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_account_insert AFTER INSERT ON accounts
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_account_update AFTER UPDATE ON accounts
WHEN NEW.unit_revision IS NOT OLD.unit_revision OR NEW.label IS NOT OLD.label
  OR NEW.included_at_ms IS NOT OLD.included_at_ms OR NEW.removed_at_ms IS NOT OLD.removed_at_ms
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;

CREATE TABLE analytics_transfer_matches (
    id TEXT PRIMARY KEY,
    out_account_id TEXT NOT NULL,
    out_ticket INTEGER NOT NULL,
    out_revision INTEGER NOT NULL,
    in_account_id TEXT NOT NULL,
    in_ticket INTEGER NOT NULL,
    in_revision INTEGER NOT NULL,
    out_classification TEXT NOT NULL,
    in_classification TEXT NOT NULL,
    out_ruleset TEXT NOT NULL,
    in_ruleset TEXT NOT NULL,
    out_ms INTEGER NOT NULL,
    in_ms INTEGER NOT NULL CHECK (in_ms >= out_ms),
    amount_usd TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 500),
    created_at_ms INTEGER NOT NULL,
    UNIQUE (out_account_id,out_ticket,out_revision,out_classification,out_ruleset),
    UNIQUE (in_account_id,in_ticket,in_revision,in_classification,in_ruleset),
    FOREIGN KEY (out_account_id,out_ticket,out_revision)
        REFERENCES history_order_revisions(account_id,ticket,revision),
    FOREIGN KEY (in_account_id,in_ticket,in_revision)
        REFERENCES history_order_revisions(account_id,ticket,revision),
    CHECK (out_account_id != in_account_id)
) STRICT;
CREATE TRIGGER analytics_transfer_immutable BEFORE UPDATE ON analytics_transfer_matches
BEGIN SELECT RAISE(ABORT,'append a transfer match'); END;
CREATE TRIGGER analytics_transfer_revision AFTER INSERT ON analytics_transfer_matches
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
