<script setup lang="ts">
import { ref } from "vue";
import { Search, Settings2 } from "@lucide/vue";
import type { Server } from "../types";
import DirectoryPicker from "./DirectoryPicker.vue";
import ManualServerForm from "./ManualServerForm.vue";
defineProps<{ server?: Server }>();
const emit = defineEmits<{ cancel: []; saved: [] }>();
const manual = ref(false);
</script>

<template>
  <div class="server-entry">
    <div class="entry-modes" aria-label="服务器接入方式">
      <button
        type="button"
        :class="{ active: !manual }"
        @click="manual = false"
      >
        <Search :size="15" />搜索服务器
      </button>
      <button type="button" :class="{ active: manual }" @click="manual = true">
        <Settings2 :size="15" />手动填写（高级）
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
