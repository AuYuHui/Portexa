import { t, locale } from "../../i18n";
import type { TrendEvent, TrendPoint } from "../../types/trends";

export function trendDate(stamp: number, compact = false): string {
  if (!stamp) return "—";
  return new Intl.DateTimeFormat(locale.value === "en" ? "en-GB" : "zh-CN", {
    timeZone: "Asia/Shanghai", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", hour12: false,
    ...(compact ? {} : { year: "numeric", second: "2-digit" }),
  }).format(stamp);
}

export function pointQuality(point: TrendPoint): string {
  const reasons: Record<string, string> = {
    missing_samples: t("trend.missingSamples"),
    sampling_skew: t("trend.samplingSkew"),
    no_members: t("trend.noMembers"),
    unit_unconfirmed: t("trend.unitUnconfirmed"),
    unit_changed: t("trend.unitChanged"),
  };
  return point.reason ? reasons[point.reason] || t("trend.unavailable")
    : !point.cadence_known ? t("trend.cadenceUnknownPoint") : t("trend.completePoint");
}

export function eventName(event: TrendEvent): string {
  const names = {
    deposit: t("trend.deposit"), withdrawal: t("trend.withdrawal"),
    account_included: t("trend.accountIncluded"), account_removed: t("trend.accountRemoved"),
  };
  return names[event.kind];
}

// Conversion is confined to chart coordinates. Tooltips and tables retain the
// exact decimal strings, and no financial calculation uses these numbers.
export function chartAmount(value: string | null): number | null {
  if (value === null) return null;
  const number = Number(value);
  return Number.isFinite(number) && Math.abs(number) <= Number.MAX_SAFE_INTEGER ? number : null;
}
