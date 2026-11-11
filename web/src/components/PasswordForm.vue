<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import { KeyRound, ShieldAlert } from "@lucide/vue";
import { api } from "../api";
import { useSession } from "../session";

const { t } = useI18n();

const emit = defineEmits<{ cancel: []; saved: [] }>();
const session = useSession();

const oldPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const passwordError = ref<string | (() => string)>("");
const passwordBusy = ref(false);

async function changePassword() {
  passwordError.value = "";
  if (Array.from(newPassword.value).length < 16) {
    passwordError.value = () => t('passwordForm.theNewPasswordMustContainAtLeastCharacters');
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = () => t('passwordForm.theNewPasswordsDoNotMatch');
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
      <span>{{ t('passwordForm.changingThePasswordRevokesAllActiveSessionsAnd') }}</span>
    </div>

    <label class="field">
      {{ t('passwordForm.currentPassword') }}
      <input
        v-model="oldPassword"
        type="password"
        autocomplete="current-password"
        required
        maxlength="1024"
        :placeholder="t('passwordForm.enterYourCurrentLoginPassword')"
      />
    </label>

    <div class="form-grid">
      <label class="field">
        {{ t('passwordForm.newPassword') }}
        <input
          v-model="newPassword"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          :placeholder="t('passwordForm.atLeastCharacters')"
        />
      </label>
      <label class="field">
        {{ t('passwordForm.confirmNewPassword') }}
        <input
          v-model="confirmPassword"
          type="password"
          autocomplete="new-password"
          required
          maxlength="1024"
          :placeholder="t('passwordForm.confirmNewPassword')"
        />
      </label>
    </div>

    <p v-if="passwordError" class="error-message" role="alert">
      {{ localizeMessage(passwordError) }}
    </p>

    <div class="dialog-actions">
      <button
        type="button"
        class="btn"
        :disabled="passwordBusy"
        @click="emit('cancel')"
      >
        {{ t('accountForm.cancel') }}
      </button>
      <button type="submit" class="btn btn-primary" :disabled="passwordBusy">
        <KeyRound :size="15" />
        {{ passwordBusy ? t('passwordForm.updating') : t('passwordForm.confirmChangeAndSignInAgain') }}
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
