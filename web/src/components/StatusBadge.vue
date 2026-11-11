<script setup lang="ts">
import { computed } from "vue";
import { runLabel } from "../format";
const props = defineProps<{ state: string; stale?: boolean }>();
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
    ><span class="status-dot" />{{ runLabel(state, stale) }}</span
  >
</template>
