<script setup lang="ts">
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
} from "@lucide/vue";
import { api } from "../api";
import type { Overview } from "../types";
import { money, date, positive } from "../format";
import AccountTable from "../components/AccountTable.vue";
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
        <h1>账户总览<span class="title-dot">.</span></h1>
        <p class="muted">所有账户的资金、持仓与同步状态，尽在此处。</p>
      </div>
      <div class="page-actions">
        <button
          class="btn"
          :disabled="isFetching"
          aria-label="刷新看板"
          @click="refetch()"
        >
          <RefreshCw :size="16" :class="{ spin: isFetching }" /><span
            class="desktop-only"
            >刷新看板</span
          ></button
        ><RouterLink class="btn btn-primary" to="/settings?add=account"
          ><Plus :size="17" />接入账户</RouterLink
        >
      </div>
    </div>
    <p v-if="error" class="notice notice-error" role="alert">
      {{ error.message }}
      <button class="text-button" @click="refetch()">重试</button>
    </p>
    <div v-if="isPending" class="skeleton-grid" aria-label="正在加载">
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
            ? "最近备份未完成，请检查服务器存储与备份目录。"
            : "WAL 超过 64 MiB 且仍有待处理页，请检查长时间读取或存储状态。"
        }}
      </div>
      <div v-if="!data.key_ready" class="notice notice-warning">
        <AlertTriangle
          :size="18"
        />凭据密钥不可用。已有数据可以查看，采集已停止，请在服务器恢复匹配密钥。
      </div>
      <section class="summary-grid" aria-label="资金概览">
        <article class="summary-card summary-featured">
          <div class="card-label">最近已知净值小计 <Wallet :size="19" /></div>
          <div class="hero-value num">
            <span>USD</span>{{ known ? money(total.equity) : "—" }}
          </div>
          <div class="featured-footer">
            <span class="badge badge-on-dark"
              ><span class="status-dot" />{{
                total.complete && !data.excluded_usd_count && !data.stale_count
                  ? "采样完整"
                  : "范围受限"
              }}</span
            ><span
              >{{ total.included_accounts }} /
              {{ data.account_count }} 个账户已计入</span
            >
          </div>
        </article>
        <article class="summary-card">
          <div class="card-label">已知余额 <CircleDollarSign :size="19" /></div>
          <div class="metric-value num">
            {{ known ? money(total.balance) : "—" }}<small>USD</small>
          </div>
          <p class="metric-note">
            信用金另计 {{ known ? money(total.credit) : "—" }} USD
          </p>
        </article>
        <article class="summary-card">
          <div class="card-label">浮动净盈亏 <Activity :size="19" /></div>
          <div
            class="metric-value num"
            :class="{
              positive: known && positive(total.floating_net_profit),
              negative: known && !positive(total.floating_net_profit),
            }"
          >
            {{ known ? money(total.floating_net_profit) : "—"
            }}<small>USD</small>
          </div>
          <p class="metric-note">含当前持仓的佣金、隔夜费及税费</p>
        </article>
        <article class="summary-card">
          <div class="card-label">账户状态 <Layers3 :size="19" /></div>
          <div class="metric-value num">
            {{ data.account_count }}<small>个账户</small>
          </div>
          <p class="metric-note">
            <span class="positive">{{ healthy }} 个正常</span
            ><span class="divider-dot">·</span
            >{{ data.accounts.filter((a) => !a.sync_enabled).length }} 个暂停
          </p>
        </article>
      </section>
      <div class="sampling-note">
        <Info :size="15" />
        <p>
          美元小计仅纳入已确认的 USD / USC，账户净值含信用金。{{
            data.missing_count ? `${data.missing_count} 个账户缺少快照；` : ""
          }}{{
            data.excluded_usd_count
              ? `${data.excluded_usd_count} 个账户未纳入换算；`
              : ""
          }}{{
            data.stale_count ? `${data.stale_count} 个账户数据过期；` : ""
          }}各账户采样并非同一时刻。
        </p>
        <span class="num"
          >采样范围 {{ date(total.observed_from_ms) }} —
          {{ date(total.observed_to_ms) }}</span
        >
      </div>
      <section class="panel account-panel">
        <div class="panel-heading">
          <div>
            <h2>
              全部账户 <span class="count-pill">{{ data.account_count }}</span>
            </h2>
            <p class="muted small">
              金额按原币展示，最近有效快照在失败后继续保留。
            </p>
          </div>
          <label class="search-box"
            ><Search :size="16" /><input
              v-model="search"
              aria-label="搜索账户"
              placeholder="搜索名称、账号或经纪商"
          /></label>
        </div>
        <AccountTable :accounts="data.accounts" :search="search" />
        <div class="panel-footer">
          <span
            ><span class="status-dot" />后台每 60 秒计划采集，页面每 10
            秒刷新</span
          ><span>看板更新 {{ date(data.as_of_ms) }}</span>
        </div>
      </section>
      <section class="bottom-grid">
        <div class="panel currency-panel">
          <div class="panel-heading">
            <div>
              <h2>原币资金</h2>
              <p class="muted small">独立展示，不将其他币种混入美元小计。</p>
            </div>
            <span class="icon-box subtle"><CircleDollarSign :size="20" /></span>
          </div>
          <div v-if="!data.currency_totals.length" class="empty-inline">
            完成币种确认和首次采集后，在这里查看原币资金。
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
                  ? "美元美分"
                  : item.currency === "USD"
                    ? "美元"
                    : "原币账户"
              }}</strong
              ><small
                >{{ item.included_accounts }} /
                {{ item.expected_accounts }} 个账户有快照
                {{
                  item.stale_accounts ? `· ${item.stale_accounts} 个过期` : ""
                }}</small
              >
            </div>
            <div class="text-right num">
              <strong>{{
                item.included_accounts ? money(item.equity) : "—"
              }}</strong
              ><small>最近已知净值</small>
            </div>
          </div>
        </div>
        <div class="scope-card">
          <span class="eyebrow">CLEAR DATA, CLEAR CONTEXT</span>
          <h2>看清数字，也看清边界。</h2>
          <p>
            未确认的单位、缺失与过期数据都会保留标记。这里的资金是最近已知估值，不代表同一时刻的组合估值或可提现金额。
          </p>
          <RouterLink to="/settings"
            >管理接入与单位确认 <ArrowUpRight :size="16"
          /></RouterLink>
          <div class="scope-decoration" aria-hidden="true">P<span>.</span></div>
        </div>
      </section>
    </template>
  </div>
</template>
