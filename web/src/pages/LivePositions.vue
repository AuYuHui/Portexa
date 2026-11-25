<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useQuery } from "@tanstack/vue-query";
import { RefreshCw } from "@lucide/vue";
import { api } from "../api";
import { localizeMessage } from "../i18n";
import { signedMoney } from "../format";
import { useOrderAccount } from "../composables/useOrderAccount";
import OrderAccountSelect from "../components/OrderAccountSelect.vue";
import LivePositionRows from "../components/LivePositionRows.vue";
import StatusBadge from "../components/StatusBadge.vue";
import { trendDate } from "../components/charts/trendFormat";
import { profitTone } from "../components/analytics/performanceFormat";
import type { Position } from "../types";

interface LivePositions {
  account_id: string;
  protocol: "mt4" | "mt5";
  currency: string;
  observed_to_ms: number | null;
  sync_enabled: boolean;
  sync_state: string;
  stale: boolean;
  market_rest: boolean;
  total_count: number | null;
  floating_net_profit: string | null;
  filtered_net_profit: string | null;
  symbols: string[];
  items: Position[];
}

const { t } = useI18n();
const { accounts, account, accountId, selectAccount, accountsError, accountsPending, refetchAccounts } = useOrderAccount();
const symbol = shallowRef("");
const side = shallowRef("");
const lossOnly = shallowRef(false);
const symbols = shallowRef<string[]>([]);
const syncingId = shallowRef("");
const queued = shallowRef(false);
const syncError = shallowRef("");
const filtered = computed(() => Boolean(symbol.value || side.value || lossOnly.value));
const { data, error, isPending, refetch } = useQuery({
  queryKey: computed(() => ["live-positions", accountId.value, symbol.value, side.value, lossOnly.value] as const),
  enabled: computed(() => Boolean(accountId.value)),
  queryFn: ({ queryKey: [, id, instrument, direction, losses], signal }) => {
    const params = new URLSearchParams();
    if (instrument) params.set("symbol", instrument);
    if (direction) params.set("side", direction);
    if (losses) params.set("loss_only", "true");
    const method = document.visibilityState === "visible" ? "POST" : "GET";
    return api<LivePositions>(`/accounts/${id}/positions?${params}`, method, undefined, signal);
  },
  refetchInterval: 3_000,
  refetchOnWindowFocus: "always",
});
watch(accountId, () => {
  symbol.value = "";
  side.value = "";
  lossOnly.value = false;
  symbols.value = [];
  queued.value = false;
  syncError.value = "";
}, { flush: "sync" });
watch(data, (value) => {
  if (value?.account_id === accountId.value) symbols.value = value.symbols;
}, { immediate: true });
const symbolOptions = computed(() => symbol.value && !symbols.value.includes(symbol.value)
  ? [symbol.value, ...symbols.value] : symbols.value);
watch(() => data.value?.observed_to_ms, (next, previous) => {
  if (next && previous !== undefined && next !== previous) queued.value = false;
});
async function collect() {
  const id = accountId.value;
  if (!id || syncingId.value || !(data.value?.sync_enabled ?? account.value?.sync_enabled)) return;
  syncingId.value = id;
  queued.value = false;
  syncError.value = "";
  try {
    await api(`/accounts/${id}/sync`, "POST");
    if (id === accountId.value) {
      queued.value = true;
      await refetch();
    }
  } catch (err) {
    if (id === accountId.value) syncError.value = (err as Error).message;
  } finally {
    syncingId.value = "";
  }
}
</script>

<template>
  <div class="page-content live-positions">
    <header class="page-title">
      <h1>{{ t("orders.positions") }}</h1>
      <button class="btn" :disabled="!account || Boolean(syncingId) || !(data?.sync_enabled ?? account.sync_enabled)" @click="collect">
        <RefreshCw :size="15" :class="{ spin: syncingId === accountId && Boolean(accountId) }" />{{ t("account.collectNow") }}
      </button>
    </header>
    <div class="live-toolbar">
      <OrderAccountSelect :account-id="accountId" :accounts="accounts" @select="selectAccount" />
      <template v-if="data">
        <StatusBadge :state="data.sync_state" :stale="data.stale" :rest="data.market_rest" />
        <span v-if="data.observed_to_ms" class="muted small" data-live-observed>
          {{ t("livePositions.lastSnapshot", { time: trendDate(data.observed_to_ms) }) }}
        </span>
      </template>
    </div>
    <p v-if="accountsError || error" role="alert" class="notice notice-error">
      {{ localizeMessage((accountsError || error)!.message) }}
      <button class="text-button" @click="accountsError ? refetchAccounts() : refetch()">{{ t("overview.retry") }}</button>
    </p>
    <p v-if="syncError" role="alert" class="notice notice-error">{{ localizeMessage(syncError) }}</p>
    <p v-else-if="queued" role="status" class="notice notice-subtle">{{ t("account.snapshotCollectionQueued") }}</p>
    <template v-if="accountId">
      <div v-if="data && data.observed_to_ms !== null" class="panel live-totals">
        <div>
          <span>{{ t("account.floatingNetPL") }}</span>
          <strong class="num" :class="profitTone(data.floating_net_profit)" data-live-total>
            {{ signedMoney(data.floating_net_profit) }} <small>{{ data.currency }}</small>
          </strong>
        </div>
        <div>
          <span>{{ t("livePositions.positionCount") }}</span>
          <strong class="num" data-live-count>{{ data.total_count }}</strong>
        </div>
      </div>
      <section class="panel" :aria-label="t('account.openPositions')">
        <div class="live-filters">
          <label>{{ t("livePositions.symbol") }}
            <select v-model="symbol">
              <option value="">{{ t("livePositions.allSymbols") }}</option>
              <option v-for="name in symbolOptions" :key="name" :value="name">{{ name }}</option>
            </select>
          </label>
          <label>{{ t("livePositions.side") }}
            <select v-model="side">
              <option value="">{{ t("livePositions.allSides") }}</option>
              <option value="buy">{{ t("mt5.buy") }}</option>
              <option value="sell">{{ t("mt5.sell") }}</option>
            </select>
          </label>
          <label class="live-loss-filter"><input v-model="lossOnly" type="checkbox" />{{ t("livePositions.lossOnly") }}</label>
        </div>
        <p v-if="isPending" class="empty-inline" role="status">{{ t("overview.loading") }}</p>
        <template v-if="data">
          <p v-if="data.observed_to_ms === null" class="empty-inline">{{ t("account.waitingForTheFirstCompleteSnapshot") }}</p>
          <template v-else>
            <p v-if="filtered" class="live-subtotal" data-live-filtered>
              {{ t("livePositions.matches", { count: data.items.length }) }}
              <strong class="num" :class="profitTone(data.filtered_net_profit)">{{ signedMoney(data.filtered_net_profit) }} {{ data.currency }}</strong>
            </p>
            <LivePositionRows v-if="data.items.length" :key="accountId" :positions="data.items" :protocol="data.protocol" :currency="data.currency" />
            <p v-else class="empty-inline">{{ data.total_count ? t("livePositions.noMatches") : t("livePositions.empty") }}</p>
          </template>
        </template>
      </section>
    </template>
    <p v-else-if="accountsPending" role="status" class="empty-inline">{{ t("overview.loading") }}</p>
    <p v-else-if="!accountsError" class="empty-inline">{{ accounts.length ? t("orders.selectAccount") : t("livePositions.noAccounts") }}</p>
  </div>
</template>

<style scoped>
.live-toolbar, .live-filters {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.live-toolbar { margin-bottom: 16px; }
.live-totals {
  display: flex;
  flex-wrap: wrap;
  gap: 18px 48px;
  padding: 18px 22px;
  margin-bottom: 16px;
}
.live-totals span, .live-filters label {
  color: var(--color-muted);
  font-size: 12px;
}
.live-totals strong {
  display: block;
  font-size: clamp(22px, 3vw, 30px);
  overflow-wrap: anywhere;
}
.live-totals small { font-size: 14px; }
.live-positions .is-positive { color: var(--color-brand-700); }
.live-positions .is-negative { color: #b4493d; }
.live-filters {
  align-items: end;
  padding: 14px 18px;
}
.live-filters label {
  display: inline-grid;
  gap: 4px;
}
.live-filters select {
  padding: 6px 24px 6px 8px;
  color: var(--color-ink);
  background: var(--color-surface);
  border: 1px solid var(--color-line);
  border-radius: 6px;
  max-width: 100%;
}
.live-filters .live-loss-filter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 0;
}
.live-loss-filter input { accent-color: var(--color-brand-600); }
.live-subtotal {
  padding: 0 18px 12px;
  color: var(--color-muted);
  font-size: 12px;
}
.live-subtotal strong { margin-left: 6px; }
</style>
