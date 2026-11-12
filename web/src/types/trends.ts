export type TrendRange = "24h" | "7d" | "30d" | "90d" | "custom";
export type TrendMetric = "equity" | "balance" | "both";
export type TrendUnit = "native" | "USD";

export interface TrendPoint {
  time_ms: number;
  observed_from_ms: number;
  observed_to_ms: number;
  snapshot_id?: string;
  currency: string;
  unit_revision: number;
  balance: string | null;
  equity: string | null;
  credit: string | null;
  floating_net_profit: string | null;
  segment: number;
  reason: string;
  expected_accounts: number;
  included_accounts: number;
  cadence_known: boolean;
}

export interface TrendEvent {
  time_ms: number;
  kind: "deposit" | "withdrawal" | "account_included" | "account_removed";
  account_id: string;
  ticket?: number;
  amount: string | null;
  currency: string;
}

export interface TrendSeries {
  scope: "account" | "portfolio";
  unit: TrendUnit;
  native_currency: string;
  currencies: string[];
  from_ms: number;
  to_ms: number;
  as_of_ms: number;
  points: TrendPoint[];
  events: TrendEvent[];
  total_points: number;
  valid_points: number;
  gap_points: number;
  break_count: number;
  can_convert_usd: boolean;
  unknown_cadence: number;
  downsampled: boolean;
  sampling_method: string;
  alignment_ms: number;
  max_sample_age_ms: number;
  excluded_accounts: number;
  pending_accounts: number;
  unplaced_events: number;
  events_truncated: boolean;
  last_observed_to_ms: number;
  included_from_ms: number;
  stale: boolean;
}
