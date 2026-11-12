import { computed } from "vue";
import { t, locale } from "./i18n";
import type { Snapshot } from "./types";

// Format decimal strings directly. Formatting never feeds back into money calculations.
function formatDecimal(
  value: string | null | undefined,
  places: number,
  grouped: boolean,
): string {
  if (value === null || value === undefined) return "—";
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!match) return "—";
  const negative = match[1] === "-";
  let whole = BigInt(match[2]!);
  const fraction = (match[3] || "").padEnd(places + 1, "0");
  let minor = BigInt(fraction.slice(0, places) || "0");
  if (Number(fraction[places]) >= 5) minor += 1n;
  const base = 10n ** BigInt(places);
  if (minor >= base) {
    whole += 1n;
    minor -= base;
  }
  const sign = negative && (whole > 0n || minor > 0n) ? "−" : "";
  const integer = whole.toString();
  return (
    sign +
    (grouped ? integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",") : integer) +
    (places ? "." + minor.toString().padStart(places, "0") : "")
  );
}
export function money(value: string | null | undefined, places = 2): string {
  return formatDecimal(value, places, true);
}
export function percent(value: string | null | undefined): string {
  const formatted = money(value);
  return formatted === "—" ? formatted : `${formatted}%`;
}
export function signedPercent(value: string | null | undefined): string {
  const formatted = signedMoney(value);
  return formatted === "—" ? formatted : `${formatted}%`;
}
export function marginLevel(snapshot: Snapshot | null | undefined): string {
  if (!snapshot?.margin_available || snapshot.margin == null) return "—";
  if (/^0(?:\.0+)?$/.test(snapshot.margin)) return t('format.noMarginUsed');
  return percent(snapshot.margin_level);
}
export function fundingLabel(state: string): string {
  return (
    (
      {
        idle: t('accountTable.waitingForHistory'),
        running: t('format.syncingHistory'),
        partial: t('format.backfillingHistory'),
        ready: t('format.historyBackfilled'),
        failed: t('format.historyCollectionFailed'),
        blocked: t('format.historyHasGaps'),
        paused: t('format.historyCollectionPaused'),
        time_zone_unconfirmed: t('settings.calibratingTime'),
      } as Record<string, string>
    )[state] || state
  );
}
export const fundCategories = computed<Record<string, string>>(() => ({
  deposit: t('format.externalDeposit'),
  withdrawal: t('format.externalWithdrawal'),
  rebate: t('format.rebate'),
  fee: t('format.fee'),
  profit_share_income: t('format.profitShareIncome'),
  profit_share_expense: t('format.profitShareExpense'),
  transfer_in: t('format.internalTransferIn'),
  transfer_out: t('format.internalTransferOut'),
  adjustment: t('format.otherAdjustment'),
  capital_adjustment: t('analytics.capitalAdjustment'),
  unclassified: t('format.unclassified'),
}));
export function signedMoney(
  value: string | null | undefined,
  places = 2,
): string {
  if (value === null || value === undefined) return "—";
  const str = formatDecimal(value, places, true);
  if (str === "—") return "—";
  if (!str.startsWith("−") && !str.startsWith("-")) {
    const num = parseFloat(value);
    if (!isNaN(num) && num > 0) {
      return "+" + str;
    }
  }
  return str;
}
export function price(
  value: string | null | undefined,
  digits: number,
): string {
  return formatDecimal(value, digits, false);
}
export function date(value: number | null | undefined, seconds = true): string {
  if (!value) return "—";
  return new Intl.DateTimeFormat(locale.value === "en" ? "en-US" : "zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" } : {}),
    hour12: false,
  }).format(value);
}
export function wallTime(seconds: number): string {
  return seconds
    ? new Date(seconds * 1000).toISOString().slice(0, 19).replace("T", " ") +
        t('format.serverWallClock')
    : "—";
}
export function positive(value: string | null | undefined) {
  return value !== undefined && value !== null && !value.startsWith("-");
}
export function runLabel(state: string, stale = false) {
  if (stale && state === "succeeded") return t('format.staleData');
  const label =
    (
      {
        paused: t('format.paused'),
        idle: t('format.waitingForFirstCollection'),
        queued: t('format.waitingForCollection'),
        running: t('format.syncing'),
        succeeded: t('format.syncHealthy'),
        failed: t('format.collectionFailed'),
        canceled: t('format.canceled'),
        interrupted: t('format.waitingToResume'),
      } as Record<string, string>
    )[state] || state;
  return stale ? t('format.stale', { value1: label }) : label;
}
export function failureLabel(code: string | null | undefined) {
  return (
    (
      {
        timeout: t('format.connectionTimedOutRetryLater'),
        connection: t('format.connectionFailedRetryLater'),
        server_rejected: t('format.serverRejectedTheRequestCheckTheConfigurationBefore'),
        unsupported_build: t('format.serverVersionNotSupportedYet'),
        unsupported_protocol: t('format.protocolNotSupportedYet'),
        resource_limit: t('format.collectionResourceLimitExceeded'),
        snapshot_invalid: t('format.incompleteResponseRetainingThePreviousData'),
        history_invalid: t('format.incompleteHistoryResponseRetainingThePreviousData'),
        history_window_too_large: t('format.largeHistoryWindowSplittingCollection'),
        configuration_changed: t('format.configurationChangedPreviousJobCanceled'),
        invalid_config: t('format.unsupportedConfigurationPleaseCheckIt'),
        read_only_required: t('format.investorPasswordRequired'),
        invalid_response: t('format.responseCouldNotBeFullyParsed'),
        credential_unavailable: t('format.credentialKeyUnavailableOrMismatchedCollectionPaused'),
        endpoint_untrusted: t('format.fixedEndpointHasNotPassedVerificationCollectionPaused'),
      } as Record<string, string>
    )[code || ""] || (code ? t('format.collectionIncomplete') : "")
  );
}
export function dailyLabel(state: string, stale = false): string {
  if (state === "time_zone_unconfirmed") return t('settings.calibratingTime');
  return runLabel(state, stale);
}
