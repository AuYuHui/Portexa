<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { computed, ref, shallowRef, watch } from "vue";
import {
  Search,
  LoaderCircle,
  Check,
  ShieldCheck,
  ChevronLeft,
  Server as ServerIcon,
} from "@lucide/vue";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { date } from "../format";
import type { DirectoryCandidate, Server } from "../types";

const { t } = useI18n();

const props = defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const query = ref(props.server?.server_name || "");
const results = shallowRef<DirectoryCandidate[]>([]);
const selected = shallowRef<DirectoryCandidate>();
const endpoint = ref(0);
const risk = ref(false);
const loading = ref(false);
const searched = ref(false);
const saving = ref(false);
const error = ref("");
const shown = computed(() =>
  props.server
    ? results.value.filter(
        (s) =>
          s.server_name.toLowerCase() ===
          props.server?.server_name.toLowerCase(),
      )
    : results.value,
);

watch(
  query,
  (value, _old, cleanup) => {
    selected.value = undefined;
    results.value = [];
    searched.value = false;
    risk.value = false;
    error.value = "";
    loading.value = false;
    if (Array.from(value.trim()).length < 2) return;
    const controller = new AbortController();
    loading.value = true;
    const timer = window.setTimeout(async () => {
      try {
        const list = await api<DirectoryCandidate[]>(
          `/server-directory?q=${encodeURIComponent(value.trim())}`,
          "GET",
          undefined,
          controller.signal,
        );
        if (!controller.signal.aborted) {
          results.value = list;
          searched.value = true;
        }
      } catch (e) {
        if (!controller.signal.aborted) {
          error.value = (e as Error).message;
          searched.value = true;
        }
      } finally {
        if (!controller.signal.aborted) loading.value = false;
      }
    }, 350);
    cleanup(() => {
      window.clearTimeout(timer);
      controller.abort();
    });
  },
  { immediate: true },
);

function choose(candidate: DirectoryCandidate) {
  selected.value = candidate;
  endpoint.value = 0;
  risk.value = false;
  error.value = "";
}

async function submit() {
  if (!selected.value || !risk.value || saving.value) return;
  saving.value = true;
  error.value = "";
  try {
    const path = props.server
      ? `/servers/${props.server.id}/discovered-endpoints`
      : "/servers/discovered";
    await api(path, "POST", {
      candidate_id: selected.value.id,
      endpoint_index: endpoint.value,
      accept_risk: risk.value,
    });
    await refreshAccounts();
    emit("saved");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <form class="form-stack directory-form" @submit.prevent="submit">
    <p v-if="server" class="notice notice-warning">
      {{ t('directoryPicker.revisionWarning', { server: server.server_name }) }}
    </p>
    <div class="field">
      <label for="server-directory-search-input">{{ t('directoryPicker.searchBrokerOrServer') }}</label>
      <div class="directory-search">
        <Search :size="16" class="search-icon" />
        <input
          id="server-directory-search-input"
          v-model="query"
          type="search"
          :aria-label="t('directoryPicker.searchBrokerOrServer')"
          autocomplete="off"
          maxlength="80"
          :placeholder="t('directoryPicker.exampleDprimeIcMarketsOrAServerName')"
          :disabled="saving"
        />
        <LoaderCircle
          v-if="loading"
          :size="16"
          class="spin loading-icon"
        />
      </div>
      <small>{{ t('directoryPicker.enterAtLeastCharactersToSearchMtServers') }}</small>
    </div>

    <template v-if="!selected">
      <div v-if="loading" class="directory-status" role="status">
        <LoaderCircle :size="18" class="spin" />{{ t('directoryPicker.searchingServerDirectory') }}
      </div>
      <div
        v-else-if="shown.length"
        class="directory-results"
        :aria-label="t('directoryPicker.serverSearchResults')"
      >
        <button
          v-for="candidate in shown"
          :key="candidate.id"
          type="button"
          class="directory-result"
          :disabled="!candidate.endpoints.length"
          :aria-label="t('directoryPicker.selectServer', { value1: candidate.server_name })"
          @click="choose(candidate)"
        >
          <span class="icon-box subtle"><ServerIcon :size="18" /></span
          ><span class="directory-result-name"
            ><strong>{{ candidate.server_name }}</strong
            ><small>{{ candidate.broker_name }}</small
            ><span class="directory-result-meta">{{
              candidate.endpoints.length
                ? t('directoryPicker.candidateEndpoints', { value1: candidate.endpoints.length })
                : t('directoryPicker.noAvailableEndpointsReturnedByTheDirectory')
            }}</span></span
          ><span
            class="badge"
            :class="candidate.demo ? 'badge-info' : 'badge-neutral'"
            >{{ candidate.demo ? t('directoryPicker.demo') : t('directoryPicker.live') }}</span
          >
        </button>
      </div>
      <div v-else-if="searched && !error" class="directory-empty">
        <Search :size="24" /><strong>{{ t('directoryPicker.noMatchingMtServers') }}</strong>
        <p>{{ t('directoryPicker.tryABrokerAbbreviationFormerNameOrFull') }}</p>
      </div>
      <div v-else-if="!error" class="directory-empty">
        <ServerIcon :size="26" /><strong>{{ t('directoryPicker.chooseABrokerAndServerToBegin') }}</strong>
        <p>{{ t('directoryPicker.candidateEndpointsAreRetrievedAndTheirSourceRecorded') }}</p>
      </div>
    </template>

    <template v-else>
      <div class="directory-selected">
        <button
          type="button"
          class="text-button"
          :disabled="saving"
          @click="
            selected = undefined;
            risk = false;
          "
        >
          <ChevronLeft :size="14" />{{ t('directoryPicker.chooseAgain') }}
        </button>
        <h3>{{ selected.server_name }}</h3>
        <p>{{ selected.broker_name }}</p>
        <span class="badge badge-good"
          ><Check :size="13" />{{ t('directoryPicker.retrievedEndpoints', { count: selected.endpoints.length }) }}</span
        >
      </div>
      <div class="directory-provenance">
        <ShieldCheck :size="18" />
        <div>
          <strong>{{ localizeMessage(selected.source_name) }}</strong>
          <p>
            {{ t('directoryPicker.sourceTimestamp', { time: date(selected.fetched_at_ms) }) }}
          </p>
        </div>
      </div>
      <details class="directory-details">
        <summary>{{ t('directoryPicker.viewEndpointAndSource') }}</summary>
        <label class="field"
          >{{ t('accountForm.fixedEndpoint') }}<select
            v-model.number="endpoint"
            :aria-label="t('directoryPicker.directoryCandidateEndpoint')"
          >
            <option
              v-for="(access, index) in selected.endpoints"
              :key="`${access.ip_address}:${access.port}`"
              :value="index"
            >
              {{ t('directoryPicker.endpointOption', { number: index + 1, address: access.ip_address, port: access.port }) }}
            </option>
          </select></label
        >
        <p class="small muted">{{ selected.source_url }}</p>
        <p class="small muted">
          {{ t('directoryPicker.thisConnectionIsPinnedToTheSelectedAddress') }}
        </p>
      </details>
      <label class="checkbox-row"
        ><input
          v-model="risk"
          type="checkbox"
          required
          :disabled="saving"
        /><span
          >{{ t('directoryPicker.iHaveCheckedTheBrokerAndServerAnd') }}<small
            >{{ t('directoryPicker.officialDirectoryAddressesDoNotProvideCryptographicServer') }}</small
          ></span
        ></label
      >
      <div class="dialog-actions">
        <button
          type="button"
          class="btn"
          :disabled="saving"
          @click="emit('cancel')"
        >
          {{ t('accountForm.cancel') }}
        </button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="!risk || saving"
        >
          <LoaderCircle v-if="saving" class="spin" :size="16" />
          {{
            saving
              ? t('accountForm.saving')
              : server
                ? t('directoryPicker.confirmNewRevisionAndPauseAccounts')
                : t('directoryPicker.registerAndConfirmConnection')
          }}
        </button>
      </div>
      <p class="small muted directory-footer">
        {{ t('directoryPicker.registrationDoesNotLogInToATrading') }}
      </p>
    </template>
    <p v-if="error" class="error-message" role="alert">{{ localizeMessage(error) }}</p>
  </form>
</template>
