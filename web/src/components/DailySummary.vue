<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import { RouterLink } from "vue-router";
import { RefreshCw } from "@lucide/vue";
import type { Account } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import {
  money,
  percent,
  date,
  positive,
  dailyLabel,
  failureLabel,
} from "../format";

const { t } = useI18n();

const props = defineProps<{ account: Account }>();
const busy = ref(false);
const message = ref<string | (() => string)>("");
const error = ref("");

async function refresh() {
  busy.value = true;
  error.value = "";
  message.value = "";
  try {
    await api(`/accounts/${props.account.id}/daily/sync`, "POST");
    message.value = () => t('dailySummary.dailyHistoryCollectionQueuedFundsAndPositionsAre');
    await refreshAccounts();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="panel daily-panel" :aria-label="t('dailySummary.todaySProfitAndBalanceChange')">
    <div class="panel-heading">
      <div>
        <h2>{{ t('dailySummary.todaySTradingStatistics') }}</h2>
        <p class="muted small">
          {{ account.daily.date || t('dailySummary.byMtServerDate') }}
          <template v-if="account.daily.time_zone">
            · {{ account.daily.time_zone }}
          </template>
          <span
            class="badge"
            :class="
              account.daily.state === 'succeeded' && !account.daily.stale
                ? 'badge-good'
                : 'badge-warning'
            "
            >{{ dailyLabel(account.daily.state, account.daily.stale) }}</span
          >
        </p>
      </div>
      <button
        class="btn btn-small"
        :disabled="
          busy ||
          !account.sync_enabled ||
          account.daily.state === 'time_zone_unconfirmed'
        "
        @click="refresh"
      >
        <RefreshCw :size="14" :class="{ spin: busy }" />{{ t('dailySummary.refreshToday') }}
      </button>
    </div>
    <div
      v-if="account.daily.state === 'time_zone_unconfirmed'"
      class="empty-inline"
    >
      {{ t('dailySummary.serverTimeIsBeingCalibratedFromNewlyClosed') }}
      <RouterLink to="/settings" class="text-button"
        >{{ t('dailySummary.viewServerTimeSettings') }}</RouterLink
      >
    </div>
    <template v-else>
      <div class="daily-metrics">
        <div>
          <div class="card-label">
            {{ t('accountTable.todaySProfit') }}
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency ||
              t('account.nativeCurrency')
            }}</span>
          </div>
          <div
            class="metric-value num"
            :class="
              account.daily.report
                ? positive(account.daily.report.net_profit)
                  ? 'positive'
                  : 'negative'
                : ''
            "
          >
            {{ money(account.daily.report?.net_profit) }}
          </div>
          <p class="metric-note">{{ t('dailySummary.closedNetProfitIncludingCommissionSwapAndTaxes') }}</p>
        </div>
        <div>
          <div class="card-label">
            {{ t('accountTable.todaySBalanceChange') }}
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency ||
              t('account.nativeCurrency')
            }}</span>
          </div>
          <div
            class="metric-value num"
            :class="
              account.daily.report
                ? positive(account.daily.report.balance_change)
                  ? 'positive'
                  : 'negative'
                : ''
            "
          >
            {{ money(account.daily.report?.balance_change) }}
          </div>
          <p class="metric-note">{{ t('dailySummary.todaySProfitBalanceAdjustmentsExcludingCredit') }}</p>
        </div>
        <div>
          <div class="card-label">{{ t('dailySummary.todaySClosedTrades') }}</div>
          <div class="metric-value num">
            {{ account.daily.report?.closed_trades ?? "—" }}<small>{{ t('dailySummary.trades') }}</small>
          </div>
          <p class="metric-note">{{ t('dailySummary.closedBuySellTradesOnlyGroupedByClosing') }}</p>
        </div>
        <div>
          <div class="card-label">{{ t('dailySummary.todaySWinRate') }}</div>
          <div class="metric-value num">
            {{ percent(account.daily.report?.win_rate) }}
          </div>
          <p class="metric-note">
            {{ t('dailySummary.netWinningTradesClosedTradesIncludingBreakevenTrades') }}
            <template v-if="account.daily.report?.breakeven_trades != null">
              {{ t('dailySummary.breakevenCount', { count: account.daily.report.breakeven_trades }) }}</template
            >
          </p>
        </div>
        <div>
          <div class="card-label">
            {{ t('dailySummary.todaySGains') }}
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency ||
              t('account.nativeCurrency')
            }}</span>
          </div>
          <div class="metric-value num positive">
            {{ money(account.daily.report?.gross_profit) }}
          </div>
          <p class="metric-note">
            {{ t('dailySummary.winningTrades', { count: account.daily.report?.winning_trades ?? '—' }) }}
          </p>
        </div>
        <div>
          <div class="card-label">
            {{ t('dailySummary.todaySLosses') }}
            <span>{{
              account.daily.report?.native_currency ||
              account.snapshot?.native_currency ||
              t('account.nativeCurrency')
            }}</span>
          </div>
          <div class="metric-value num negative">
            {{ money(account.daily.report?.gross_loss) }}
          </div>
          <p class="metric-note">
            {{ t('dailySummary.losingTrades', { count: account.daily.report?.losing_trades ?? '—' }) }}
          </p>
        </div>
      </div>
      <p
        v-if="
          account.daily.report && account.daily.report.winning_trades == null
        "
        class="daily-note muted small"
      >
        {{ t('dailySummary.olderDailyReportsLackWinLossDetailsThe') }}
      </p>
      <p
        v-else-if="account.daily.report?.closed_trades === 0"
        class="daily-note muted small"
      >
        {{ t('dailySummary.noTradesClosedTodayWinRateIsUnavailable') }}
      </p>
      <p v-if="!account.daily.report" class="daily-note muted small">
        {{ t('dailySummary.waitingForCompleteDailyHistoryMissingDataIs') }}
      </p>
      <p v-if="account.daily.error_code" class="daily-note warning-text small">
        {{ failureLabel(account.daily.error_code) || t('dailySummary.todaySCollectionIsNotCompleteYet') }}
        <template v-if="account.daily.raw_status != null">
          {{ t('dailySummary.rawServerStatus', { status: account.daily.raw_status }) }}
        </template>
        {{
          account.daily.report ? t('dailySummary.retainingThePreviousDailyResult') : t('dailySummary.noCompleteDailyResultYet')
        }}
      </p>
      <div v-if="account.daily.report" class="panel-footer">
        <span class="num">
          {{ t('dailySummary.reportTotals', { count: account.daily.report.closed_trades, adjustment: money(account.daily.report.balance_adjustment), credit: money(account.daily.report.credit_change) }) }}
        </span>
        <span>
          {{ t('account.sampleCompleted') }} {{ date(account.daily.report.observed_to_ms) }} {{ t('dailySummary.nativeCurrencyUnconverted') }}
        </span>
      </div>
    </template>
    <p v-if="message" class="daily-note muted small" role="status">
      {{ localizeMessage(message) }}
    </p>
    <p v-if="error" class="daily-note warning-text small" role="alert">
      {{ localizeMessage(error) }}
    </p>
  </section>
</template>
