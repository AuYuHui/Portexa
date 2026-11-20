<script setup lang="ts">
import { ArrowUpRight } from "@lucide/vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
</script>

<template>
  <a
    class="repository-link"
    href="https://github.com/AuYuHui/Portexa"
    target="_blank"
    rel="noreferrer noopener"
    :aria-label="t('app.githubRepository')"
  >
    GitHub <ArrowUpRight :size="14" aria-hidden="true" />
  </a>
</template>

<style scoped>
.repository-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 32px;
  padding-inline: 4px;
  white-space: nowrap;
  color: var(--color-text-secondary);
  font-size: 12px;
  font-weight: 600;
}
.repository-link:hover {
  color: var(--color-brand-600);
  text-decoration: underline;
}
</style>
