<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Activity, RefreshCw, Info } from "@lucide/vue";
import { localizeMessage } from "../../i18n";
import { money } from "../../format";
import { useEquitySeries } from "../../composables/useEquitySeries";
import type { TrendPoint } from "../../types/trends";
import { chartAmount, eventName, pointQuality, trendDate } from "./trendFormat";
import TrendToolbar from "./TrendToolbar.vue";

const TrendChart = defineAsyncComponent(() => import("./TrendChart.vue"));
const props = defineProps<{ accountId?: string }>();
const { t } = useI18n();
const {
  data, error, isPending, isFetching, refetch, range, metric, unit, customFrom, customTo, invalid,
  selectRange, selectMetric, selectUnit, applyCustom,
} = useEquitySeries(() => props.accountId);
const selected = ref<TrendPoint>();
const currencyChoice = ref("");
const detailsOpen = ref(false);
const detailPage = ref(0);
const currency = computed(() => unit.value === "USD" ? "USD" : currencyChoice.value || data.value?.native_currency || "");
const points = computed(() => (data.value?.points || []).filter((point) => point.currency === currency.value));
const events = computed(() => (data.value?.events || []).filter((event) => event.currency === currency.value));
const valid = computed(() => points.value.filter((point) => point.equity !== null));
const plottable = computed(() => valid.value.some((point) => chartAmount(point.equity) !== null || chartAmount(point.balance) !== null));
const activePoint = computed(() => selected.value && points.value.includes(selected.value) ? selected.value : valid.value.at(-1));
const detailPoints = computed(() => points.value.slice(detailPage.value * 20, detailPage.value * 20 + 20));
const chartKey = computed(() => [props.accountId, range.value, unit.value, currency.value, customFrom.value, customTo.value].join(":"));
const partial = computed(() => !!data.value && !!(data.value.gap_points || data.value.break_count || data.value.pending_accounts || data.value.unknown_cadence));
const emptyReason = computed(() => {
  const last = points.value.at(-1);
  return last?.reason ? pointQuality(last) : t("trend.empty");
});

watch(data, () => { selected.value = undefined; detailPage.value = 0; }, { flush: "sync" });
watch(() => props.accountId, () => { currencyChoice.value = ""; selected.value = undefined; });
watch(currency, () => { selected.value = undefined; detailPage.value = 0; });
</script>

<template>
  <section class="panel trend-panel" :aria-label="accountId ? t('trend.accountTitle') : t('trend.portfolioTitle')" :aria-busy="isFetching">
    <div class="panel-heading trend-heading">
      <div>
        <span class="trend-eyebrow">ASSET HISTORY</span>
        <h2><Activity :size="18" />{{ accountId ? t('trend.accountTitle') : t('trend.portfolioTitle') }}</h2>
        <p>{{ accountId ? t('trend.accountSubtitle') : t('trend.portfolioSubtitle') }}</p>
      </div>
      <button class="icon-button" type="button" :disabled="isFetching || invalid" :aria-label="t('trend.refresh')" @click="refetch()">
        <RefreshCw :size="16" :class="{ spin: isFetching }" />
      </button>
    </div>
    <TrendToolbar :range="range" :metric="metric" :unit="unit" :account="!!accountId" :can-convert-u-s-d="!!data?.can_convert_usd"
      v-model:custom-from="customFrom" v-model:custom-to="customTo"
      @range="selectRange" @metric="selectMetric" @unit="selectUnit" @apply="applyCustom" />
    <p v-if="invalid" class="error-message trend-feedback" role="alert">{{ t('trend.invalidRange') }}</p>
    <div v-else-if="error" class="error-message trend-feedback" role="alert">
      {{ localizeMessage(error.message) }}
      <button class="text-button" type="button" @click="refetch()">{{ t('overview.retry') }}</button>
    </div>
    <p v-else-if="isPending" class="empty-inline" role="status">{{ t('trend.loading') }}</p>
    <template v-else-if="data">
      <div class="trend-meta">
        <span class="badge" :class="data.stale || partial ? 'badge-warning' : 'badge-good'">
          {{ data.stale ? t('trend.stale') : partial ? t('trend.partial') : t('trend.available') }}
        </span>
        <span>{{ t('trend.coverage', { valid: data.valid_points, total: data.total_points }) }}</span>
        <span>{{ t('trend.beijing') }}</span>
        <span v-if="data.downsampled">{{ t('trend.downsampled', { count: data.points.length }) }}</span>
        <select v-if="unit === 'native' && data.currencies.length > 1" v-model="currencyChoice" :aria-label="t('trend.historicalCurrency')">
          <option value="">{{ data.native_currency }}</option>
          <option v-for="value in data.currencies.filter((item) => item !== data.native_currency)" :key="value" :value="value">{{ value }}</option>
        </select>
      </div>
      <p v-if="data.pending_accounts || data.excluded_accounts" class="trend-scope-note">
        {{ t('trend.excluded', { pending: data.pending_accounts, excluded: data.excluded_accounts }) }}
      </p>
      <template v-if="activePoint">
        <div class="trend-values" aria-live="polite">
          <div><span class="trend-legend equity">{{ t('account.equity') }}</span><strong class="num">{{ money(activePoint.equity) }} <small>{{ currency }}</small></strong></div>
          <div><span class="trend-legend balance">{{ t('account.balance') }}</span><strong class="num">{{ money(activePoint.balance) }} <small>{{ currency }}</small></strong></div>
          <p class="muted small">{{ selected ? t('trend.selectedSample') : t('trend.lastValidSample') }}<br /><span class="num">{{ trendDate(activePoint.time_ms) }}</span></p>
        </div>
      </template>
      <TrendChart v-if="points.length && plottable" :points="points" :events="events" :metric="metric" :currency="currency"
        :from="data.from_ms" :to="data.to_ms" :reset-key="chartKey" @select="selected = $event" />
      <div v-else class="trend-empty" role="status">
        <Activity :size="30" />
        <strong>{{ valid.length ? t('trend.unplottable') : emptyReason }}</strong>
        <p>{{ t('trend.emptyDescription') }}</p>
      </div>
      <div v-if="selected" class="trend-selection" data-selected-trend>
        <strong>{{ trendDate(selected.time_ms) }} · {{ pointQuality(selected) }}</strong>
        <span>{{ t('trend.credit') }} {{ money(selected.credit) }} {{ selected.currency }}</span>
        <span>{{ t('account.floatingNetPL') }} {{ money(selected.floating_net_profit) }} {{ selected.currency }}</span>
        <span>{{ t('trend.accountCoverage', { included: selected.included_accounts, expected: selected.expected_accounts }) }}</span>
        <span>{{ t('overview.samplingWindow') }} {{ trendDate(selected.observed_from_ms) }} — {{ trendDate(selected.observed_to_ms) }}</span>
      </div>
      <details v-if="events.length" class="trend-events">
        <summary>{{ t('trend.events', { count: events.length }) }}</summary>
        <ul><li v-for="event in events" :key="[event.account_id, event.kind, event.ticket, event.time_ms].join(':')">
          <span class="num">{{ trendDate(event.time_ms) }}</span><strong>{{ eventName(event) }}</strong>
          <span v-if="event.amount !== null" class="num">{{ money(event.amount) }} {{ event.currency }}</span>
        </li></ul>
      </details>
      <details v-if="points.length" class="trend-details" @toggle="detailsOpen = ($event.target as HTMLDetailsElement).open">
        <summary>{{ t('trend.details') }} <span class="muted">{{ t('trend.clickSample') }}</span></summary>
        <template v-if="detailsOpen">
          <div class="table-scroll">
            <table class="data-table trend-table">
              <thead><tr><th>{{ t('account.sampleCompleted') }}</th><th class="text-right">{{ t('account.equity') }}</th><th class="text-right">{{ t('account.balance') }}</th><th>{{ t('trend.quality') }}</th></tr></thead>
              <tbody><tr v-for="point in detailPoints" :key="[point.time_ms, point.snapshot_id].join(':')">
                <td><button class="text-button num" type="button" @click="selected = point">{{ trendDate(point.time_ms) }}</button></td>
                <td class="text-right num">{{ money(point.equity) }} {{ point.currency }}</td>
                <td class="text-right num">{{ money(point.balance) }} {{ point.currency }}</td>
                <td>{{ pointQuality(point) }}</td>
              </tr></tbody>
            </table>
          </div>
          <div class="trend-pagination">
            <button class="btn btn-small" type="button" :disabled="detailPage === 0" @click="detailPage--">{{ t('trend.previous') }}</button>
            <span class="muted small">{{ detailPage + 1 }} / {{ Math.ceil(points.length / 20) }}</span>
            <button class="btn btn-small" type="button" :disabled="(detailPage + 1) * 20 >= points.length" @click="detailPage++">{{ t('trend.next') }}</button>
          </div>
        </template>
      </details>
      <div class="trend-footnote">
        <Info :size="14" /><p>{{ t('trend.assetNotReturn') }} {{ t('trend.creditIncluded') }}
          <span v-if="!accountId">{{ t('trend.alignment') }}</span>
          <span v-if="data.unknown_cadence">{{ t('trend.unknownCadence') }}</span>
          <span v-if="data.unplaced_events || data.events_truncated">{{ t('trend.partialEvents') }}</span>
          {{ t('trend.markersScope') }}
        </p>
      </div>
    </template>
  </section>
</template>
