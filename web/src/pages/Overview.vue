<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { RouterLink } from "vue-router";
import {
  Plus,
  RefreshCw,
  Search,
  ArrowUpRight,
  Wallet,
  Activity,
  CircleDollarSign,
  Layers3,
  Info,
  AlertTriangle,
  ShieldCheck,
} from "@lucide/vue";
import { api } from "../api";
import type { Overview } from "../types";
import { money, signedMoney, date, positive } from "../format";
import AccountTable from "../components/AccountTable.vue";

const { t } = useI18n();
const search = ref("");
const { data, error, isPending, isFetching, refetch } = useQuery({
  queryKey: ["overview"],
  queryFn: () => api<Overview>("/overview"),
  refetchInterval: 10_000,
});
const total = computed(() => data.value?.usd_total);
const known = computed(() => (total.value?.included_accounts || 0) > 0);
const healthy = computed(
  () =>
    data.value?.accounts.filter((a) => a.sync_state === "succeeded" && !a.stale)
      .length || 0,
);
</script>

<template>
  <div class="page-content">
    <div class="page-title">
      <div>
        <span class="eyebrow">PORTFOLIO OVERVIEW</span>
        <h1>{{ t('overview.accountOverview') }}<span class="title-dot">.</span></h1>
        <p class="muted">{{ t('overview.fundsPositionsAndSyncStatusForEveryAccount') }}</p>
      </div>
      <div class="page-actions">
        <button
          class="btn"
          :disabled="isFetching"
          :aria-label="t('overview.refreshDashboard')"
          @click="refetch()"
        >
          <RefreshCw :size="16" :class="{ spin: isFetching }" /><span
            class="desktop-only"
            >{{ t('overview.refreshDashboard') }}</span
          ></button
        ><RouterLink class="btn btn-primary" to="/settings?add=account"
          ><Plus :size="17" />{{ t('overview.connectAccount') }}</RouterLink
        >
      </div>
    </div>
    <p v-if="error" class="notice notice-error" role="alert">
      {{ localizeMessage(error.message) }}
      <button class="text-button" @click="refetch()">{{ t('overview.retry') }}</button>
    </p>
    <div v-if="isPending" class="skeleton-grid" :aria-label="t('overview.loading')">
      <div v-for="n in 4" :key="n" class="skeleton" />
    </div>
    <template v-if="data && total">
      <div
        v-if="data.maintenance.backup_failed || data.maintenance.wal_warning"
        class="notice notice-warning"
        role="status"
      >
        <AlertTriangle :size="18" />{{
          data.maintenance.backup_failed
            ? t('overview.theLatestBackupDidNotCompleteCheckServer')
            : t('overview.theWalExceedsMibWithPagesStillPending')
        }}
      </div>
      <div v-if="!data.key_ready" class="notice notice-warning">
        <AlertTriangle
          :size="18"
        />{{ t('overview.theCredentialKeyIsUnavailableExistingDataRemains') }}
      </div>
      <section class="summary-grid" :aria-label="t('overview.fundsOverview')">
        <article class="summary-card summary-featured">
          <div class="card-label">{{ t('overview.latestKnownEquitySubtotal') }} <Wallet :size="19" /></div>
          <div class="hero-value num">
            <span>USD</span>{{ known ? money(total.equity) : "—" }}
          </div>
          <div class="featured-footer">
            <span class="badge badge-on-dark"
              ><span class="status-dot" />{{
                total.complete && !data.excluded_usd_count && !data.stale_count
                  ? t('overview.completeSampling')
                  : t('overview.limitedCoverage')
              }}</span
            ><span
              >{{ t('overview.includedAccounts', { included: total.included_accounts, total: data.account_count }) }}</span
            >
          </div>
        </article>
        <article class="summary-card">
          <div class="card-label">{{ t('overview.knownBalance') }} <CircleDollarSign :size="19" /></div>
          <div class="metric-value num">
            {{ known ? money(total.balance) : "—" }}<small>USD</small>
          </div>
          <p class="metric-note">
            {{ t('overview.creditAmount', { amount: known ? money(total.credit) : '—' }) }}
          </p>
        </article>
        <article class="summary-card">
          <div class="card-label">{{ t('account.floatingNetPL') }} <Activity :size="19" /></div>
          <div
            class="metric-value num"
            :class="{
              positive: known && positive(total.floating_net_profit),
              negative: known && !positive(total.floating_net_profit),
            }"
          >
            {{ known ? signedMoney(total.floating_net_profit) : "—"
            }}<small>USD</small>
          </div>
          <p class="metric-note">{{ t('overview.includesCommissionSwapAndTaxesOnCurrentPositions') }}</p>
        </article>
        <article class="summary-card">
          <div class="card-label">{{ t('overview.accountStatus') }} <Layers3 :size="19" /></div>
          <div class="metric-value num">
            {{ data.account_count }}<small>{{ t('overview.accounts') }}</small>
          </div>
          <p class="metric-note">
            <span class="positive">{{ healthy }} {{ t('overview.healthy') }}</span
            ><span class="divider-dot">·</span
            >{{ data.accounts.filter((a) => !a.sync_enabled).length }} {{ t('overview.paused') }}
          </p>
        </article>
      </section>
      <div class="sampling-note">
        <Info :size="15" />
        <p>
          {{ t('overview.theUsdSubtotalIncludesOnlyConfirmedUsdUsc') }}{{
            data.missing_count ? t('overview.accountsMissingSnapshots', { value1: data.missing_count }) : ""
          }}{{
            data.excluded_usd_count
              ? t('overview.accountsExcludedFromConversion', { value1: data.excluded_usd_count })
              : ""
          }}{{
            data.stale_count ? t('overview.accountsWithStaleData', { value1: data.stale_count }) : ""
          }}{{ t('overview.accountsAreSampledAtDifferentTimes') }}
        </p>
        <span class="num"
          >{{ t('overview.samplingWindow') }} {{ date(total.observed_from_ms) }} —
          {{ date(total.observed_to_ms) }}</span
        >
      </div>
      <section class="panel account-panel">
        <div class="panel-heading">
          <div>
            <h2>
              {{ t('account.allAccounts') }} <span class="count-pill">{{ data.account_count }}</span>
            </h2>
            <p class="muted small">
              {{ t('overview.amountsUseNativeCurrencyTheLastValidSnapshot') }}
            </p>
          </div>
          <label class="search-box"
            ><Search :size="16" /><input
              v-model="search"
              :aria-label="t('overview.searchAccounts')"
              :placeholder="t('overview.searchByNameLoginOrBroker')"
          /></label>
        </div>
        <AccountTable :accounts="data.accounts" :search="search" />
        <div class="panel-footer">
          <span
            ><span class="status-dot" />{{ t('overview.collectionSchedule', { seconds: data.collection_settings?.snapshot_interval_seconds ?? 60 }) }}</span
          ><span>{{ t('overview.dashboardUpdated') }} {{ date(data.as_of_ms) }}</span>
        </div>
      </section>
      <section class="bottom-grid">
        <div class="panel currency-panel">
          <div class="panel-heading">
            <div>
              <h2>{{ t('overview.fundsByCurrency') }}</h2>
              <p class="muted small">{{ t('overview.equityGroupedByNativeCurrencyUscRetainsIts') }}</p>
            </div>
            <span class="icon-box subtle"><CircleDollarSign :size="20" /></span>
          </div>
          <div v-if="!data.currency_totals.length" class="empty-inline">
            {{ t('overview.nativeCurrencyEquityWillAppearHereAfterThe') }}
          </div>
          <div
            v-for="item in data.currency_totals"
            :key="item.currency"
            class="currency-row"
          >
            <span class="currency-token">{{ item.currency }}</span>
            <div>
              <strong>{{
                item.currency === "USC"
                  ? t('overview.usDollarCents')
                  : item.currency === "USD"
                    ? t('overview.usDollars')
                    : t('overview.nativeCurrencyAccount')
              }}</strong
              ><small
                >{{ t('overview.accountSnapshotCoverage', { included: item.included_accounts, total: item.expected_accounts }) }}
                {{
                  item.stale_accounts ? t('overview.stale', { value1: item.stale_accounts }) : ""
                }}</small
              >
            </div>
            <div class="text-right num">
              <strong>{{
                item.included_accounts ? money(item.equity) : "—"
              }} {{ item.currency }}</strong
              ><small>{{ t('overview.latestKnownEquityUnconverted') }}</small>
            </div>
          </div>
        </div>
        <div class="panel scope-panel">
          <div class="panel-heading">
            <div>
              <h2>{{ t('overview.valuationAndSamplingScope') }}</h2>
              <p class="muted small">{{ t('overview.understandHowTheFiguresAreCalculatedAndWhen') }}</p>
            </div>
            <span class="icon-box subtle"><ShieldCheck :size="20" /></span>
          </div>
          <div class="scope-body">
            <div class="scope-item">
              <strong>{{ t('overview.unconfirmedUnitsStayOutsideTheSubtotal') }}</strong>
              <p>{{ t('overview.onlyConfirmedUsdUscEntersTheUsdSubtotal') }}</p>
            </div>
            <div class="scope-item">
              <strong>{{ t('overview.eachSnapshotKeepsItsOwnTimestamp') }}</strong>
              <p>{{ t('overview.accountsAreCollectedAtDifferentTimesAndReflect') }}</p>
            </div>
            <div class="scope-footer">
              <RouterLink class="text-button" to="/settings">
                {{ t('overview.manageConnectionsAndConfirmUnits') }} <ArrowUpRight :size="15" />
              </RouterLink>
            </div>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>
