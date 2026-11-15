<script setup lang="ts">
import { computed } from "vue";
import { runLabel } from "../format";
import { useI18n } from "vue-i18n";
const { t } = useI18n();
const props = defineProps<{ state: string; stale?: boolean; rest?: boolean }>();
const tone = computed(() =>
  props.stale || props.state === "failed"
    ? "warning"
    : props.state === "succeeded"
      ? "good"
      : props.state === "running"
        ? "info"
        : "neutral",
);
</script>
<template>
  <span class="badge" :class="`badge-${tone}`"
    ><span class="status-dot" />{{ rest && !stale && state === 'succeeded' ? t('collectionSettings.marketRest') : runLabel(state, stale) }}</span
  >
</template>
