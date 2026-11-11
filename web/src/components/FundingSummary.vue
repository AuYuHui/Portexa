<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref, shallowRef, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { RefreshCw } from "@lucide/vue";
import type { Account, FundEntry, FundsPage } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import {
  money,
  signedMoney,
  date,
  wallTime,
  positive,
  fundingLabel,
  fundCategories,
  failureLabel,
} from "../format";
import Modal from "./Modal.vue";
import FundClassificationForm from "./FundClassificationForm.vue";

const { t } = useI18n();

const props = defineProps<{ account: Account }>();
const funding = computed(() => props.account.funding!);
const id = computed(() => props.account.id);
const cursor = ref("");
const previous = ref<string[]>([]);
const unclassified = ref(false);
const { data, error, isPending, isFetching, refetch } = useQuery({
  queryKey: ["funds", id, cursor, unclassified],
  queryFn: () =>
    api<FundsPage>(
      `/accounts/${id.value}/funds?cursor=${encodeURIComponent(cursor.value)}&unclassified=${unclassified.value ? 1 : 0}`,
    ),
  refetchInterval: 10_000,
});
watch([id, unclassified], () => {
  cursor.value = "";
  previous.value = [];
});
const selected = shallowRef<FundEntry | null>(null);
const open = ref(false);
const busy = ref(false);
const message = ref<string | (() => string)>("");
const actionError = ref("");
function edit(entry: FundEntry) {
  selected.value = entry;
  open.value = true;
}
function next() {
  if (!data.value?.next_cursor) return;
  previous.value.push(cursor.value);
  cursor.value = data.value.next_cursor;
}
function back() {
  cursor.value = previous.value.pop() || "";
}
function saved() {
  open.value = false;
  message.value = () => t('fundingSummary.fundClassificationSavedTotalsHaveBeenUpdated');
}
async function sync() {
  busy.value = true;
  actionError.value = "";
  message.value = "";
  try {
    await api(`/accounts/${id.value}/history/sync`, "POST");
    message.value = () => t('fundingSummary.historyBackfillAndRecheckQueuedFundSnapshotsTake');
    await refreshAccounts();
  } catch (e) {
    actionError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="panel funding-panel" :aria-label="t('fundingSummary.totalDepositsWithdrawalsAndFundEntries')">
    <div class="panel-heading">
      <div>
        <h2>{{ t('fundingSummary.totalDepositsWithdrawals') }}</h2>
        <p class="muted small">
          {{ t('fundingSummary.externalFundEntriesDetectedAutomaticallyAndTotaledIn') }}
          <span
            class="badge"
            :class="
              funding.coverage_complete && !funding.stale
                ? 'badge-good'
                : 'badge-warning'
            "
            >{{ fundingLabel(funding.state)
            }}{{ funding.stale ? t('accountTable.stale') : "" }}</span
          >
        </p>
      </div>
      <button
        class="btn btn-small"
        :disabled="
          busy ||
          !account.sync_enabled ||
          funding.state === 'time_zone_unconfirmed'
        "
        @click="sync"
      >
        <RefreshCw :size="14" :class="{ spin: busy }" />{{ t('fundingSummary.backfillRecheckHistory') }}
      </button>
    </div>
    <div class="funding-metrics">
      <div>
        <div class="card-label">
          {{ t('fundingSummary.totalDeposits') }}
          <span>{{
            funding.native_currency ||
            account.snapshot?.native_currency ||
            t('account.nativeCurrency')
          }}</span>
        </div>
        <div class="metric-value num" data-fund-total="deposits">
          {{ money(funding.deposits) }}
        </div>
        <p class="metric-note">{{ t('fundingSummary.externalDepositsDetectedAutomaticallyWithManualCorrections') }}</p>
      </div>
      <div>
        <div class="card-label">
          {{ t('fundingSummary.totalWithdrawals') }}
          <span>{{
            funding.native_currency ||
            account.snapshot?.native_currency ||
            t('account.nativeCurrency')
          }}</span>
        </div>
        <div class="metric-value num" data-fund-total="withdrawals">
          {{ money(funding.withdrawals) }}
        </div>
        <p class="metric-note">{{ t('fundingSummary.externalWithdrawalsDetectedAutomaticallyAbsoluteValuesShown') }}</p>
      </div>
    </div>
    <p class="daily-note muted small">
      {{ t('fundingSummary.classificationCounts', { automatic: funding.automatic_count ?? 0, manual: funding.manual_count ?? funding.confirmed_count, unclassified: funding.unclassified_count }) }}
      <template v-if="!funding.coverage_complete"
        >{{ t('fundingSummary.historyBackfillIsIncompleteTotalsIncludeIdentifiedEntries') }}</template
      >
      <template v-if="funding.blocked_windows"
        >{{ t('fundingSummary.blockedWindows', { count: funding.blocked_windows }) }}</template
      >
      {{ t('fundingSummary.depositsWithdrawalsAndRebatesAreIdentifiedFromComments') }}
    </p>
    <p
      v-if="funding.last_run?.error_code"
      class="daily-note warning-text small"
    >
      {{
        failureLabel(funding.last_run.error_code) || t('fundingSummary.historyCollectionIsNotCompleteYet')
      }}{{ t('fundingSummary.retainingExistingHistoryAndConfirmedAmounts') }}
    </p>
    <div class="funding-toolbar">
      <h3>{{ t('fundingSummary.fundEntries') }}</h3>
      <label class="checkbox-line"
        ><input v-model="unclassified" type="checkbox" />{{ t('fundingSummary.unclassifiedOnly') }}</label
      >
    </div>
    <p v-if="error" role="alert" class="daily-note negative">
      {{ localizeMessage(error.message) }}
      <button class="text-button" @click="refetch()">{{ t('overview.retry') }}</button>
    </p>
    <p v-if="actionError" role="alert" class="daily-note negative">
      {{ localizeMessage(actionError) }}
    </p>
    <p v-if="message" role="status" class="daily-note positive">
      {{ localizeMessage(message) }}
    </p>
    <div class="table-scroll">
      <table class="data-table funds-table">
        <thead>
          <tr>
            <th>{{ t('fundingSummary.entryTicket') }}</th>
            <th>{{ t('settings.serverTime') }}</th>
            <th class="text-right">{{ t('fundingSummary.amountNativeCurrency') }}</th>
            <th>{{ t('fundingSummary.category') }}</th>
            <th>{{ t('fundingSummary.commentDetectionBasis') }}</th>
            <th>{{ t('fundingSummary.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="entry in data?.items || []" :key="entry.ticket">
            <td class="num">#{{ entry.ticket }}</td>
            <td class="small muted">{{ wallTime(entry.event_time_wall) }}</td>
            <td
              class="text-right num"
              :class="positive(entry.net_profit) ? 'positive' : 'negative'"
            >
              {{ signedMoney(entry.net_profit) }} {{ entry.native_currency }}
            </td>
            <td>
              <span
                class="badge"
                :class="
                  entry.command === 6 &&
                  (!entry.classification ||
                    entry.needs_review ||
                    entry.classification.category === 'unclassified')
                    ? 'badge-warning'
                    : 'badge-neutral'
                "
                >{{
                  entry.command === 7
                    ? t('fundingSummary.creditSeparate')
                    : entry.needs_review
                      ? t('fundingSummary.needsReconfirmation')
                      : fundCategories[
                          entry.classification?.category || "unclassified"
                        ]
                }}</span
              >
              <small
                v-if="entry.classification && !entry.needs_review"
                class="cell-note muted"
                >{{
                  entry.classification.source === "automatic"
                    ? t('settings.automaticDetection')
                    : t('fundingSummary.manualCorrection')
                }}</small
              >
            </td>
            <td class="fund-reason small muted">
              {{ entry.comment || t('fundingSummary.noComment') }}
              <small v-if="entry.classification" class="cell-note">{{
                entry.classification.source === "automatic" ? localizeMessage(entry.classification.reason) : entry.classification.reason
              }}</small>
            </td>
            <td>
              <button
                v-if="entry.command === 6"
                class="text-button"
                :aria-label="t('fundingSummary.classifyEntry', { value1: entry.ticket })"
                @click="edit(entry)"
              >
                {{ entry.classification ? t('fundingSummary.correct') : t('fundingSummary.classify') }}</button
              ><span v-else class="muted small">{{ t('fundingSummary.trackedSeparately') }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="!error && !data?.items.length" class="empty-inline">
      {{
        isPending
          ? t('fundingSummary.loadingFundEntries')
          : unclassified
            ? t('fundingSummary.noUnclassifiedEntries')
            : funding.deposits == null
              ? t('fundingSummary.waitingForServerHistoryNoFundEntriesAre')
              : t('fundingSummary.noFundEntriesInTheBackfilledPeriod')
      }}
    </p>
    <div class="panel-footer">
      <span>{{ t('fundingSummary.latestHistorySample') }} {{ date(funding.last_sync_at_ms) }}</span>
      <div class="funding-pagination">
        <button
          class="btn btn-small"
          :disabled="!previous.length || isFetching"
          @click="back"
        >
          {{ t('fundingSummary.previous') }}</button
        ><button
          class="btn btn-small"
          :disabled="!data?.next_cursor || isFetching"
          @click="next"
        >
          {{ t('fundingSummary.next') }}
        </button>
      </div>
    </div>
    <Modal
      v-model:open="open"
      :title="t('fundingSummary.correctFundClassification')"
      :description="t('fundingSummary.correctOnlyEntriesIdentifiedIncorrectlyOrLeftUnclassified')"
      ><FundClassificationForm
        v-if="selected"
        :key="`${selected.ticket}-${selected.revision}`"
        :account-id="account.id"
        :entry="selected"
        @saved="saved"
    /></Modal>
  </section>
</template>
