<script setup lang="ts">
import NavIcon from '@/components/ui/NavIcon.vue'

// One empty / not-configured state for every page: what's missing, why, and the one action that fixes it.
withDefaults(defineProps<{
  icon?: string
  title: string
  text?: string
  actionLabel?: string
  actionTo?: string | Record<string, unknown>
  compact?: boolean
}>(), { icon: 'inbox' })

const emit = defineEmits<{ (e: 'action'): void }>()
</script>

<template>
  <div class="es" :class="{ 'es--compact': compact }" role="status">
    <span class="es__icon"><NavIcon :name="icon" :size="compact ? 18 : 22" /></span>
    <p class="es__title">{{ title }}</p>
    <p v-if="text" class="es__text">{{ text }}</p>
    <div v-if="actionLabel || $slots.default" class="es__actions">
      <slot>
        <router-link v-if="actionTo" :to="actionTo" class="base-btn base-btn--primary base-btn--sm">{{ actionLabel }}</router-link>
        <button v-else class="base-btn base-btn--primary base-btn--sm" type="button" @click="emit('action')">{{ actionLabel }}</button>
      </slot>
    </div>
  </div>
</template>

<style scoped>
.es {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 6px;
  padding: 56px 24px;
  margin: 0 auto;
  max-width: 460px;
}
.es--compact { padding: 28px 16px; }
.es__icon {
  width: 44px;
  height: 44px;
  display: grid;
  place-items: center;
  margin-bottom: 8px;
  border-radius: var(--r);
  background: var(--bg-hover-strong);
  color: var(--text-muted);
}
.es--compact .es__icon { width: 36px; height: 36px; margin-bottom: 4px; }
.es__title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.es--compact .es__title { font-size: 14px; }
.es__text {
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--text-secondary);
}
.es__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
  margin-top: 12px;
}
.es__actions a { text-decoration: none; }
</style>
