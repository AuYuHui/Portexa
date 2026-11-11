<script setup lang="ts">
import { useI18n } from "vue-i18n";
import {
  DialogRoot,
  DialogPortal,
  DialogOverlay,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogClose,
} from "reka-ui";
import { X } from "@lucide/vue";

const { t } = useI18n();
defineProps<{ title: string; description: string }>();
const open = defineModel<boolean>("open", { required: true });
</script>
<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="modal-overlay" />
      <DialogContent class="modal-content">
        <div class="modal-heading">
          <div>
            <DialogTitle class="modal-title">{{ title }}</DialogTitle
            ><DialogDescription class="muted">{{
              description
            }}</DialogDescription>
          </div>
          <DialogClose class="icon-button" :aria-label="t('modal.close')"
            ><X :size="20"
          /></DialogClose>
        </div>
        <slot v-if="open" />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
