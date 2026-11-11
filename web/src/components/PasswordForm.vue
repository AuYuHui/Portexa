<script setup lang="ts">
import { ref } from "vue";
import { KeyRound, ShieldAlert } from "@lucide/vue";
import { api } from "../api";
import { useSession } from "../session";

const emit = defineEmits<{ cancel: []; saved: [] }>();
const session = useSession();

const oldPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const passwordError = ref("");
const passwordBusy = ref(false);

async function changePassword() {
  passwordError.value = "";
  if (Array.from(newPassword.value).length < 16) {
    passwordError.value = "新密码至少需要 16 个字符。";
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = "两次输入的新密码不一致。";
    return;
  }
  passwordBusy.value = true;
  try {
    await api("/auth/password", "PUT", {
      old_password: oldPassword.value,
      new_password: newPassword.value,
    });
    emit("saved");
    session.clear();
  } catch (e) {
    passwordError.value = (e as Error).message;
  } finally {
    passwordBusy.value = false;
  }
}
</script>

<template>
  <form class="form-stack password-dialog-form" @submit.prevent="changePassword">
    <div class="security-alert-callout">
      <ShieldAlert :size="16" />
      <span>修改成功后将撤销所有活跃会话，当前页面将返回登录界面。</span>
    </div>

    <label class="field">
      当前密码
      <input
        v-model="oldPassword"
        type="password"
        autocomplete="current-password"
        required
        maxlength="1024"
        placeholder="输入当前使用的登录密码"
      />
    </label>

    <div class="form-grid">
      <label class="field">
        新密码
        <input
          v-model="newPassword"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          placeholder="至少 16 个字符"
        />
      </label>
      <label class="field">
        再次输入新密码
        <input
          v-model="confirmPassword"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          placeholder="再次输入新密码"
        />
      </label>
    </div>

    <p v-if="passwordError" class="error-message" role="alert">
      {{ passwordError }}
    </p>

    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="passwordBusy"
        @click="emit('cancel')"
      >
        取消
      </button>
      <button type="submit" class="btn btn-primary" :disabled="passwordBusy">
        <KeyRound :size="15" />
        {{ passwordBusy ? "正在修改…" : "确认修改并重新登录" }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.password-dialog-form {
  margin-top: 4px;
}
.security-alert-callout {
  display: flex;
  align-items: center;
  gap: 10px;
  background: #fffbeb;
  border: 1px solid #fde68a;
  color: #92400e;
  border-radius: 6px;
  padding: 10px 14px;
  font-size: 12px;
  line-height: 1.5;
}
.security-alert-callout svg {
  flex-shrink: 0;
  color: #d97706;
}
</style>
