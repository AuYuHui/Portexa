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
export function marginLevel(snapshot: Snapshot | null | undefined): string {
  if (!snapshot?.margin_available || snapshot.margin == null) return "—";
  if (/^0(?:\.0+)?$/.test(snapshot.margin)) return "无占用";
  return percent(snapshot.margin_level);
}
export function fundingLabel(state: string): string {
  return (
    (
      {
        idle: "等待历史采集",
        running: "历史同步中",
        partial: "历史回填中",
        ready: "历史已回填",
        failed: "历史采集失败",
        blocked: "历史存在缺口",
        paused: "历史采集暂停",
        time_zone_unconfirmed: "时间校准中",
      } as Record<string, string>
    )[state] || state
  );
}
export const fundCategories: Record<string, string> = {
  deposit: "外部入金",
  withdrawal: "外部出金",
  rebate: "返佣",
  fee: "费用",
  profit_share_income: "分成收入",
  profit_share_expense: "分成支出",
  transfer_in: "内部转入",
  transfer_out: "内部转出",
  adjustment: "其他调整",
  unclassified: "待分类",
};
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
  return new Intl.DateTimeFormat("zh-CN", {
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
        "（服务器墙钟）"
    : "—";
}
export function positive(value: string | null | undefined) {
  return value !== undefined && value !== null && !value.startsWith("-");
}
export function runLabel(state: string, stale = false) {
  if (stale && state === "succeeded") return "数据过期";
  const label =
    (
      {
        paused: "已暂停",
        idle: "等待首次采集",
        queued: "等待采集",
        running: "同步中",
        succeeded: "同步正常",
        failed: "采集失败",
        canceled: "已取消",
        interrupted: "等待恢复",
      } as Record<string, string>
    )[state] || state;
  return stale ? `${label} · 过期` : label;
}
export function failureLabel(code: string | null | undefined) {
  return (
    (
      {
        timeout: "连接超时，稍后重试",
        connection: "连接失败，稍后重试",
        server_rejected: "服务器拒绝，请核对配置后恢复",
        unsupported_build: "暂不支持该服务器版本",
        unsupported_protocol: "暂不支持该协议",
        resource_limit: "超过采集资源上限",
        snapshot_invalid: "响应不完整，保留上次数据",
        history_invalid: "历史响应不完整，保留上次数据",
        history_window_too_large: "历史区间较大，正在拆分采集",
        configuration_changed: "配置变化，旧任务已取消",
        invalid_config: "配置不支持，请核对",
        read_only_required: "需要投资者密码",
        invalid_response: "响应无法完整解析",
        credential_unavailable: "凭据密钥不可用或不匹配，采集已暂停",
        endpoint_untrusted: "固定接入点未通过核验，采集已暂停",
      } as Record<string, string>
    )[code || ""] || (code ? "采集未完成" : "")
  );
}
export function dailyLabel(state: string, stale = false): string {
  if (state === "time_zone_unconfirmed") return "时间校准中";
  return runLabel(state, stale);
}
