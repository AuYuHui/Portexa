<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import { AlertTriangle, LoaderCircle } from "@lucide/vue";
import type { Endpoint } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";

const { t } = useI18n();
const props = defineProps<{ endpoint: Endpoint }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const reference = ref(props.endpoint.evidence_ref || "");
const digest = ref("");
const risk = ref(false);
const busy = ref(false);
const error = ref<string | (() => string)>("");
async function hashFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) return;
  if (file.size > 10 * 1024 * 1024) {
    error.value = () => t('reviewForm.keepTheVerificationFileWithinMb');
    return;
  }
  try {
    const hash = await crypto.subtle.digest(
      "SHA-256",
      await file.arrayBuffer(),
    );
    digest.value = Array.from(new Uint8Array(hash), (b) =>
      b.toString(16).padStart(2, "0"),
    ).join("");
  } catch {
    error.value = () => t('reviewForm.unableToCalculateTheDigestEnterShaManually');
  }
}
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    await api(`/endpoints/${props.endpoint.id}/review`, "POST", {
      evidence_ref: reference.value,
      evidence_sha256: digest.value,
      accept_risk: risk.value,
    });
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
  <form class="form-stack" @submit.prevent="submit">
    <div class="endpoint-summary num">
      {{ t('accountForm.revision') }} {{ endpoint.revision }}
      <strong>{{ endpoint.ip_address }}:{{ endpoint.port }}</strong>
    </div>
    <label class="field"
      >{{ t('reviewForm.independentVerificationEvidence') }}<input
        v-model="reference"
        required
        maxlength="512"
        :placeholder="t('reviewForm.exampleOfficialBrokerDocumentLocalEvidenceReference')"
        :readonly="endpoint.state === 'reviewed'"
      /><small
        >{{ t('reviewForm.verifyIndependentlyThroughOfficialBrokerChannelsOrA') }}</small
      ></label
    >
    <label class="field"
      >{{ t('reviewForm.evidenceSha') }}<input
        v-model="digest"
        required
        pattern="[a-fA-F0-9]{64}"
        minlength="64"
        maxlength="64"
        :placeholder="t('reviewForm.characterHexadecimalDigest')"
        class="mono"
    /></label>
    <label class="field"
      >{{ t('reviewForm.orCalculateAFileDigestLocally') }}<input type="file" @change="hashFile" /><small
        >{{ t('reviewForm.theFileDigestIsCalculatedOnlyInYour') }}</small
      ></label
    >
    <div class="notice notice-warning risk-notice">
      <AlertTriangle :size="20" />
      <p>
        {{ t('reviewForm.thePinnedSdkSendsPasswordDerivedMaterialBefore') }}
      </p>
    </div>
    <label class="checkbox-row"
      ><input v-model="risk" required type="checkbox" /><span
        >{{ t('reviewForm.iHaveIndependentlyVerifiedThisIpPortAnd') }}<small
          >{{ t('reviewForm.validOnlyForTheCurrentRevisionAddressOr') }}</small
        ></span
      ></label
    >
    <p v-if="error" role="alert" class="error-message">{{ localizeMessage(error) }}</p>
    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('cancel')"
      >
        {{ t('accountForm.cancel') }}
      </button>
      <button
        type="submit"
        class="btn btn-primary"
        :disabled="busy || !risk"
      >
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{ busy ? t('accountForm.saving') : t('reviewForm.saveVerificationAndRiskAcceptance') }}
      </button>
    </div>
  </form>
</template>
