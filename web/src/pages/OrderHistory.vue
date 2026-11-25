<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
import { localizeMessage } from "../i18n";
import { beijingInput } from "../composables/useEquitySeries";
import { useOrderAccount } from "../composables/useOrderAccount";
import OrderAccountSelect from "../components/OrderAccountSelect.vue";
import OrderHistoryTable from "../components/OrderHistoryTable.vue";
import type { ClosedOrderPage } from "../types/orderHistory";
import type { LocationQueryRaw } from "vue-router";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const { accountId, account, accounts, selectAccount, accountsError, accountsPending, refetchAccounts } = useOrderAccount();

function param(key: string): string {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}
const filters = computed(() => {
  const from = param("from") || param("date");
  const to = param("to") || param("date");
  const dates = from || to || param("range") === "all" ? { from, to } : dateRange("today");
  return { ...dates, side: param("side"), result: param("result"), symbol: param("symbol"), ticket: param("ticket") };
});
const draft = ref({ ...filters.value });
watch(filters, (value) => { draft.value = { ...value }; });
const page = computed(() => {
  const n = Number(param("page"));
  return Number.isSafeInteger(n) && n > 0 ? n : 1;
});
const queryString = computed(() => {
  const search = new URLSearchParams({ page: String(page.value) });
  for (const [key, value] of Object.entries(filters.value)) {
    if (value) search.set(key, value);
  }
  return search.toString();
});
const { data, error, isPending, refetch } = useQuery({
  queryKey: computed(() => ["order-history", accountId.value, queryString.value] as const),
  enabled: computed(() => !!account.value),
  queryFn: ({ queryKey: [, id, query], signal }) =>
    api<ClosedOrderPage>(`/accounts/${encodeURIComponent(id)}/order-history?${query}`, "GET", undefined, signal),
});
const pages = computed(() => data.value ? Math.max(1, Math.ceil(data.value.total / data.value.page_size)) : 1);
const jump = ref(page.value);
const resultsPanel = ref<HTMLElement>();
let focusPage: number | undefined;
watch(page, (value) => { jump.value = value; });
function navigatePage(raw: number) {
  if (!Number.isSafeInteger(raw)) return;
  const next = Math.min(pages.value, Math.max(1, raw));
  jump.value = next;
  if (next === page.value) return;
  focusPage = next;
  void router.push({ query: { ...route.query, page: next > 1 ? String(next) : undefined } });
}
watch(data, (value) => {
  if (!value) return;
  if (value.page !== page.value) {
    void router.replace({ query: { ...route.query, page: value.page > 1 ? String(value.page) : undefined } });
  }
  if (focusPage === value.page) {
    resultsPanel.value?.focus({ preventScroll: true });
    resultsPanel.value?.scrollIntoView({ block: "start" });
    focusPage = undefined;
  }
}, { immediate: true, flush: "post" });

function applyFilters() {
  focusPage = undefined;
  const query: LocationQueryRaw = { ...route.query, date: undefined, page: undefined };
  query.range = !draft.value.from && !draft.value.to ? "all" : undefined;
  for (const [key, value] of Object.entries(draft.value)) {
    query[key] = value.trim() || undefined;
  }
  void router.push({ query });
}
function dateRange(preset: "all" | "today" | "week" | "month") {
  const now = Date.now();
  const today = beijingInput(now).slice(0, 10);
  if (preset === "all") return { from: "", to: "" };
  if (preset === "month") return { from: today.slice(0, 8) + "01", to: today };
  if (preset === "week") return { from: beijingInput(now - 6 * 86_400_000).slice(0, 10), to: today };
  return { from: today, to: today };
}
const presets = computed(() => [
  { id: "all", label: t("orderHistory.allDates") },
  { id: "today", label: t("orderHistory.today") },
  { id: "week", label: t("orderHistory.week") },
  { id: "month", label: t("orderHistory.month") },
] as const);
function quickRange(preset: Parameters<typeof dateRange>[0]) {
  Object.assign(draft.value, dateRange(preset));
  applyFilters();
}
function isRange(preset: Parameters<typeof dateRange>[0]) {
  const range = dateRange(preset);
  return range.from === draft.value.from && range.to === draft.value.to;
}
function resetFilters() {
  draft.value = { ...dateRange("today"), side: "", result: "", symbol: "", ticket: "" };
  applyFilters();
}
</script>

<template>
  <div class="page-content history-page" data-order-history>
    <div class="page-title">
      <h1>{{ t("orders.history") }}</h1>
      <OrderAccountSelect :accounts="accounts" :account-id="accountId" @select="selectAccount" />
    </div>
    <p v-if="accountsError" role="alert" class="notice notice-error">
      {{ localizeMessage(accountsError.message) }}
      <button class="text-button" @click="refetchAccounts()">{{ t("overview.retry") }}</button>
    </p>
    <p v-else-if="accountsPending" role="status" class="empty-inline">{{ t("account.loadingAccount") }}</p>
    <p v-else-if="!account" class="empty-inline">{{ t("orderHistory.selectAccount") }}</p>
    <template v-else>
      <form class="panel history-filters" @submit.prevent="applyFilters">
        <div class="history-presets" :aria-label="t('orderHistory.dateRange')" role="group">
          <span>{{ t("trend.beijing") }}</span>
          <button v-for="preset in presets" :key="preset.id" type="button" :aria-pressed="isRange(preset.id)" @click="quickRange(preset.id)">{{ preset.label }}</button>
        </div>
        <div class="history-controls">
          <label>{{ t("orderHistory.from") }}
            <input v-model="draft.from" data-history-filter="from" type="date" :max="draft.to || undefined" />
          </label>
          <label>{{ t("orderHistory.to") }}
            <input v-model="draft.to" data-history-filter="to" type="date" :min="draft.from || undefined" />
          </label>
          <label>{{ t("orderHistory.side") }}
            <select v-model="draft.side" data-history-filter="side">
              <option value="">{{ t("orderHistory.allSides") }}</option>
              <option value="buy">{{ t("mt5.buy") }}</option>
              <option value="sell">{{ t("mt5.sell") }}</option>
            </select>
          </label>
          <label>{{ t("orderHistory.result") }}
            <select v-model="draft.result" data-history-filter="result">
              <option value="">{{ t("orderHistory.allResults") }}</option>
              <option value="profit">{{ t("analytics.profit") }}</option>
              <option value="loss">{{ t("analytics.loss") }}</option>
              <option value="flat">{{ t("analytics.flat") }}</option>
            </select>
          </label>
          <label>{{ t("orderHistory.symbol") }}
            <input v-model.trim="draft.symbol" data-history-filter="symbol" type="search" />
          </label>
          <label>{{ account.protocol === "mt5" ? t("orderHistory.orderOrDeal") : t("orderHistory.orderTicket") }}
            <input v-model.trim="draft.ticket" data-history-filter="ticket" inputmode="numeric" pattern="[0-9]+" type="text" />
          </label>
        </div>
        <div class="history-actions">
          <button type="button" class="text-button" @click="resetFilters">{{ t("orderHistory.reset") }}</button>
          <button type="submit" class="btn btn-primary" data-history-apply>{{ t("orderHistory.apply") }}</button>
        </div>
      </form>

      <p v-if="error" role="alert" class="notice notice-error">
        {{ localizeMessage(error.message) }}
        <button class="text-button" @click="refetch()">{{ t("overview.retry") }}</button>
      </p>
      <p v-else-if="isPending" role="status" class="empty-inline">{{ t("analytics.loadingTrades") }}</p>
      <section v-else-if="data" ref="resultsPanel" tabindex="-1" class="panel history-results" :aria-label="t('orders.history')">
        <OrderHistoryTable v-if="data.items.length" :key="accountId + queryString" :items="data.items" :currency="data.currency" :protocol="account.protocol" />
        <p v-else class="empty-inline">{{ data.complete ? t("orderHistory.empty") : t("orderHistory.partialEmpty") }}</p>
        <nav v-if="data.total" class="history-pagination" :aria-label="t('orderHistory.pagination')">
          <span class="muted small">{{ t("orderHistory.pageSize", { size: data.page_size }) }}</span>
          <div class="history-page-controls">
            <button type="button" class="btn btn-small" data-history-prev :disabled="page <= 1" @click="navigatePage(page - 1)">{{ t("mt5.previousPage") }}</button>
            <span class="num">{{ t("orderHistory.pageOf", { page: data.page, pages }) }}</span>
            <button type="button" class="btn btn-small" data-history-next :disabled="page >= pages" @click="navigatePage(page + 1)">{{ t("mt5.nextPage") }}</button>
          </div>
          <form v-if="pages > 1" class="history-jump" @submit.prevent="navigatePage(jump)">
            <label>{{ t("orderHistory.goTo") }}
              <input v-model.number="jump" data-history-page type="number" min="1" :max="pages" step="1" required />
            </label>
            <button class="btn btn-small" data-history-jump>{{ t("orderHistory.go") }}</button>
          </form>
        </nav>
      </section>
    </template>
  </div>
</template>

<style scoped src="../styles/order-history.css"></style>
