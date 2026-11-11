<script setup lang="ts">
import { ref } from "vue";
import type { FundEntry } from "../types";
import { api } from "../api";
import { refreshAccounts } from "../state";
import { money, fundCategories } from "../format";

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
      {{ entry.native_currency }} · 流水版本 {{ entry.revision }}
    </p>
    <p v-if="entry.needs_review" class="notice notice-warning">
      流水已修订，原分类已退出累计。请核对后重新确认。
    </p>
    <p v-if="entry.classification" class="muted small">
      {{
        entry.classification.source === "automatic"
          ? "自动识别依据"
          : "上次校正依据"
      }}：{{ entry.classification.reason }}
    </p>
    <p v-if="entry.comment" class="muted small">
      原始备注：{{ entry.comment }}
    </p>
    <div class="field">
      <label for="fund-category">资金分类</label>
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
      <label for="fund-reason">校正原因</label>
      <textarea
        id="fund-reason"
        v-model="reason"
        required
        maxlength="500"
        rows="3"
        placeholder="说明补充或校正识别结果的原因"
      />
    </div>
    <p class="muted small">
      外部出入金自动统计；返佣、费用、内部转账及其他调整单独保留。
      分类作用于整笔流水，混合用途需拆分的流水请保留待分类。
    </p>
    <p v-if="error" role="alert" class="notice notice-error">{{ error }}</p>
    <div class="form-actions">
      <button class="btn btn-primary" :disabled="busy || !reason.trim()">
        {{ busy ? "保存中…" : "保存校正" }}
      </button>
    </div>
  </form>
</template>
