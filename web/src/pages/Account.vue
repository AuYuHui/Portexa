<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import {
  ArrowLeft,
  RefreshCw,
  Settings2,
  Pause,
  Play,
  Info,
  AlertTriangle,
} from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import {
  money,
  signedMoney,
  date,
  failureLabel,
  positive,
  marginLevel,
} from "../format";
import type { Detail, Snapshot } from "../types";
import StatusBadge from "../components/StatusBadge.vue";
import Modal from "../components/Modal.vue";
import AccountForm from "../components/AccountForm.vue";
import PositionTable from "../components/PositionTable.vue";
import DailySummary from "../components/DailySummary.vue";
import FundingSummary from "../components/FundingSummary.vue";
import EquityTrend from "../components/charts/EquityTrend.vue";

const { t } = useI18n();
const route = useRoute();
const id = computed(() => String(route.params.id));
const sort = ref("ticket");
const { data, error, isPending } = useQuery({
  queryKey: ["account", id, sort],
  queryFn: () => api<Detail>(`/accounts/${id.value}?sort=${sort.value}`),
  refetchInterval: 10_000,
});
const { data: snapshots, error: snapshotsError } = useQuery({
  queryKey: ["snapshots", id],
  queryFn: () => api<Snapshot[]>(`/accounts/${id.value}/snapshots?limit=10`),
  refetchInterval: 10_000,
});
const tab = ref("positions");
const open = ref(false);
const busy = ref(false);
const actionError = ref("");
const message = ref<string | (() => string)>("");
const current = computed(() => data.value?.account.snapshot);
const positions = computed(
  () =>
    data.value?.positions.filter((p) =>
      tab.value === "positions" ? p.command <= 1 : p.command >= 2,
    ) || [],
);
async function requestSync() {
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}/sync`, "POST");
    message.value = () => t('account.snapshotCollectionQueued');
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function toggleSync() {
  if (!data.value) return;
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}`, "PATCH", {
      config_version: data.value.account.config_version,
      sync_enabled: !data.value.account.sync_enabled,
    });
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="page-content">
    <RouterLink to="/" class="back-link"
      ><ArrowLeft :size="16" />{{ t('account.allAccounts') }}</RouterLink
    >
    <p v-if="error" class="notice notice-error" role="alert">
      {{ localizeMessage(error.message) }}
    </p>
    <p v-if="isPending" class="empty-inline">{{ t('account.loadingAccount') }}</p>
    <template v-if="data">
      <div class="page-title detail-title">
        <div>
          <span class="eyebrow">ACCOUNT DETAIL</span>
          <h1>{{ data.account.label }}<span class="title-dot">.</span></h1>
          <p class="muted num">
            {{ data.account.login }} <span class="divider-dot">/</span>
            {{ data.account.broker_name }} <span class="divider-dot">/</span>
            {{ data.account.server_name }}
          </p>
        </div>
        <div class="page-actions">
          <button class="btn" :disabled="busy" @click="open = true">
            <Settings2 :size="16" />{{ t('account.accountSettings') }}</button
          ><button class="btn" :disabled="busy" @click="toggleSync">
            <Pause v-if="data.account.sync_enabled" :size="16" /><Play
              v-else
              :size="16"
            />{{ data.account.sync_enabled ? t('account.pauseCollection') : t('account.resumeCollection') }}</button
          ><button
            class="btn btn-primary"
            :disabled="busy || !data.account.sync_enabled"
            @click="requestSync"
          >
            <RefreshCw :size="16" :class="{ spin: busy }" />{{ t('account.collectNow') }}
          </button>
        </div>
      </div>
      <p v-if="actionError" role="alert" class="notice notice-error">
        {{ localizeMessage(actionError) }}
      </p>
      <p v-if="message" role="status" class="notice notice-subtle">
        {{ localizeMessage(message) }}
      </p>
      <div class="detail-status">
        <StatusBadge
          :state="data.account.sync_state"
          :stale="data.account.stale"
        /><span class="muted small">{{
          current
            ? t('account.latestSample', { value1: date(current.observed_from_ms), value2: date(current.observed_to_ms) })
            : t('account.noCompleteSnapshotYetFundsAndPositionsAre')
        }}</span
        ><span class="unit-label"
          >{{
            current?.native_currency ||
            data.account.confirmed_currency ||
            t('account.unitUnconfirmed')
          }}
          ·
          {{ data.account.unit_confirmed ? t('account.unitConfirmed') : t('account.conversionUnconfirmed') }}</span
        >
      </div>
      <div
        v-if="
          data.account.last_run?.error_code &&
          data.account.last_run.state !== 'succeeded'
        "
        class="notice notice-warning"
      >
        <AlertTriangle :size="17" />
        <p>
          {{ failureLabel(data.account.last_run.error_code)
          }}{{
            data.account.last_run.raw_status !== null
              ? t('account.serverStatusThisDoesNotEstablishAPassword', { value1: data.account.last_run.raw_status })
              : ""
          }} {{ current ? t('account.showingTheLastCompleteSnapshot') : t('account.waitingForTheFirstCompleteSnapshot') }}
        </p>
      </div>
      <section class="summary-grid detail-metrics">
        <article class="summary-card">
          <div class="card-label">
            {{ t('account.accountEquity') }}
            <span class="badge badge-neutral">{{
              current?.native_currency || t('account.nativeCurrency')
            }}</span>
          </div>
          <div class="metric-value num">
            {{ money(current?.equity) }}
            <small v-if="current?.native_currency">{{
              current.native_currency
            }}</small>
          </div>
          <p class="metric-note">{{ t('account.includesCreditFollowingTheSdkDefinition') }}</p>
        </article>
        <article class="summary-card">
          <div class="card-label">{{ t('account.balanceCredit') }}</div>
          <div class="metric-value num">
            {{ money(current?.balance) }}
            <small v-if="current?.native_currency">{{
              current.native_currency
            }}</small>
          </div>
          <p class="metric-note num">
            {{ t('account.credit') }} {{ money(current?.credit) }}
            {{ current?.native_currency || "" }}
          </p>
        </article>
        <article class="summary-card">
          <div class="card-label">{{ t('account.floatingNetPL') }}</div>
          <div
            class="metric-value num"
            :class="{
              positive: current && positive(current.floating_net_profit),
              negative: current && !positive(current.floating_net_profit),
            }"
          >
            {{ signedMoney(current?.floating_net_profit) }}
            <small v-if="current?.native_currency">{{
              current.native_currency
            }}</small>
          </div>
          <p class="metric-note">{{ t('account.openBuySellPositionsIncludingReportedFees') }}</p>
        </article>
        <article class="summary-card">
          <div class="card-label" :title="t('account.accountEquityUsedMargin')">
            {{ t('account.marginLevel') }}
          </div>
          <div class="metric-value num">
            {{ marginLevel(current) }}
          </div>
          <p class="metric-note">
            {{
              current?.margin_available
                ? t('account.usedLeverage', { value1: money(current.margin), value2: current.native_currency, value3: current.leverage })
                : t('account.marginDataUnavailable')
            }}
          </p>
        </article>
      </section>
      <EquityTrend :account-id="id" />
      <DailySummary v-if="data.account.daily" :account="data.account" />
      <FundingSummary v-if="data.account.funding" :account="data.account" />
      <section class="panel">
        <div class="panel-heading">
          <div class="tabs" :aria-label="t('account.orderType')">
            <button
              :class="{ active: tab === 'positions' }"
              @click="tab = 'positions'"
            >
              {{ t('account.openPositions') }}
              <span>{{
                data.positions.filter((p) => p.command <= 1).length
              }}</span></button
            ><button
              :class="{ active: tab === 'pending' }"
              @click="tab = 'pending'"
            >
              {{ t('account.pendingOrders') }}
              <span>{{
                data.positions.filter((p) => p.command >= 2).length
              }}</span>
            </button>
          </div>
          <label class="sort-label"
            >{{ t('account.sort') }}<select v-model="sort" :aria-label="t('account.orderSorting')">
              <option value="ticket">{{ t('account.ticket') }}</option>
              <option value="open_time_wall">{{ t('account.serverOpenTime') }}</option>
            </select></label
          >
        </div>
        <PositionTable
          v-if="current"
          :key="`${id}:${tab}`"
          :positions="positions"
        />
        <p v-else class="empty-inline">
          {{ t('account.noCompleteSnapshotYetMissingPositionsDoNot') }}
        </p>
        <div class="panel-footer">
          <span
            ><Info :size="14" />{{ t('account.openTimesUseTheServerWallClock') }} {{
              data.server_time_status === "verified"
                ? `(${data.account.daily.time_zone})`
                : t('account.timeZoneUnverified')
            }}</span
          ><span>{{ t('account.amountsShownInNativeCurrency') }}</span>
        </div>
      </section>
      <section class="panel snapshot-panel">
        <div class="panel-heading">
          <div>
            <h2>{{ t('account.recentSuccessfulSnapshots') }}</h2>
            <p class="muted small">
              {{ t('account.theLastCompleteCollectionsFailedCollectionsNeverBecome') }}
            </p>
          </div>
          <span class="badge badge-neutral">{{ t('account.snapshotDetails') }}</span>
        </div>
        <p v-if="snapshotsError" class="error-message">
          {{ localizeMessage(snapshotsError.message) }}
        </p>
        <div class="table-scroll">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ t('account.sampleStarted') }}</th>
                <th>{{ t('account.sampleCompleted') }}</th>
                <th class="text-right">{{ t('account.balance') }}</th>
                <th class="text-right">{{ t('account.equity') }}</th>
                <th class="text-right">{{ t('account.floatingNetPL') }}</th>
                <th>{{ t('account.unit') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in snapshots" :key="s.id">
                <td class="num muted small">{{ date(s.observed_from_ms) }}</td>
                <td class="num small">{{ date(s.observed_to_ms) }}</td>
                <td class="num text-right">{{ money(s.balance) }}</td>
                <td class="num text-right">{{ money(s.equity) }}</td>
                <td
                  class="num text-right"
                  :class="
                    positive(s.floating_net_profit) ? 'positive' : 'negative'
                  "
                >
                  {{ money(s.floating_net_profit) }}
                </td>
                <td>{{ s.native_currency }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="snapshots && !snapshots.length" class="empty-inline">
            {{ t('account.snapshotsAccumulateAfterTheFirstCompleteCollection') }}
          </p>
        </div>
      </section>
      <Modal
        v-model:open="open"
        :title="t('account.accountSettings')"
        :description="t('account.changesCancelPreviousJobsTheConfigurationVersionIs')"
      >
        <AccountForm
          :account="data.account"
          @saved="open = false"
          @cancel="open = false"
        />
      </Modal>
    </template>
  </div>
</template>
