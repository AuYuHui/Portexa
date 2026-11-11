<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import { Search, Settings2 } from "@lucide/vue";
import type { Server } from "../types";
import DirectoryPicker from "./DirectoryPicker.vue";
import ManualServerForm from "./ManualServerForm.vue";

const { t } = useI18n();
defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const manual = ref(false);
</script>

<template>
  <div class="server-entry">
    <div class="entry-modes" :aria-label="t('serverForm.serverConnectionMethod')">
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
      @saved="emit('saved')"
      @cancel="emit('cancel')"
    />
    <DirectoryPicker
      v-else
      :server="server"
      @saved="emit('saved')"
      @cancel="emit('cancel')"
    />
  </div>
</template>

<style src="../styles/server-discovery.css"></style>
