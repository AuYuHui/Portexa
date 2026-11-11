<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import { LoaderCircle } from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import type { Server } from "../types";

const { t } = useI18n();
const props = defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const broker = ref("");
const name = ref("");
const address = ref("");
const port = ref(443);
const busy = ref(false);
const error = ref("");
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    if (props.server) {
      await api(`/servers/${props.server.id}/endpoints`, "POST", {
        ip_address: address.value,
        port: port.value,
      });
    } else {
      await api("/servers", "POST", {
        broker_name: broker.value,
        server_name: name.value,
        ip_address: address.value,
        port: port.value,
      });
    }
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
    <template v-if="!server"
      ><label class="field"
        >{{ t('manualServerForm.broker') }}<input
          v-model="broker"
          required
          maxlength="128"
          :placeholder="t('manualServerForm.brokerName')" /></label
      ><label class="field"
        >{{ t('manualServerForm.exactMtServerName') }}<input
          v-model="name"
          required
          maxlength="128"
          :placeholder="t('manualServerForm.exampleSyntheticLive')"
        /><small>{{ t('manualServerForm.theServerNameIsUsedForProtocolConfiguration') }}</small></label
      ></template
    >
    <p v-else class="notice notice-warning">
      {{ t('manualServerForm.aNewRevisionRetiresThePreviousAddressAnd') }}
    </p>
    <div class="form-grid">
      <label class="field"
        >{{ t('manualServerForm.fixedIpAddress') }}<input
          v-model="address"
          required
          :placeholder="t('manualServerForm.independentlyVerifiedPublicIp')" /></label
      ><label class="field"
        >{{ t('manualServerForm.port') }}<input
          v-model.number="port"
          required
          type="number"
          min="1"
          max="65535"
      /></label>
    </div>
    <p class="muted small">
      {{ t('manualServerForm.savingRegistersConfigurationWithoutSendingCredentialsNextRecord') }}
    </p>
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
      <button type="submit" class="btn btn-primary" :disabled="busy">
        <LoaderCircle v-if="busy" class="spin" :size="16" />
        {{
          busy
            ? t('manualServerForm.registering')
            : server
              ? t('manualServerForm.createRevisionAndPauseAccounts')
              : t('manualServerForm.registerServerAndContinueVerification')
        }}
      </button>
    </div>
  </form>
</template>
