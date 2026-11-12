export interface PerformanceMetric {
  value: string | null;
  reasons: string[];
}
export interface PerformanceMetrics {
  net_profit: PerformanceMetric;
  return_pct: PerformanceMetric;
  realized: PerformanceMetric;
  max_drawdown_pct: PerformanceMetric;
  current_drawdown_pct: PerformanceMetric;
  floating_change: PerformanceMetric;
  other_income: PerformanceMetric;
  costs: PerformanceMetric;
  capital_net: PerformanceMetric;
  deposits: PerformanceMetric;
  withdrawals: PerformanceMetric;
}
export interface PerformanceQuality {
  reasons: string[];
  history_complete: boolean;
  expected_points: number;
  valid_points: number;
  valid_intervals: number;
  raw_samples: number;
  unclassified: number;
  unresolved_times: number;
  balance_residual: string | null;
  credit_residual: string | null;
  reconciliation_tolerance: string | null;
  reconciliation: string;
  observed_from_ms: number;
  observed_to_ms: number;
  first_sample_id: string;
  last_sample_id: string;
}
export interface PerformancePoint {
  time_ms: number;
  segment: number;
  net_profit: string | null;
  return_pct: string | null;
  drawdown_pct: string | null;
  statistical_equity: string | null;
  reason: string;
}
export interface PerformanceDay {
  date: string;
  from_ms: number;
  to_ms: number;
  partial: boolean;
  net_profit: PerformanceMetric;
  realized: PerformanceMetric;
  floating_change: PerformanceMetric;
  other_income: PerformanceMetric;
  costs: PerformanceMetric;
  trades: number | null;
}
export interface PerformanceResult {
  account_id: string;
  label: string;
  currency: string;
  from_ms: number;
  to_ms: number;
  metrics: PerformanceMetrics;
  quality: PerformanceQuality;
  drawdown: { peak_ms: number; trough_ms: number; recovered_ms: number | null };
  points: PerformancePoint[];
  days: PerformanceDay[];
  months: { month: string; net_profit: PerformanceMetric; partial: boolean }[];
  evidence_id: string;
  downsampled: boolean;
}
export interface PerformanceEvent {
  time_ms: number;
  account_id: string;
  ticket: number;
  revision: number;
  kind: string;
  amount: string;
  symbol?: string;
  classification: string;
  source_run_id: string;
}
export interface AnalyticsReport {
  result: PerformanceResult;
  accounts: PerformanceResult[];
  events: PerformanceEvent[];
  unmatched_transfers: PerformanceEvent[];
  requested_from_ms: number;
  requested_to_ms: number;
  generated_at_ms: number;
  source_revision: number;
  calculation_version: string;
  method: string;
  timezone: string;
  sampling_ms: number;
  pending_accounts: number;
  excluded_accounts: number;
  events_truncated: boolean;
  stale: boolean;
}
export interface PerformanceActivity {
  items: PerformanceEvent[];
  total: number;
  offset: number;
  currency: string;
}
