import { t } from "../../i18n";
import { fundCategories } from "../../format";

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
