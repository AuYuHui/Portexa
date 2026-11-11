<script setup lang="ts">
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
      选择
      {{
        server.server_name
      }}
      的新接入点后，旧修订将停用，相关账户会暂停；随后请在账户配置中重新绑定。
    </p>
    <div class="field">
      <label for="server-directory-search-input">搜索经纪商或服务器</label>
      <div class="directory-search">
        <Search :size="16" class="search-icon" />
        <input
          id="server-directory-search-input"
          v-model="query"
          type="search"
          aria-label="搜索经纪商或服务器"
          autocomplete="off"
          maxlength="80"
          placeholder="例如 DPrime、IC Markets、服务器名称"
          :disabled="saving"
        />
        <LoaderCircle
          v-if="loading"
          :size="16"
          class="spin loading-icon"
        />
      </div>
      <small>输入至少 2 个字符，自动查询 MT4 服务器及候选地址，无需账户密码。</small>
    </div>

    <template v-if="!selected">
      <div v-if="loading" class="directory-status" role="status">
        <LoaderCircle :size="18" class="spin" />正在查询服务器目录…
      </div>
      <div
        v-else-if="shown.length"
        class="directory-results"
        aria-label="服务器搜索结果"
      >
        <button
          v-for="candidate in shown"
          :key="candidate.id"
          type="button"
          class="directory-result"
          :disabled="!candidate.endpoints.length"
          :aria-label="`选择服务器 ${candidate.server_name}`"
          @click="choose(candidate)"
        >
          <span class="icon-box subtle"><ServerIcon :size="18" /></span
          ><span class="directory-result-name"
            ><strong>{{ candidate.server_name }}</strong
            ><small>{{ candidate.broker_name }}</small
            ><span class="directory-result-meta">{{
              candidate.endpoints.length
                ? `${candidate.endpoints.length} 个候选接入点`
                : "目录暂未返回可用地址"
            }}</span></span
          ><span
            class="badge"
            :class="candidate.demo ? 'badge-info' : 'badge-neutral'"
            >{{ candidate.demo ? "模拟" : "真实" }}</span
          >
        </button>
      </div>
      <div v-else-if="searched && !error" class="directory-empty">
        <Search :size="24" /><strong>没有找到匹配的 MT4 服务器</strong>
        <p>试试经纪商简称、旧名称或服务器完整名称。新服务器可能尚未收录。</p>
      </div>
      <div v-else-if="!error" class="directory-empty">
        <ServerIcon :size="26" /><strong>选择经纪商和服务器即可开始</strong>
        <p>系统自动取得候选地址，并保存来源记录。你无需查找或输入 IP。</p>
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
          <ChevronLeft :size="14" />重新选择
        </button>
        <h3>{{ selected.server_name }}</h3>
        <p>{{ selected.broker_name }}</p>
        <span class="badge badge-good"
          ><Check :size="13" />已取得
          {{ selected.endpoints.length }} 个候选接入点</span
        >
      </div>
      <div class="directory-provenance">
        <ShieldCheck :size="18" />
        <div>
          <strong>{{ selected.source_name }}</strong>
          <p>
            地址与端口自动带入，来源记录自动保存。查询于
            {{ date(selected.fetched_at_ms) }}。
          </p>
        </div>
      </div>
      <details class="directory-details">
        <summary>查看接入地址与来源</summary>
        <label class="field"
          >固定接入点<select
            v-model.number="endpoint"
            aria-label="目录候选接入点"
          >
            <option
              v-for="(access, index) in selected.endpoints"
              :key="`${access.ip_address}:${access.port}`"
              :value="index"
            >
              接入点 {{ index + 1 }} · {{ access.ip_address }}:{{ access.port }}
            </option>
          </select></label
        >
        <p class="small muted">{{ selected.source_url }}</p>
        <p class="small muted">
          本次固定到所选地址，不会自动向其他接入点发送凭据。
        </p>
      </details>
      <label class="checkbox-row"
        ><input
          v-model="risk"
          type="checkbox"
          required
          :disabled="saving"
        /><span
          >我已核对经纪商和服务器，并确认此次接入。<small
            >候选地址来自官方目录，仍不能提供密码学服务端身份认证；直连仍有伪造对端和网络路径劫持的剩余风险。本次确认仅适用于所选接入点修订。</small
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
          取消
        </button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="!risk || saving"
        >
          <LoaderCircle v-if="saving" class="spin" :size="16" />
          {{
            saving
              ? "正在保存…"
              : server
                ? "确认新接入修订并暂停账户"
                : "登记并确认接入"
          }}
        </button>
      </div>
      <p class="small muted directory-footer">
        登记过程不登录交易账户，也不发送投资者凭据。
      </p>
    </template>
    <p v-if="error" class="error-message" role="alert">{{ error }}</p>
  </form>
</template>
