<script setup lang="ts">
import { useConfirm } from '@/composables/useConfirm'

const { state, answer } = useConfirm()
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="state.open" class="modal-backdrop" @click.self="answer(false)">
        <div class="modal modal--sm">
          <div class="modal-hd">
            <span class="modal-title">{{ state.title }}</span>
            <button class="icon-btn" @click="answer(false)">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-bd">
            <p style="font-size:13px;color:var(--text-secondary);line-height:1.6">{{ state.message }}</p>
          </div>
          <div class="modal-ft">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="answer(false)">Cancel</button>
            <button class="base-btn base-btn--danger base-btn--sm" style="background:var(--danger);color:#fff;border-color:var(--danger)" @click="answer(true)">Confirm</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
