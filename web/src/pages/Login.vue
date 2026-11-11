<script setup lang="ts">
import { localizeMessage } from "../i18n";
import { useI18n } from "vue-i18n";
import { ref } from "vue";
import {
  Layers3,
  ArrowRight,
  ShieldCheck,
  LoaderCircle,
  KeyRound,
} from "@lucide/vue";
import { useSession } from "../session";
import LanguageSwitch from "../components/LanguageSwitch.vue";

const { t } = useI18n();
const session = useSession();
const password = ref("");
const busy = ref(false);
const error = ref("");
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    await session.login(password.value);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    password.value = "";
    busy.value = false;
  }
}
</script>

<template>
  <main class="login-screen">
    <section class="login-story">
      <div class="brand">
        <span class="brand-mark"><Layers3 :size="25" /></span>Portexa<span
          class="brand-dot"
          >.</span
        >
      </div>
      <span class="eyebrow">YOUR ACCOUNTS, ONE PERSPECTIVE</span>
      <h1>{{ t('login.everyAccount') }}<br /> {{ t('login.atAGlance') }}</h1>
      <p>{{ t('login.storyDescription') }}</p>
      <div class="login-assurance">
        <ShieldCheck :size="20" /><span
          >{{ t('login.privateDeployment') }} <span>·</span> {{ t('login.encryptedInvestorCredentials') }} <span>·</span> {{ t('login.readOnlyCollection') }}</span
        >
      </div>
      <div class="story-lines" aria-hidden="true">
        <i /><i /><i /><i /><i />
      </div>
    </section>
    <section class="login-form-wrap">
      <LanguageSwitch class="login-language" />
      <form class="login-form" @submit.prevent="submit">
        <span class="icon-box"><KeyRound :size="24" /></span>
        <h2>{{ t('login.enterYourWorkspace') }}</h2>
        <p class="muted">{{ t('login.signInWithTheSystemPasswordSetDuring') }}</p>
        <label class="field"
          >{{ t('login.systemPassword') }}<input
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            autofocus
            :placeholder="t('login.enterAPasswordOfAtLeastCharacters')"
            maxlength="1024"
        /></label>
        <p v-if="error" class="error-message" role="alert">{{ localizeMessage(error) }}</p>
        <button
          class="btn btn-primary login-submit"
          :disabled="busy || !password"
        >
          <LoaderCircle v-if="busy" :size="18" class="spin" /><span>{{
            busy ? t('login.verifying') : t('login.signInSecurely')
          }}</span
          ><ArrowRight v-if="!busy" :size="18" />
        </button>
        <p class="login-help">
          {{ t('login.forgotYourPasswordResetItLocallyOnThe') }}<br />{{ t('login.noUsernameOrRegistrationRequired') }}
        </p>
      </form>
      <span class="login-footer">{{ t('login.portexaYourDataInYourHands') }}</span>
    </section>
  </main>
</template>
