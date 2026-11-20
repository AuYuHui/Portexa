export interface Snapshot {
  id: string;
  account_id: string;
  sync_run_id: string;
  observed_from_ms: number;
  observed_to_ms: number;
  native_currency: string;
  usd_scale: string | null;
  unit_revision: number;
  balance: string;
  credit: string;
  equity: string;
  floating_net_profit: string;
  margin: string | null;
  margin_available: boolean;
  margin_level?: string | null;
  leverage: number;
}
export interface Run {
  state: string;
  error_code: string | null;
  raw_status: number | null;
  started_at_ms: number | null;
  finished_at_ms: number | null;
}
export interface Account {
  id: string;
  protocol: "mt4" | "mt5";
  server_id: string;
  server_name: string;
  broker_name: string;
  endpoint_id: string;
  endpoint_approved: boolean;
  login: number;
  label: string;
  confirmed_currency: string | null;
  unit_confirmed: boolean;
  configured_unit_confirmed: boolean;
  config_version: number;
  sync_enabled: boolean;
  next_sync_at_ms: number;
  sync_state: string;
  snapshot: Snapshot | null;
  last_run: Run | null;
  stale: boolean;
  market_rest?: boolean;
  effective_collection_interval_seconds?: number;
  daily: DailyView;
  funding?: FundingView;
}
export interface DailyReport {
  date: string;
  time_zone: string;
  observed_from_ms: number;
  observed_to_ms: number;
  native_currency: string;
  net_profit: string;
  balance_adjustment: string;
  balance_change: string;
  credit_change: string;
  closed_trades: number;
  winning_trades?: number | null;
  losing_trades?: number | null;
  breakeven_trades?: number | null;
  gross_profit?: string | null;
  gross_loss?: string | null;
  win_rate?: string | null;
  closed_trade_extremes?: {
    loss: DailyClosedTrade | null;
    profit: DailyClosedTrade | null;
  };
}
interface DailyClosedTrade {
  net_profit: string;
  ticket: string;
  symbol: string;
  close_wall_ms: number;
}
export interface FundingView {
  state: string;
  native_currency: string;
  deposits: string | null;
  withdrawals: string | null;
  unclassified_count: number;
  confirmed_count: number;
  automatic_count?: number;
  manual_count?: number;
  coverage_complete: boolean;
  covered_through_wall: number;
  pending_windows: number;
  blocked_windows: number;
  last_sync_at_ms: number | null;
  stale: boolean;
  last_run: Run | null;
}
export interface FundClassification {
  revision: number;
  based_trade_revision: number;
  category: string;
  reason: string;
  created_at_ms: number;
  source?: "automatic" | "manual";
  rule_version?: string;
}
export interface FundEntry {
  ticket: number;
  revision: number;
  command: number;
  native_currency: string;
  event_time_wall: number;
  profit: string;
  commission: string;
  swap: string;
  taxes: string;
  net_profit: string;
  comment?: string;
  classification: FundClassification | null;
  needs_review: boolean;
}
export interface FundsPage {
  items: FundEntry[];
  next_cursor: string;
}
export interface DailyView {
  date: string;
  time_zone: string;
  state: string;
  error_code: string | null;
  raw_status: number | null;
  stale: boolean;
  report: DailyReport | null;
}
export interface Totals {
  currency: string;
  expected_accounts: number;
  included_accounts: number;
  stale_accounts: number;
  complete: boolean;
  balance: string | null;
  credit: string | null;
  equity: string | null;
  floating_net_profit: string | null;
  observed_from_ms: number | null;
  observed_to_ms: number | null;
}
export interface CollectionSettings {
  snapshot_interval_seconds: number;
  updated_at_ms: number;
  market_rest_template?: string;
  market_rest_learning_enabled?: boolean;
  market_rest_policy_revision?: number;
  market_rest_excluded_account_ids?: string[];
}

export interface Overview {
  collection_settings?: CollectionSettings;
  accounts: Account[];
  usd_total: Totals;
  currency_totals: Totals[];
  account_count: number;
  missing_count: number;
  stale_count: number;
  excluded_usd_count: number;
  key_ready: boolean;
  as_of_ms: number;
  maintenance: {
    last_backup_at_ms: number;
    backup_failed: boolean;
    wal_warning: boolean;
  };
}
export interface Endpoint {
  id: string;
  revision: number;
  ip_address: string;
  port: number;
  state: string;
  evidence_ref: string | null;
  reviewed_at_ms: number | null;
  risk_accepted_at_ms: number | null;
}
export interface Server {
  id: string;
  protocol: "mt4" | "mt5";
  broker_name: string;
  server_name: string;
  endpoints: Endpoint[];
  time_zone: string;
  time_zone_verified: boolean;
  time_ruleset_id: string;
  time_mode?: "automatic" | "calibrated" | "manual" | "unavailable";
  automatic_time_zone?: {
    available: boolean;
    time_zone: string;
    description: string;
    source_url: string;
    source?: "official" | "events" | "learning";
    status?: "learning" | "calibrated" | "conflict" | "official";
    evidence_count?: number;
    event_window_count?: number;
    required_events?: number;
    observed_at_ms?: number | null;
    expires_at_ms?: number | null;
  };
}
export interface Deal {
  ticket: number;
  mt5_deal_type: number;
  mt5_entry: number;
  mt5_order_ticket: number;
  mt5_wall_time_ms: number;
  event_time_wall: number;
  symbol: string;
  comment: string;
  exact_lots: string;
  digits: number;
  close_price: string;
  commission: string;
  swap: string;
  fee: string;
  net_profit: string;
  native_currency: string;
}
export interface DealsPage {
  items: Deal[];
  next_cursor: string;
}
export interface Position {
  ticket: number;
  symbol: string;
  command: number;
  digits: number;
  lots: string;
  open_time_wall: number;
  open_price: string;
  current_price: string;
  trigger_price?: string;
  profit: string;
  commission: string;
  swap: string;
  taxes: string;
  net_profit: string;
  sl: string;
  tp: string;
}
export interface Detail {
  account: Account;
  positions: Position[];
  server_time_status: string;
}

export interface DirectoryServerName {
  broker_name: string;
  server_name: string;
}

export interface DirectoryCandidate extends DirectoryServerName {
  id: string;
  demo: boolean;
  endpoints: { ip_address: string; port: number }[];
  source_name: string;
  source_url: string;
  fetched_at_ms: number;
  expires_at_ms: number;
}

export interface RestLearningModel { id: string; close_minute: number; open_minute: number; expires_at_ms: number; }
export interface RestLearningGroup { id: string; server_id: string; server_name: string; symbol: string; state: string; reason: string; valid_weekends: number; candidate?: RestLearningModel; active?: RestLearningModel; }

export interface StrategyMetrics {
  total_trades: number;
  win_count: number;
  loss_count: number;
  win_rate: number;
  gross_profit: string;
  gross_loss: string;
  net_profit: string;
  profit_factor: number;
  avg_win: string;
  avg_loss: string;
  total_commission: string;
  total_swap: string;
}

export interface ActiveGrid {
  has_active: boolean;
  symbol: string;
  direction: string;
  dominant_side: string;
  buy_count: number;
  sell_count: number;
  buy_lots: string;
  sell_lots: string;
  layers: number;
  total_lots: string;
  avg_price: string;
  current_price: string;
  dist_to_cost: string;
  total_profit: string;
  max_historic_layer: number;
}

export interface LayerDist {
  layer: number;
  count: number;
  percent: number;
}

export interface GridHistory {
  total_cycles: number;
  cycle_win_rate: number;
  total_cycle_profit: string;
  avg_cycle_profit: string;
  layer_distribution: LayerDist[];
}

export interface StrategyView {
  account_id: string;
  currency: string;
  archetype: string;
  confidence: number;
  tags: string[];
  basket_exit_ratio: number;
  sltp_ratio: number;
  metrics: StrategyMetrics;
  active_grid: ActiveGrid;
  grid_history: GridHistory;
}

