<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { ref, shallowRef } from "vue";
import { Search, Settings2 } from "@lucide/vue";
import type { DirectoryServerName, Server } from "../types";
import DirectoryPicker from "./DirectoryPicker.vue";
import ManualServerForm from "./ManualServerForm.vue";

const { t } = useI18n();
const props = defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const manual = ref(props.server?.protocol === "mt5");
const mt5Selection = shallowRef<DirectoryServerName>();

function selectMT5(server: DirectoryServerName) {
  mt5Selection.value = server;
  manual.value = true;
}
</script>

<template>
  <div class="server-entry">
    <div v-if="server?.protocol !== 'mt5'" class="entry-modes" :aria-label="t('serverForm.serverConnectionMethod')">
      <button
        type="button"
        :class="{ active: !manual }"
        @click="manual = false"
      >
        <Search :size="15" />{{ t('settings.findServer') }}
      </button>
      <button type="button" :class="{ active: manual }" @click="manual = true">
        <Settings2 :size="15" />{{ t('serverForm.manualEntryAdvanced') }}
      </button>
    </div>
    <ManualServerForm
      v-if="manual"
      :server="server"
      :mt5-selection="mt5Selection"
      @saved="emit('saved')"
      @cancel="emit('cancel')"
    />
    <DirectoryPicker
      v-else
      :server="server"
      :initial-platform="mt5Selection ? 'mt5' : undefined"
      @select-mt5="selectMT5"
      @saved="emit('saved')"
      @cancel="emit('cancel')"
    />
  </div>
</template>

<style src="../styles/server-discovery.css"></style>
