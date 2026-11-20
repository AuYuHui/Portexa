<script setup lang="ts">
import { ref, computed } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { Cpu, ChevronDown, ChevronUp, Layers, Target } from "@lucide/vue";
import { api } from "../api";
import { money, signedMoney } from "../format";
import type { StrategyView } from "../types";

const props = defineProps<{ accountId: string }>();

const { data: strategy, isPending } = useQuery({
  queryKey: ["strategy", computed(() => props.accountId)],
  queryFn: () => api<StrategyView>(`/accounts/${props.accountId}/strategy`),
  refetchInterval: 10_000,
});

const expanded = ref(false);

const dominantText = computed(() => {
  if (!strategy.value?.active_grid.has_active) return "暂无持仓";
  const grid = strategy.value.active_grid;
  if (grid.dominant_side === "SELL") {
    return `主空头 (${grid.sell_count}空 / ${grid.buy_count}多)`;
  } else if (grid.dominant_side === "BUY") {
    return `主多头 (${grid.buy_count}多 / ${grid.sell_count}空)`;
  }
  return `双向持仓 (${grid.layers}笔)`;
});
</script>

<template>
  <div v-if="strategy" class="compact-strategy-wrapper">
    <!-- 极简高密主控栏 (高度仅 ~38px，不占版面) -->
    <div class="compact-bar" @click="expanded = !expanded">
      <div class="compact-left">
        <Cpu :size="15" class="icon-brand" />
        <span class="bar-title">策略诊断:</span>
        <span class="badge badge-good compact-badge">{{ strategy.archetype }}</span>
        <span class="sep">/</span>
        <span class="item">一篮子胜率: <strong class="positive">{{ strategy.grid_history.cycle_win_rate.toFixed(1) }}%</strong></span>
        <span class="sep">/</span>
        <span class="item">累计净利: <strong class="positive">+{{ money(strategy.metrics.net_profit) }} {{ strategy.currency }}</strong></span>
        <template v-if="strategy.active_grid.has_active">
          <span class="sep">/</span>
          <span class="item">
            当前持仓: <strong>{{ strategy.active_grid.layers }}层 ({{ strategy.active_grid.total_lots }}手)</strong>
            · 成本偏离:
            <strong :class="parseFloat(strategy.active_grid.dist_to_cost) >= 0 ? 'positive' : 'negative'">
              {{ parseFloat(strategy.active_grid.dist_to_cost) >= 0 ? '+' : '' }}{{ strategy.active_grid.dist_to_cost }}$
            </strong>
          </span>
        </template>
      </div>

      <button class="toggle-link" type="button" @click.stop="expanded = !expanded">
        <span>{{ expanded ? "收起详情" : "展开量化详情" }}</span>
        <ChevronUp v-if="expanded" :size="13" />
        <ChevronDown v-else :size="13" />
      </button>
    </div>

    <!-- 折叠展开的紧凑详细面板 (只在展开时显示，结构高度精简) -->
    <div v-if="expanded" class="compact-details-pane">
      <div class="details-cols">
        <!-- 左侧：策略白话逻辑与关键指标 -->
        <div class="col-left">
          <div class="logic-summary">
            💡 <strong>机制解读：</strong>
            首单微利快速止盈；逆势时每跌/涨 ~0.80$ 补仓一层拉低均价；微幅反弹时<strong>一篮子全部盈利平仓</strong>，平仓后 1 秒内重启底仓。
          </div>
          <div class="mini-metrics-row">
            <div class="mini-metric">
              <span class="m-label">盈亏比 (PF)</span>
              <span class="m-val num">{{ strategy.metrics.profit_factor.toFixed(2) }}</span>
            </div>
            <div class="mini-metric">
              <span class="m-label">一篮子平仓比</span>
              <span class="m-val num">{{ strategy.basket_exit_ratio.toFixed(1) }}%</span>
            </div>
            <div class="mini-metric">
              <span class="m-label">累计完成周期</span>
              <span class="m-val num">{{ strategy.grid_history.total_cycles }} 轮</span>
            </div>
            <div class="mini-metric">
              <span class="m-label">单轮平均收益</span>
              <span class="m-val num positive">+{{ money(strategy.grid_history.avg_cycle_profit) }}</span>
            </div>
          </div>
        </div>

        <!-- 右侧：当前持仓态势 & 出场层数分布 -->
        <div class="col-right">
          <!-- 当前网格态势 -->
          <div v-if="strategy.active_grid.has_active" class="active-grid-mini">
            <div class="mini-head">
              <Layers :size="14" class="icon-brand" />
              <strong>当前网格态势: {{ strategy.active_grid.symbol }}</strong>
              <span class="muted small">· {{ dominantText }}</span>
            </div>
            <div class="mini-grid-stats">
              <span>加权成本: <strong class="num">{{ strategy.active_grid.avg_price }}</strong></span>
              <span>当前市价: <strong class="num">{{ strategy.active_grid.current_price }}</strong></span>
              <span>
                距回本:
                <strong class="num" :class="parseFloat(strategy.active_grid.dist_to_cost) >= 0 ? 'positive' : 'negative'">
                  {{ parseFloat(strategy.active_grid.dist_to_cost) >= 0 ? '盈利 +' : '逆势 ' }}{{ strategy.active_grid.dist_to_cost }}$
                </strong>
              </span>
              <span>
                浮盈亏:
                <strong class="num" :class="parseFloat(strategy.active_grid.total_profit) >= 0 ? 'positive' : 'negative'">
                  {{ signedMoney(strategy.active_grid.total_profit) }} {{ strategy.currency }}
                </strong>
              </span>
            </div>
          </div>

          <!-- 解套层数极简迷你条 -->
          <div class="layer-dist-mini">
            <div class="mini-head">
              <Target :size="14" class="icon-brand" />
              <strong>解套出场层数分布 (前5名)</strong>
            </div>
            <div class="mini-layer-bars">
              <div
                v-for="item in strategy.grid_history.layer_distribution.slice(0, 5)"
                :key="item.layer"
                class="bar-row"
              >
                <span class="bar-title">第{{ item.layer }}层{{ item.layer === 1 ? ' (秒止盈)' : '' }}</span>
                <div class="bar-bg">
                  <div class="bar-fill" :style="{ width: `${item.percent}%` }"></div>
                </div>
                <span class="bar-num num">{{ item.percent.toFixed(1) }}%</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div v-else-if="isPending" class="compact-loading">
    <Cpu :size="13" class="icon-brand" /> 正在加载策略诊断...
  </div>
</template>

<style scoped>
.compact-strategy-wrapper {
  margin: 12px 0 16px 0;
  border: 1px solid var(--color-line, #e2e8f0);
  border-radius: 8px;
  background: var(--color-surface, #ffffff);
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.03);
  overflow: hidden;
}

/* 顶部紧凑栏：高度仅 ~38px */
.compact-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 14px;
  background: #f8fafc;
  cursor: pointer;
  user-select: none;
  font-size: 0.8rem;
  transition: background 0.15s ease;
}

.compact-bar:hover {
  background: #f1f5f9;
}

.compact-left {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--color-text-secondary, #334155);
}

.bar-title {
  font-weight: 600;
  color: var(--color-ink, #0f172a);
}

.compact-badge {
  font-size: 0.7rem;
  padding: 1px 6px;
  border-radius: 4px;
}

.sep {
  color: #cbd5e1;
}

.item {
  font-size: 0.8rem;
}

.toggle-link {
  display: flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: transparent;
  color: var(--color-brand-600, #059669);
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  padding: 2px 6px;
}

.toggle-link:hover {
  text-decoration: underline;
}

/* 展开区域：紧凑两栏排版 */
.compact-details-pane {
  padding: 12px 14px;
  border-top: 1px solid var(--color-line-subtle, #f1f5f9);
  background: #ffffff;
  font-size: 0.8rem;
}

.details-cols {
  display: grid;
  grid-template-columns: 46% 54%;
  gap: 16px;
}

@media (max-width: 960px) {
  .details-cols {
    grid-template-columns: 1fr;
  }
}

.col-left {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.logic-summary {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 8px 10px;
  line-height: 1.5;
  color: var(--color-text-secondary, #334155);
  font-size: 0.75rem;
}

.mini-metrics-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
}

.mini-metric {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 6px 8px;
  display: flex;
  flex-direction: column;
}

.m-label {
  font-size: 0.68rem;
  color: var(--color-muted, #64748b);
  margin-bottom: 2px;
}

.m-val {
  font-size: 0.95rem;
  font-weight: 700;
}

.col-right {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.active-grid-mini,
.layer-dist-mini {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 8px 10px;
}

.mini-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.75rem;
  margin-bottom: 6px;
}

.mini-grid-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 0.75rem;
}

.mini-layer-bars {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.bar-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.72rem;
}

.bar-title {
  width: 75px;
  color: var(--color-muted, #64748b);
}

.bar-bg {
  flex: 1;
  height: 5px;
  background: #e2e8f0;
  border-radius: 3px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  background: #10b981;
  border-radius: 3px;
}

.bar-num {
  width: 40px;
  text-align: right;
  font-weight: 600;
}

.icon-brand {
  color: #059669;
}

.positive {
  color: #059669 !important;
}

.negative {
  color: #dc2626 !important;
}

.compact-loading {
  padding: 6px 12px;
  font-size: 0.75rem;
  color: var(--color-muted, #64748b);
  margin-bottom: 8px;
}
</style>
