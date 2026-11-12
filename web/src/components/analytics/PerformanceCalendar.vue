<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { ChevronLeft, ChevronRight } from "@lucide/vue";
import { signedMoney, positive } from "../../format";
import type { PerformanceDay, PerformanceResult } from "../../types/analytics";
const props = defineProps<{
  days: PerformanceDay[];
  months: PerformanceResult["months"];
  currency: string;
}>();
const emit = defineEmits<{ select: [day: PerformanceDay] }>();
const { t } = useI18n();
const months = computed(() => [
  ...new Set(props.days.map((day) => day.date.slice(0, 7))),
]);
const month = ref("");
watch(
  months,
  (values) => {
    if (!values.includes(month.value)) month.value = values.at(-1) || "";
  },
  { immediate: true },
);
const monthIndex = computed(() => months.value.indexOf(month.value));
const monthValue = computed(() =>
  props.months.find((item) => item.month === month.value),
);
const weekdays = computed(() => [
  t("analytics.mon"),
  t("analytics.tue"),
  t("analytics.wed"),
  t("analytics.thu"),
  t("analytics.fri"),
  t("analytics.sat"),
  t("analytics.sun"),
]);
const cells = computed(() => {
  if (!month.value) return [];
  const start = new Date(`${month.value}-01T00:00:00Z`);
  const padding = (start.getUTCDay() + 6) % 7;
  const count = new Date(
    Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + 1, 0),
  ).getUTCDate();
  const byDate = new Map(props.days.map((day) => [day.date, day]));
  return Array.from(
    { length: Math.ceil((padding + count) / 7) * 7 },
    (_, index) => {
      const number = index - padding + 1;
      if (number < 1 || number > count)
        return { key: `pad-${index}`, number: 0, day: undefined };
      const key = `${month.value}-${String(number).padStart(2, "0")}`;
      return { key, number, day: byDate.get(key) };
    },
  );
});
function tone(day?: PerformanceDay) {
  const value = day?.net_profit.value;
  if (value == null) return "is-missing";
  if (/^-?0(?:\.0+)?$/.test(value)) return "is-flat";
  return value.startsWith("-") ? "is-negative" : "is-positive";
}
</script>

<template>
  <section
    class="panel performance-calendar"
    :aria-label="t('analytics.calendar')"
  >
    <div class="performance-panel-heading">
      <div>
        <span class="performance-section-no">02 /</span>
        <h2>{{ t("analytics.calendar") }}</h2>
        <p>{{ t("analytics.calendarHint") }}</p>
      </div>
      <div class="performance-month-switch">
        <button
          class="icon-button"
          :disabled="monthIndex <= 0"
          :aria-label="t('analytics.previousMonth')"
          @click="month = months[monthIndex - 1]!"
        >
          <ChevronLeft :size="17" />
        </button>
        <span class="num">{{ month || "—" }}</span
        ><button
          class="icon-button"
          :disabled="monthIndex < 0 || monthIndex >= months.length - 1"
          :aria-label="t('analytics.nextMonth')"
          @click="month = months[monthIndex + 1]!"
        >
          <ChevronRight :size="17" />
        </button>
      </div>
    </div>
    <p class="performance-month-total">
      {{ t("analytics.monthProfit") }}
      <strong
        class="num"
        :class="{
          positive: monthValue?.net_profit.value && positive(monthValue.net_profit.value),
          negative: monthValue?.net_profit.value?.startsWith('-'),
        }"
        >{{ signedMoney(monthValue?.net_profit.value) }} {{ currency }}</strong
      ><span v-if="monthValue?.partial">
        · {{ t("analytics.selectedMonthRange") }}</span
      >
    </p>
    <div class="performance-calendar-grid">
      <span
        v-for="(weekday, index) in weekdays"
        :key="index"
        class="performance-weekday"
        >{{ weekday }}</span
      >
      <div
        v-for="cell in cells"
        :key="cell.key"
        class="performance-calendar-slot"
      >
        <button
          v-if="cell.number"
          type="button"
          class="performance-calendar-day"
          :class="tone(cell.day)"
          :disabled="!cell.day"
          :aria-label="
            cell.day
              ? `${cell.key} ${signedMoney(cell.day.net_profit.value)} ${currency}`
              : cell.key
          "
          :data-calendar-date="cell.key"
          @click="cell.day && emit('select', cell.day)"
        >
          <span class="performance-day-number num"
            >{{ cell.number
            }}<span
              v-if="cell.day?.partial"
              class="performance-day-partial"
              :title="t('analytics.partialDay')"
              >*</span
            ></span
          >
          <strong class="num">{{
            cell.day ? signedMoney(cell.day.net_profit.value) : "—"
          }}</strong>
          <small v-if="cell.day">{{
            cell.day.trades === null
              ? t("analytics.tradesUnknown")
              : t("analytics.tradesCount", { count: cell.day.trades })
          }}</small>
        </button>
      </div>
    </div>
    <div class="performance-calendar-legend">
      <span><i class="is-negative" />{{ t("analytics.loss") }}</span
      ><span><i class="is-flat" />{{ t("analytics.flat") }}</span
      ><span><i class="is-positive" />{{ t("analytics.profit") }}</span
      ><span><i class="is-missing" />{{ t("analytics.insufficient") }}</span
      ><small>{{ currency }} · {{ t("analytics.partialDay") }}</small>
    </div>
  </section>
</template>
