import { watch } from "vue";
import { createI18n } from "vue-i18n";
import zhCN from "./locales/zh-CN.json";
import en from "./locales/en.json";

export type Locale = "zh-CN" | "en";
const preferenceKey = "portexa.locale";

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(preferenceKey);
    if (saved === "zh-CN" || saved === "en") return saved;
  } catch {
    // Language switching still works when browser storage is disabled.
  }
  for (const language of navigator.languages) {
    if (/^zh(?:-|$)/i.test(language)) return "zh-CN";
    if (/^en(?:-|$)/i.test(language)) return "en";
  }
  return "zh-CN";
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: "zh-CN",
  messages: { "zh-CN": zhCN, en },
});
export const t = i18n.global.t;
export const locale = i18n.global.locale;

export function setLocale(value: string) {
  if (value !== "zh-CN" && value !== "en") return;
  locale.value = value;
  try {
    localStorage.setItem(preferenceKey, value);
  } catch {
    // Keep the selection in memory if persistence is unavailable.
  }
}

watch(locale, (value) => { document.documentElement.lang = value; }, {
  immediate: true,
  flush: "sync",
});

// Existing APIs return system messages as text. Resolve only messages displayed
// as feedback or system descriptions; account names, comments and audit data
// are never passed through this compatibility layer.
const messageKeys = new Map<string, string>();
for (const messages of [zhCN, en]) {
  for (const [key, value] of Object.entries(messages.serverMessages)) {
    messageKeys.set(value, `serverMessages.${key}`);
  }
  for (const [key, value] of Object.entries(messages.api)) {
    messageKeys.set(value, `api.${key}`);
  }
}

export type LocalizedMessage = string | (() => string);

export function localizeMessage(value: LocalizedMessage | null | undefined): string {
  if (typeof value === "function") return value();
  if (!value) return "";
  if (["Failed to fetch", "Load failed", "NetworkError when attempting to fetch resource."].includes(value)) {
    return t("api.networkUnavailable");
  }
  const key = messageKeys.get(value);
  return key ? t(key) : value;
}
