<script setup lang="ts">
import { computed } from 'vue'
import { NTag, NTooltip } from 'naive-ui'

interface Props {
  permissions: string[]
  size?: 'small' | 'medium' | 'large'
}

const props = withDefaults(defineProps<Props>(), {
  size: 'small'
})

const permissionLevel = computed(() => {
  const perms = props.permissions || []
  
  // Full access
  if (perms.length >= 7 || (perms.includes('create') && perms.includes('alter') && perms.includes('drop'))) {
    return {
      label: 'Full Access',
      type: 'success' as const,
      icon: '🔓',
      description: 'Can execute all database operations including DDL (CREATE, ALTER, DROP)'
    }
  }
  
  // Read-write
  if (perms.includes('insert') || perms.includes('update') || perms.includes('delete')) {
    return {
      label: 'Read-Write',
      type: 'info' as const,
      icon: '✏️',
      description: 'Can read and modify data (SELECT, INSERT, UPDATE, DELETE)'
    }
  }
  
  // Read-only
  if (perms.includes('select')) {
    return {
      label: 'Read-Only',
      type: 'warning' as const,
      icon: '👁️',
      description: 'Can only read data (SELECT queries only)'
    }
  }
  
  // No access
  return {
    label: 'No Access',
    type: 'default' as const,
    icon: '🔒',
    description: 'No permissions granted'
  }
})

const permissionDetails = computed(() => {
  const perms = props.permissions || []
  const permLabels: Record<string, string> = {
    select: 'SELECT (read data)',
    insert: 'INSERT (add rows)',
    update: 'UPDATE (modify rows)',
    delete: 'DELETE (remove rows)',
    create: 'CREATE (new tables/indexes)',
    alter: 'ALTER (modify structure)',
    drop: 'DROP (delete tables/indexes)'
  }
  
  return perms.map(p => permLabels[p] || p).join(', ') || 'None'
})
</script>

<template>
  <NTooltip>
    <template #trigger>
      <NTag :type="permissionLevel.type" :size="size" :bordered="false">
        <span v-if="size !== 'small'" style="margin-right: 4px">{{ permissionLevel.icon }}</span>
        {{ permissionLevel.label }}
      </NTag>
    </template>
    <div style="max-width: 300px">
      <div style="font-weight: 600; margin-bottom: 4px">{{ permissionLevel.description }}</div>
      <div style="font-size: 0.85rem; opacity: 0.9">
        <strong>Allowed operations:</strong><br>
        {{ permissionDetails }}
      </div>
    </div>
  </NTooltip>
</template>
