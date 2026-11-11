<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { Clock, RefreshCw, CheckCircle2 } from "@lucide/vue";
import { api } from "../api";
import { queryClient, refreshAccounts } from "../state";
import type { CollectionSettings } from "../types";

const { t } = useI18n();

const intervals = [30, 60, 120, 300];
const { data, error, isPending, refetch } = useQuery({
  queryKey: ["collection-settings"],
  queryFn: ({ signal }) => api<CollectionSettings>("/settings/collection", "GET", undefined, signal),
  refetchInterval: 10_000,
});
const draft = ref<number>();
const busy = ref(false);
const saved = ref(false);
const saveError = ref("");
const interval = computed({
  get: () => draft.value ?? data.value?.snapshot_interval_seconds ?? 60,
  set: (value: number) => {
    draft.value = value;
    saved.value = false;
    saveError.value = "";
  },
});
const changed = computed(() => data.value && interval.value !== data.value.snapshot_interval_seconds);

async function save() {
  if (!changed.value || busy.value) return;
  busy.value = true;
  saved.value = false;
  saveError.value = "";
  try {
    await queryClient.cancelQueries({ queryKey: ["collection-settings"] });
    const result = await api<CollectionSettings>("/settings/collection", "PUT", {
      snapshot_interval_seconds: interval.value,
    });
    queryClient.setQueryData(["collection-settings"], result);
    draft.value = undefined;
    saved.value = true;
    await refreshAccounts();
  } catch (e) {
    saveError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <section class="panel collection-settings" aria-labelledby="collection-heading">
    <div class="panel-heading">
      <div>
        <h2 id="collection-heading">{{ t('collectionSettings.collectionSettings') }}</h2>
        <p class="muted small">{{ t('collectionSettings.setASharedFundsAndPositionsCollectionInterval') }}</p>
      </div>
      <span class="icon-box subtle"><Clock :size="20" /></span>
    </div>
    <p v-if="isPending" class="empty-inline">{{ t('collectionSettings.loadingCollectionSettings') }}</p>
    <form v-else-if="data" class="collection-form" @submit.prevent="save">
      <div class="collection-controls">
        <label class="field">
          {{ t('collectionSettings.backgroundCollectionInterval') }}
          <select v-model.number="interval" :disabled="busy" aria-describedby="collection-description">
            <option v-for="seconds in intervals" :key="seconds" :value="seconds">
              {{ seconds === 60 ? t('collectionSettings.defaultInterval', { seconds }) : t('collectionSettings.intervalSeconds', { seconds }) }}
            </option>
          </select>
        </label>
        <div class="collection-refresh">
          <span class="muted small">{{ t('collectionSettings.dashboardRefresh') }}</span>
          <strong><RefreshCw :size="15" />{{ t('collectionSettings.everySeconds') }}</strong>
        </div>
        <button class="btn btn-primary" type="submit" :disabled="busy || !changed">
          {{ busy ? t('accountForm.saving') : t('collectionSettings.saveCollectionSettings') }}
        </button>
      </div>
      <p id="collection-description" class="muted small collection-description">
        {{ t('collectionSettings.dailyStatisticsAndHistoryBackfillRunOnSeparate') }}
      </p>
      <p v-if="interval === 300" class="muted small collection-description">
        {{ t('collectionSettings.longIntervalClockHint') }}
      </p>
      <p v-if="saved" role="status" class="collection-saved">
        <CheckCircle2 :size="15" />{{ t('collectionSettings.savedEnabledAccountsWillUseTheNewInterval') }}
      </p>
    </form>
    <p v-if="saveError || error" role="alert" class="error-message collection-error">
      {{ localizeMessage(saveError || error?.message) }}
      <button v-if="!data" class="text-button" type="button" @click="refetch()">{{ t('overview.retry') }}</button>
    </p>
  </section>
</template>

<style scoped>
.collection-settings .panel-heading > div {
  flex: 1;
  min-width: 0;
}
.collection-form {
  padding: 4px 26px 24px;
}
.collection-controls {
  display: flex;
  align-items: flex-end;
  gap: 32px;
}
.collection-controls .field {
  width: 240px;
  max-width: 100%;
}
.collection-refresh {
  display: grid;
  gap: 10px;
  padding-bottom: 10px;
}
.collection-refresh strong {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 500;
}
.collection-controls .btn {
  margin-left: auto;
}
.collection-description {
  margin: 16px 0 0;
  line-height: 1.7;
}
.collection-saved {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  color: var(--color-brand-600);
  font-size: 12px;
  margin: 12px 0 0;
}
.collection-saved svg {
  flex-shrink: 0;
}
.collection-error {
  padding: 0 26px 20px;
}
@media (max-width: 640px) {
  .collection-settings .panel-heading {
    flex-direction: row;
    align-items: flex-start;
  }
  .collection-settings .icon-box {
    flex-shrink: 0;
  }
  .collection-form {
    padding: 0 18px 20px;
  }
  .collection-controls {
    flex-wrap: wrap;
    gap: 20px;
  }
  .collection-controls .field {
    flex: 1;
    min-width: 150px;
  }
  .collection-controls .btn {
    width: 100%;
  }
  .collection-error {
    padding: 0 18px 20px;
  }
}
</style>
