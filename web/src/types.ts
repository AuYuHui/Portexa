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
export interface Overview {
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
  broker_name: string;
  server_name: string;
  endpoints: Endpoint[];
  time_zone: string;
  time_zone_verified: boolean;
}
export interface Position {
  ticket: number;
  symbol: string;
  command: number;
  lots: string;
  open_time_wall: number;
  open_price: string;
  current_price: string;
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

export interface DirectoryCandidate {
  id: string;
  broker_name: string;
  server_name: string;
  demo: boolean;
  endpoints: { ip_address: string; port: number }[];
  source_name: string;
  source_url: string;
  fetched_at_ms: number;
  expires_at_ms: number;
}
