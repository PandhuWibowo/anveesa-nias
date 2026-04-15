<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, NNotificationProvider, NSpin } from 'naive-ui'
import { onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useTheme } from '@/composables/useTheme'
import { useAuth } from '@/composables/useAuth'

const { naiveTheme, themeOverrides, syncTheme } = useTheme()
const { authReady, authEnabled, isAuthenticated, fetchMe } = useAuth()
const router = useRouter()
const route = useRoute()

onMounted(async () => {
  syncTheme()
  await fetchMe()
  
  // After auth check, redirect to login if auth is enabled and user is not authenticated
  if (authEnabled.value && !isAuthenticated.value && route.name !== 'login') {
    router.push({ name: 'login' })
  }
})
</script>

<template>
  <NConfigProvider :theme="naiveTheme" :theme-overrides="themeOverrides" class="n-providers">
    <NDialogProvider>
      <NMessageProvider>
        <NNotificationProvider>
          <div v-if="!authReady" class="app-loading">
            <NSpin size="large" />
          </div>
          <router-view v-else />
        </NNotificationProvider>
      </NMessageProvider>
    </NDialogProvider>
  </NConfigProvider>
</template>

<style scoped>
.app-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  background: var(--bg-body);
}
</style>
