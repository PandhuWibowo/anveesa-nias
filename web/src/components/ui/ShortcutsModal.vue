<script setup lang="ts">
defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()

const groups = [
  {
    title: 'Query Editor',
    shortcuts: [
      { keys: ['Ctrl', 'Enter'], desc: 'Run query' },
      { keys: ['Mod', 'Enter'], desc: 'Run query (Mac)' },
      { keys: ['Ctrl', 'Space'], desc: 'Show autocomplete' },
      { keys: ['Mod', 'Space'], desc: 'Show autocomplete (Mac)' },
      { keys: ['Alt', '/'], desc: 'Fallback autocomplete' },
      { keys: ['Ctrl', 'Shift', 'F'], desc: 'Format SQL' },
      { keys: ['Tab'], desc: 'Indent / accept completion' },
      { keys: ['Ctrl', 'Z'], desc: 'Undo' },
      { keys: ['Ctrl', 'Shift', 'Z'], desc: 'Redo' },
      { keys: ['Ctrl', '/'], desc: 'Toggle comment' },
    ],
  },
  {
    title: 'Tabs',
    shortcuts: [
      { keys: ['Ctrl', 'T'], desc: 'New query tab' },
      { keys: ['Ctrl', 'W'], desc: 'Close active tab' },
    ],
  },
  {
    title: 'Navigation',
    shortcuts: [
      { keys: ['?'], desc: 'Show keyboard shortcuts' },
      { keys: ['Esc'], desc: 'Close modal / panel' },
    ],
  },
  {
    title: 'Results',
    shortcuts: [
      { keys: ['Ctrl', 'E'], desc: 'Export CSV' },
      { keys: ['Ctrl', 'Shift', 'E'], desc: 'Export JSON' },
    ],
  },
]
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="km-overlay" @click.self="emit('close')">
      <div class="km-modal">
        <div class="km-header">
          <span class="km-title">Keyboard Shortcuts</span>
          <button class="km-close" @click="emit('close')">×</button>
        </div>
        <div class="km-body">
          <div v-for="g in groups" :key="g.title" class="km-group">
            <div class="km-group-title">{{ g.title }}</div>
            <div class="km-rows">
              <div v-for="s in g.shortcuts" :key="s.desc" class="km-row">
                <span class="km-desc">{{ s.desc }}</span>
                <div class="km-keys">
                  <kbd v-for="k in s.keys" :key="k" class="km-kbd">{{ k }}</kbd>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.km-overlay {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center;
  z-index: 1100;
}
.km-modal {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 10px;
  width: min(600px, 92vw);
  max-height: 80vh;
  display: flex; flex-direction: column;
  box-shadow: 0 24px 64px rgba(0,0,0,0.5);
}
.km-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 20px;
  border-bottom: 1px solid var(--border);
}
.km-title { font-size: 14px; font-weight: 700; color: var(--text-primary); }
.km-close {
  background: transparent; border: none;
  color: var(--text-muted); cursor: pointer;
  font-size: 20px; line-height: 1; padding: 0 4px;
  transition: color 0.15s;
}
.km-close:hover { color: var(--text-primary); }
.km-body {
  flex: 1; min-height: 0; overflow-y: auto;
  padding: 16px 20px;
  display: grid; grid-template-columns: 1fr 1fr; gap: 20px;
}
.km-group-title {
  font-size: 10.5px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.6px; color: var(--brand); margin-bottom: 10px;
}
.km-rows { display: flex; flex-direction: column; gap: 6px; }
.km-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: 12px;
}
.km-desc { font-size: 12.5px; color: var(--text-secondary); }
.km-keys { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
.km-kbd {
  display: inline-flex; align-items: center; justify-content: center;
  padding: 2px 6px;
  background: var(--bg-surface);
  border: 1px solid var(--border-2);
  border-bottom-width: 2px;
  border-radius: 4px;
  font-size: 10.5px; font-family: inherit;
  color: var(--text-secondary);
  min-width: 22px;
}
</style>
