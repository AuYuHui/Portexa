<script setup lang="ts">
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";
import { api } from "../api";
import type { Account, RestLearningGroup } from "../types";

const { t } = useI18n();
defineProps<{ disabled: boolean }>();
const learning = defineModel<boolean>("learning", {required:true});
const {data:groups,error:learningError}=useQuery({queryKey:["rest-learning"],queryFn:({signal})=>api<RestLearningGroup[]>("/market-rest/learning","GET",undefined,signal),refetchInterval:60_000});
const stateKey:Record<string,string>={learning:"learning",candidate:"candidate",adopted:"adopted",suspended:"suspended",off:"off"};
const reasonKeys:Record<string,string>={collecting:"collecting",no_quote:"noQuote",evidence_limit:"limit",silent_candidate:"silent",awaiting_holdout:"holdout",validation_failed:"failed",boundary_drift:"drift",validated:"validated",quote_conflict:"conflict",stale:"stale",disabled:"disabled"};
function windowLabel(close:number,open:number){
 const days=[t("restLearning.friday"),t("restLearning.saturday"),t("restLearning.sunday"),t("restLearning.monday")];
 const format=(minute:number)=>`${days[Math.floor(minute/1440)] ?? ""} ${String(Math.floor(minute%1440/60)).padStart(2,"0")}:${String(minute%60).padStart(2,"0")}`;
 return `${format(close)} – ${format(open)} · America/New_York`;
}
const template = defineModel<string>("template", { required: true });
const excluded = defineModel<string[]>("excluded", { required: true });
const { data: accounts } = useQuery({
  queryKey: ["accounts-for-collection"],
  queryFn: ({ signal }) => api<Account[]>("/accounts", "GET", undefined, signal),
});
</script>

<template>
  <fieldset class="market-rest-settings" :disabled="disabled">
    <legend>{{ t("collectionSettings.marketRest") }}</legend>
    <label class="field">
      {{ t("collectionSettings.marketRestTemplate") }}
      <select v-model="template" aria-describedby="market-rest-description">
        <option value="off">{{ t("collectionSettings.marketRestOff") }}</option>
        <option value="fx_metals_weekend_v1">{{ t("collectionSettings.marketRestWeekend") }}</option>
      </select>
    </label>
    <p id="market-rest-description" class="muted small">
      {{ t("collectionSettings.marketRestWindow") }}
      {{ t("collectionSettings.marketRestIntervals") }}
    </p>
    <p class="muted small">{{ t("collectionSettings.marketRestSafety") }}</p>
    <label class="market-rest-account">
      <input v-model="learning" type="checkbox" :disabled="template === 'off'" />
      <span>{{ t('restLearning.enable') }}</span>
    </label>
    <p class="muted small">{{ t('restLearning.description') }}</p>
    <p v-if="learningError" class="error-message">{{ t('restLearning.loadError') }}</p>
    <p v-else-if="!groups?.length" class="muted small" data-rest-learning>{{ t('restLearning.empty') }}</p>
    <ul v-else class="learning-groups" data-rest-learning>
      <li v-for="group in groups" :key="group.id">
        <strong>{{ group.server_name }} · {{ group.symbol }}</strong>
        <span>{{ t(`restLearning.${!learning || template === 'off' ? 'off' : stateKey[group.state] ?? 'learning'}`) }} · {{ t('restLearning.weeks', {count:group.valid_weekends}) }}</span>
        <span class="muted small">{{ t(`restLearning.${reasonKeys[group.reason] ?? 'collecting'}`) }}</span>
        <span v-if="group.candidate" class="small">{{ t('restLearning.candidateWindow') }} {{ windowLabel(group.candidate.close_minute,group.candidate.open_minute) }}</span>
        <span v-if="learning && template !== 'off' && group.active && group.state === 'adopted'" class="small">{{ t('restLearning.appliedWindow') }} {{ windowLabel(group.active.close_minute,group.active.open_minute) }}</span>
        <span v-else class="small muted">{{ t('restLearning.templateFallback') }}</span>
      </li>
    </ul>
    <details v-if="accounts?.length" class="market-rest-exceptions">
      <summary>{{ t("collectionSettings.marketRestExceptions") }}</summary>
      <label v-for="account in accounts" :key="account.id" class="market-rest-account">
        <input v-model="excluded" type="checkbox" :value="account.id" />
        <span>{{ account.label }}</span>
      </label>
    </details>
  </fieldset>
</template>

<style scoped>
.market-rest-settings { border: 0; border-top: 1px solid var(--color-border); margin: 20px 0 0; padding: 18px 0 0; min-width: 0; }
.market-rest-settings legend { font-size: 14px; font-weight: 600; padding-right: 12px; }
.market-rest-settings .field { max-width: 360px; }
.market-rest-settings p { margin: 12px 0 0; line-height: 1.7; }
.market-rest-exceptions { margin-top: 16px; max-height: 240px; overflow: auto; }
.market-rest-exceptions summary { cursor: pointer; font-size: 12px; color: var(--color-text-muted); }
.market-rest-account { display: flex; align-items: center; gap: 10px; padding: 8px 0; font-size: 13px; }
.learning-groups { list-style: none; padding: 0; display: grid; gap: 12px; max-height: 360px; overflow: auto; }
.learning-groups li { display: grid; gap: 6px; padding: 12px; border: 1px solid var(--color-border); border-radius: 8px; overflow-wrap: anywhere; }
.market-rest-account input { width: 16px; height: 16px; }
</style>
