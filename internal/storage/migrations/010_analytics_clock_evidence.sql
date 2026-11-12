-- Analytics now reads bounded calibration evidence as well as time rules.
-- Fence those sources across paged reads, including provenance invalidation.
CREATE TRIGGER analytics_clock_insert AFTER INSERT ON trade_clock_observations
WHEN NEW.state='accepted'
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_clock_accept AFTER UPDATE ON trade_clock_observations
WHEN NEW.state='accepted' AND OLD.state<>'accepted'
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_clock_delete AFTER DELETE ON trade_clock_observations
WHEN OLD.state='accepted'
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_clock_account AFTER UPDATE ON accounts
WHEN NEW.config_version IS NOT OLD.config_version
  OR NEW.pinned_endpoint_id IS NOT OLD.pinned_endpoint_id
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER analytics_clock_endpoint AFTER UPDATE OF state ON server_endpoints
WHEN NEW.state IS NOT OLD.state
BEGIN UPDATE analytics_revision SET revision=revision+1 WHERE id=1; END;
