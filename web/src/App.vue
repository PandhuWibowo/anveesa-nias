<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, NNotificationProvider, NSpin } from 'naive-ui'
import { onMounted, computed } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { useAuth } from '@/composables/useAuth'

const { naiveTheme, themeOverrides, syncTheme } = useTheme()
const { authReady, fetchMe } = useAuth()

onMounted(async () => {
  syncTheme()
  await fetchMe()
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
