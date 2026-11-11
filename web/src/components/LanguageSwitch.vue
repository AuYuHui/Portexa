<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Languages } from "@lucide/vue";
import { setLocale } from "../i18n";

const { t, locale } = useI18n();
</script>

<template>
  <label class="language-switch">
    <Languages :size="16" aria-hidden="true" />
    <select
      :value="locale"
      :aria-label="t('language.select')"
      @change="setLocale(($event.target as HTMLSelectElement).value)"
    >
      <option value="zh-CN" lang="zh-CN">简体中文</option>
      <option value="en" lang="en">English</option>
    </select>
  </label>
</template>

<style scoped>
.language-switch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  color: var(--color-text-secondary);
  font-size: 12px;
}
.language-switch select {
  width: auto;
  min-width: 0;
  padding: 6px 3px;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: pointer;
}
.language-switch:focus-within {
  outline: 2px solid var(--color-brand-600);
  outline-offset: 3px;
  border-radius: 4px;
}
</style>
