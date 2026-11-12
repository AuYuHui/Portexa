<script setup lang="ts">
import { computed, defineAsyncComponent, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useQuery } from "@tanstack/vue-query";
import {
  RefreshCw,
  ShieldCheck,
  Clock3,
  ArrowUpRight,
  Info,
} from "@lucide/vue";
import { RouterLink } from "vue-router";
import { api } from "../api";
import { useAnalytics } from "../composables/useAnalytics";
import { localizeMessage } from "../i18n";
import { money, signedMoney, percent } from "../format";
import { trendDate } from "../components/charts/trendFormat";
import {
  performanceReasons,
  performanceEvent,
} from "../components/analytics/performanceFormat";
import PerformanceKpis from "../components/analytics/PerformanceKpis.vue";
import PerformanceCalendar from "../components/analytics/PerformanceCalendar.vue";
import AccountContribution from "../components/analytics/AccountContribution.vue";
import PerformanceDayDetails from "../components/analytics/PerformanceDayDetails.vue";
import TransferReview from "../components/analytics/TransferReview.vue";
import Modal from "../components/Modal.vue";
import type { Account } from "../types";
import type { PerformanceDay, PerformancePoint } from "../types/analytics";
import type { TrendRange } from "../types/trends";
const PerformanceChart = defineAsyncComponent(
  () => import("../components/analytics/PerformanceChart.vue"),
);
const { t } = useI18n();
const {
  data,
  error,
  isPending,
  isFetching,
  refetch,
  range,
  account,
  unit,
  customFrom,
  customTo,
  invalid,
  selectRange,
  selectAccount,
  selectUnit,
  applyCustom,
} = useAnalytics();
const { data: accounts } = useQuery({
  queryKey: ["accounts-for-analytics"],
  queryFn: () => api<Account[]>("/accounts"),
});
const mode = ref<"return" | "profit">("return");
const selected = shallowRef<PerformancePoint>();
const selectedDay = shallowRef<PerformanceDay>();
const dayOpen = ref(false);
const result = computed(() => data.value?.result);
const currentAccount = computed(() =>
  accounts.value?.find((item) => item.id === account.value),
);
const names = computed(() =>
  Object.fromEntries(
    (accounts.value || []).map((item) => [item.id, item.label]),
  ),
);
const ranges: { value: TrendRange; label: string }[] = [
  { value: "24h", label: "24H" },
  { value: "7d", label: "7D" },
  { value: "30d", label: "30D" },
  { value: "90d", label: "90D" },
];
const breakdown = computed(() =>
  result.value
    ? [
        {
          label: t("analytics.realized"),
          value: result.value.metrics.realized.value,
        },
        {
          label: t("analytics.floatingChange"),
          value: result.value.metrics.floating_change.value,
        },
        {
          label: t("analytics.otherIncome"),
          value: result.value.metrics.other_income.value,
        },
        {
          label: t("analytics.costs"),
          value: result.value.metrics.costs.value,
        },
      ]
    : [],
);
watch(data, () => {
  selected.value = undefined;
  dayOpen.value = false;
});
function openDay(day: PerformanceDay) {
  selectedDay.value = day;
  dayOpen.value = true;
}
</script>

<template>
  <div class="page-content performance-page">
    <div class="page-title">
      <div>
        <span class="eyebrow">PORTEXA ANALYTICS</span>
        <h1>{{ t("analytics.title") }}<span class="title-dot">.</span></h1>
        <p>{{ t("analytics.subtitle") }}</p>
      </div>
      <button class="btn" :disabled="isFetching || invalid" @click="refetch()">
        <RefreshCw :size="15" :class="{ spin: isFetching }" />{{
          t("analytics.refresh")
        }}
      </button>
    </div>
    <div class="performance-toolbar">
      <div class="performance-scope">
        <label class="sr-only" for="analytics-account">{{
          t("analytics.scope")
        }}</label
        ><select
          id="analytics-account"
          :value="account"
          @change="selectAccount(($event.target as HTMLSelectElement).value)"
        >
          <option value="">{{ t("analytics.allAccounts") }}</option>
          <option v-for="item in accounts" :key="item.id" :value="item.id">
            {{ item.label }} · {{ item.login }}
          </option>
        </select>
        <select
          v-if="account"
          :value="unit"
          :aria-label="t('analytics.displayUnit')"
          @change="selectUnit(($event.target as HTMLSelectElement).value)"
        >
          <option value="USD">USD</option>
          <option value="native">{{ t("trend.nativeUnit") }}</option>
        </select>
      </div>
      <div class="performance-range" :aria-label="t('analytics.range')">
        <button
          v-for="item in ranges"
          :key="item.value"
          :aria-pressed="range === item.value"
          @click="selectRange(item.value)"
        >
          {{ item.label }}</button
        ><button
          :aria-pressed="range === 'custom'"
          @click="selectRange('custom')"
        >
          {{ t("trend.custom") }}
        </button>
      </div>
    </div>
    <form
      v-if="range === 'custom'"
      class="performance-custom"
      @submit.prevent="applyCustom"
    >
      <label
        >{{ t("trend.start")
        }}<input v-model="customFrom" type="datetime-local" required /></label
      ><label
        >{{ t("trend.end")
        }}<input v-model="customTo" type="datetime-local" required /></label
      ><button class="btn btn-primary">{{ t("trend.apply") }}</button>
    </form>
    <p v-if="invalid" role="alert" class="notice notice-error">
      {{ t("trend.invalidRange") }}
    </p>
    <p v-if="error" role="alert" class="notice notice-error">
      {{ localizeMessage(error.message)
      }}<button class="text-button" @click="refetch()">
        {{ t("overview.retry") }}
      </button>
    </p>
    <div
      v-if="isPending && !invalid"
      class="skeleton-grid"
      role="status"
      :aria-label="t('analytics.loading')"
    >
      <div v-for="n in 4" :key="n" class="skeleton" />
    </div>
    <template v-if="data && result">
      <div
        class="performance-assurance"
        :class="{ 'has-issues': result.quality.reasons.length || data.stale }"
      >
        <ShieldCheck :size="17" /><strong>{{
          result.quality.reasons.length
            ? t("analytics.partialEvidence")
            : t("analytics.evidenceReady")
        }}</strong
        ><span>{{
          t("analytics.observations", {
            valid: result.quality.valid_points,
            total: result.quality.expected_points,
          })
        }}</span
        ><span class="performance-cutoff"
          ><Clock3 :size="13" />{{ t("analytics.asOf") }}
          {{ trendDate(result.to_ms) }}</span
        >
      </div>
      <p class="performance-scope-note">
        {{ trendDate(result.from_ms) }} — {{ trendDate(result.to_ms) }} ·
        {{ t("trend.beijing") }} ·
        {{ account ? currentAccount?.label : t("analytics.usdScope")
        }}<span v-if="data.stale"> · {{ t("analytics.stale") }}</span
        ><span v-if="data.pending_accounts || data.excluded_accounts">
          ·
          {{
            t("analytics.excluded", {
              pending: data.pending_accounts,
              excluded: data.excluded_accounts,
            })
          }}</span
        >
      </p>
      <PerformanceKpis :result="result" />
      <div class="performance-bridge">
        <span>{{ t("analytics.profitBreakdown") }}</span
        ><span v-for="item in breakdown" :key="item.label"
          >{{ item.label
          }}<strong class="num">{{ signedMoney(item.value) }}</strong></span
        ><small>{{ result.currency }}</small>
      </div>
      <section
        class="panel performance-chart-panel"
        :aria-label="t('analytics.performance')"
      >
        <div class="performance-panel-heading">
          <div>
            <span class="performance-section-no">01 /</span>
            <h2>{{ t("analytics.performance") }}</h2>
            <p>{{ t("analytics.performanceHint") }}</p>
          </div>
          <div class="performance-chart-mode">
            <button :aria-pressed="mode === 'return'" @click="mode = 'return'">
              {{ t("analytics.returnCurve") }}</button
            ><button :aria-pressed="mode === 'profit'" @click="mode = 'profit'">
              {{ t("analytics.profitCurve") }}
            </button>
          </div>
        </div>
        <PerformanceChart
          :key="[account, range, unit, result.from_ms, result.to_ms].join(':')"
          :result="result"
          :mode="mode"
          @select="selected = $event"
        />
        <div v-if="selected" class="performance-selected" aria-live="polite">
          <strong>{{ trendDate(selected.time_ms) }}</strong
          ><span
            >{{ t("analytics.netProfit") }}
            {{ signedMoney(selected.net_profit) }} {{ result.currency }}</span
          ><span
            >{{ t("analytics.return") }}
            {{ percent(selected.return_pct) }}</span
          ><span
            >{{ t("analytics.currentDrawdown") }}
            {{ percent(selected.drawdown_pct) }}</span
          >
        </div>
        <div class="performance-plot-footnote">
          <span
            ><i class="performance-green-line" />{{
              mode === "return"
                ? t("analytics.returnCurve")
                : t("analytics.profitCurve")
            }}</span
          ><span
            ><i class="performance-amber-line" />{{
              t("analytics.drawdownCurve")
            }}</span
          ><small>{{ t("analytics.samplingNote") }}</small>
        </div>
      </section>
      <PerformanceCalendar
        :days="result.days"
        :months="result.months"
        :currency="result.currency"
        @select="openDay"
      />
      <AccountContribution v-if="!account" :accounts="data.accounts" />
      <RouterLink
        v-else
        :to="`/accounts/${account}`"
        class="performance-account-shortcut"
        >{{ t("analytics.accountDetails") }}<ArrowUpRight :size="16"
      /></RouterLink>
      <section
        class="panel performance-quality"
        :aria-label="t('analytics.dataQuality')"
      >
        <details :open="result.quality.reasons.length > 0">
          <summary>
            <ShieldCheck :size="18" /><strong>{{
              t("analytics.dataQuality")
            }}</strong
            ><span>{{ t("analytics.auditHint") }}</span>
          </summary>
          <div class="performance-quality-body">
            <p
              v-if="result.quality.reasons.length"
              class="notice notice-warning"
            >
              {{ performanceReasons(result.quality.reasons) }}
            </p>
            <dl class="performance-quality-grid">
              <div>
                <dt>{{ t("analytics.history") }}</dt>
                <dd>
                  {{
                    result.quality.history_complete
                      ? t("analytics.covered")
                      : t("analytics.insufficient")
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t("analytics.reconciliation") }}</dt>
                <dd>
                  {{
                    result.quality.reconciliation === "passed"
                      ? t("analytics.passed")
                      : result.quality.reconciliation === "failed"
                        ? t("analytics.failed")
                        : t("analytics.unavailable")
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t("analytics.balanceResidual") }}</dt>
                <dd class="num">
                  {{ money(result.quality.balance_residual, 8) }}
                  {{ result.currency }}
                </dd>
              </div>
              <div>
                <dt>{{ t("analytics.creditResidual") }}</dt>
                <dd class="num">
                  {{ money(result.quality.credit_residual, 8) }}
                  {{ result.currency }}
                </dd>
              </div>
              <div>
                <dt>{{ t("analytics.pendingFunds") }}</dt>
                <dd>{{ result.quality.unclassified }}</dd>
              </div>
              <div>
                <dt>{{ t("analytics.sourceRevision") }}</dt>
                <dd class="num">{{ data.source_revision }}</dd>
              </div>
              <div>
                <dt>{{ t("analytics.actualWindow") }}</dt>
                <dd>
                  {{ trendDate(result.quality.observed_from_ms) }} —
                  {{ trendDate(result.quality.observed_to_ms) }}
                </dd>
              </div>
              <div>
                <dt>{{ t("analytics.method") }}</dt>
                <dd>Modified Dietz · {{ data.calculation_version }}</dd>
              </div>
            </dl>
            <p class="performance-evidence">
              <span>{{ t("analytics.evidence") }}</span
              ><code>{{ result.evidence_id }}</code>
            </p>
            <p class="muted small">
              {{ t("analytics.sourceLimit") }}
              {{ t("analytics.independentLimit") }}
            </p>
            <TransferReview
              v-if="!account && data.unmatched_transfers.length"
              :events="data.unmatched_transfers"
              :names="names"
            />
            <details v-if="data.events.length" class="performance-events">
              <summary>
                {{
                  t("analytics.capitalEvents", { count: data.events.length })
                }}
              </summary>
              <ul>
                <li
                  v-for="event in data.events"
                  :key="`${event.account_id}:${event.ticket}:${event.revision}`"
                >
                  <span>{{ trendDate(event.time_ms) }}</span
                  ><RouterLink :to="`/accounts/${event.account_id}`"
                    >{{ performanceEvent(event.kind) }} #{{
                      event.ticket
                    }}</RouterLink
                  ><strong class="num"
                    >{{ signedMoney(event.amount) }}
                    {{ result.currency }}</strong
                  >
                </li>
              </ul>
              <p v-if="data.events_truncated" class="muted small">
                {{ t("analytics.eventsTruncated") }}
              </p>
            </details>
          </div>
        </details>
      </section>
      <p class="performance-method-note">
        <Info :size="14" />{{ t("analytics.methodNote") }}
      </p>
    </template>
    <Modal
      v-model:open="dayOpen"
      :title="`${t('analytics.dayDetails')} · ${selectedDay?.date || ''}`"
      :description="t('analytics.dayDescription')"
      ><PerformanceDayDetails
        v-if="selectedDay && result"
        :key="`${account}:${selectedDay.date}`"
        :day="selectedDay"
        :account-id="account"
        :unit="unit"
        :currency="result.currency"
    /></Modal>
  </div>
</template>
