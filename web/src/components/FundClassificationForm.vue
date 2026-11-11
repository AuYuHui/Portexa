<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import type { FundEntry } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { money, fundCategories } from "../format";

const { t } = useI18n();

const props = defineProps<{ accountId: string; entry: FundEntry }>();
const emit = defineEmits<{ saved: [] }>();
const category = ref(
  props.entry.needs_review
    ? "unclassified"
    : props.entry.classification?.category || "unclassified",
);
const reason = ref("");
const busy = ref(false);
const error = ref("");
async function save() {
  busy.value = true;
  error.value = "";
  try {
    await api(
      `/accounts/${props.accountId}/funds/${props.entry.ticket}/classification`,
      "PUT",
      {
        trade_revision: props.entry.revision,
        classification_revision: props.entry.classification?.revision || 0,
        category: category.value,
        reason: reason.value,
      },
    );
    await refreshAccounts();
    emit("saved");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <form class="form-stack" @submit.prevent="save">
    <p class="notice notice-subtle num">
      #{{ entry.ticket }} · {{ money(entry.net_profit) }}
      {{ entry.native_currency }} {{ t('fundClassificationForm.entryRevision') }} {{ entry.revision }}
    </p>
    <p v-if="entry.needs_review" class="notice notice-warning">
      {{ t('fundClassificationForm.theEntryWasRevisedAndItsPreviousClassification') }}
    </p>
    <p v-if="entry.classification" class="muted small">
      {{
        entry.classification.source === "automatic"
          ? t('fundClassificationForm.automaticDetectionBasis')
          : t('fundClassificationForm.previousCorrectionBasis')
      }}: {{ entry.classification.source === "automatic" ? localizeMessage(entry.classification.reason) : entry.classification.reason }}
    </p>
    <p v-if="entry.comment" class="muted small">
      {{ t('fundClassificationForm.originalComment') }}{{ entry.comment }}
    </p>
    <div class="field">
      <label for="fund-category">{{ t('fundClassificationForm.fundCategory') }}</label>
      <select id="fund-category" v-model="category" required>
        <option
          v-for="(label, value) in fundCategories"
          :key="value"
          :value="value"
        >
          {{ label }}
        </option>
      </select>
    </div>
    <div class="field">
      <label for="fund-reason">{{ t('fundClassificationForm.correctionReason') }}</label>
      <textarea
        id="fund-reason"
        v-model="reason"
        required
        maxlength="500"
        rows="3"
        :placeholder="t('fundClassificationForm.explainWhyYouAreClassifyingOrCorrectingThis')"
      />
    </div>
    <p class="muted small">
      {{ t('fundClassificationForm.externalDepositsAndWithdrawalsAreCalculatedAutomaticallyRebates') }}
    </p>
    <p v-if="error" role="alert" class="notice notice-error">{{ localizeMessage(error) }}</p>
    <div class="form-actions">
      <button class="btn btn-primary" :disabled="busy || !reason.trim()">
        {{ busy ? t('fundClassificationForm.saving') : t('fundClassificationForm.saveCorrection') }}
      </button>
    </div>
  </form>
</template>
