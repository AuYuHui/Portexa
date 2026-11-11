<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { TrendMetric, TrendRange, TrendUnit } from "../../types/trends";

const props = defineProps<{
  range: TrendRange;
  metric: TrendMetric;
  unit: TrendUnit;
  account: boolean;
  canConvertUSD: boolean;
  customFrom: string;
  customTo: string;
}>();
const emit = defineEmits<{
  range: [value: TrendRange];
  metric: [value: TrendMetric];
  unit: [value: TrendUnit];
  "update:customFrom": [value: string];
  "update:customTo": [value: string];
  apply: [];
}>();
const { t } = useI18n();
const presets: { value: TrendRange; label: string }[] = [
  { value: "24h", label: "24H" }, { value: "7d", label: "7D" },
  { value: "30d", label: "30D" }, { value: "90d", label: "90D" },
];
</script>

<template>
  <div class="trend-toolbar">
    <div class="trend-segmented" role="group" :aria-label="t('trend.metric')">
      <button type="button" :aria-pressed="metric === 'equity'" @click="emit('metric', 'equity')">{{ t('account.equity') }}</button>
      <button type="button" :aria-pressed="metric === 'balance'" @click="emit('metric', 'balance')">{{ t('account.balance') }}</button>
      <button type="button" :aria-pressed="metric === 'both'" @click="emit('metric', 'both')">{{ t('trend.both') }}</button>
    </div>
    <div class="trend-range-controls">
      <div class="trend-segmented" role="group" :aria-label="t('trend.range')">
        <button v-for="preset in presets" :key="preset.value" type="button" :aria-pressed="range === preset.value" @click="emit('range', preset.value)">{{ preset.label }}</button>
        <button type="button" :aria-pressed="range === 'custom'" @click="emit('range', 'custom')">{{ t('trend.custom') }}</button>
      </div>
      <select v-if="account" class="trend-unit-select" :value="unit" :aria-label="t('trend.displayUnit')" @change="emit('unit', ($event.target as HTMLSelectElement).value as TrendUnit)">
        <option value="native">{{ t('trend.nativeUnit') }}</option>
        <option value="USD" :disabled="!canConvertUSD">{{ t('trend.convertedUSD') }}</option>
      </select>
    </div>
    <form v-if="props.range === 'custom'" class="trend-custom" @submit.prevent="emit('apply')">
      <label>{{ t('trend.start') }}<input type="datetime-local" :value="customFrom" required @input="emit('update:customFrom', ($event.target as HTMLInputElement).value)" /></label>
      <label>{{ t('trend.end') }}<input type="datetime-local" :value="customTo" required @input="emit('update:customTo', ($event.target as HTMLInputElement).value)" /></label>
      <button class="btn btn-small" type="submit">{{ t('trend.apply') }}</button>
      <span class="muted small">{{ t('trend.beijing') }}</span>
    </form>
  </div>
</template>
