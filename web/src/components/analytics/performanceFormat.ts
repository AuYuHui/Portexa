import { t } from "../../i18n";
import { fundCategories } from "../../format";
import type { PerformanceDay } from "../../types/analytics";

export function calendarRealizedReason(day: PerformanceDay): string {
  if (day.realized.value !== null || day.realized.reasons.includes("no_members")) return "";
  return performanceReasons(day.realized.reasons);
}

export function calendarDayReason(day: PerformanceDay): string {
  if (day.net_profit.value !== null) return "";
  const reasons = day.net_profit.reasons;
  if (reasons.includes("no_members")) return "";
  if (!day.partial && reasons.includes("boundary_event")) {
    const start = reasons.includes("missing_start");
    const end = reasons.includes("missing_end");
    if (start && end) return t("analytics.dayBothBoundary");
    if (start) return t("analytics.dayStartBoundary");
    if (end) return t("analytics.dayEndBoundary");
  }
  return performanceReasons(reasons);
}

export function performanceReason(reason: string): string {
  const keys: Record<string, string> = {
    history_incomplete: t("analytics.reasonHistory"),
    time_unresolved: t("analytics.reasonTime"),
    unit_unconfirmed: t("analytics.reasonUnit"),
    unit_changed: t("analytics.reasonUnitChanged"),
    unclassified_funds: t("analytics.reasonFunds"),
    reconciliation_mismatch: t("analytics.reasonReconcile"),
    missing_start: t("analytics.reasonStart"),
    missing_end: t("analytics.reasonEnd"),
    missing_endpoints: t("analytics.reasonEndpoints"),
    insufficient_samples: t("analytics.reasonSamples"),
    missing_samples: t("analytics.reasonSamples"),
    sampling_gap: t("analytics.reasonGap"),
    boundary_event: t("analytics.reasonBoundary"),
    structural_mismatch: t("analytics.reasonStructure"),
    nonpositive_capital: t("analytics.reasonCapital"),
    return_chain_broken: t("analytics.reasonChain"),
    transfer_unresolved: t("analytics.reasonTransfer"),
    membership_unresolved: t("analytics.reasonMember"),
    no_members: t("analytics.reasonNoMembers"),
    sampling_skew: t("analytics.reasonSkew"),
    unknown_cadence: t("analytics.reasonCadence"),
  };
  return keys[reason] || t("analytics.unavailable");
}
export function performanceReasons(reasons: string[]): string {
  return [...new Set(reasons.map(performanceReason))].join(" · ");
}
export function performanceEvent(kind: string): string {
  if (kind === "trade") return t("analytics.trade");
  if (kind === "credit") return t("trend.credit");
  if (kind === "membership_in") return t("trend.accountIncluded");
  if (kind === "membership_out") return t("trend.accountRemoved");
  return fundCategories.value[kind] || t("format.unclassified");
}
