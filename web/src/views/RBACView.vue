<template>
  <div class="rbac-root">
    <div class="rbac-header">
      <h2>Permission Matrix</h2>
      <button class="rbac-btn rbac-btn--primary" @click="showForm = true">+ Grant Permission</button>
    </div>

    <div class="rbac-desc">
      Control what each user can do per connection.
      <strong>readonly</strong> = SELECT only &nbsp;|&nbsp;
      <strong>readwrite</strong> = SELECT + DML &nbsp;|&nbsp;
      <strong>admin</strong> = all including DDL.
    </div>

    <div class="rbac-table-wrap">
      <table class="rbac-table">
        <thead>
          <tr>
            <th>User</th>
            <th>Connection</th>
            <th>Level</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in permissions" :key="p.id">
            <td>{{ p.username || `#${p.user_id}` }}</td>
            <td>{{ p.conn_name || 'All connections' }}</td>
            <td>
              <span :class="['rbac-badge', 'rbac-badge--' + p.level]">{{ p.level }}</span>
            </td>
            <td>
              <select :value="p.level" @change="changeLevel(p, ($event.target as HTMLSelectElement).value)"
                class="rbac-select">
                <option value="readonly">readonly</option>
                <option value="readwrite">readwrite</option>
                <option value="admin">admin</option>
              </select>
              <button class="rbac-del" @click="deletePermission(p.id)" title="Revoke">✕</button>
            </td>
          </tr>
          <tr v-if="permissions.length === 0">
            <td colspan="4" class="rbac-empty">No permissions configured — all authenticated users have read-write access by default.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Grant form modal -->
    <div v-if="showForm" class="rbac-modal-overlay" @click.self="showForm = false">
      <div class="rbac-modal">
        <div class="rbac-modal-header">
          <span>Grant Permission</span>
          <button class="rbac-modal-close" @click="showForm = false">✕</button>
        </div>
        <div class="rbac-form">
          <label>User</label>
          <select v-model="form.user_id" class="rbac-input">
            <option value="">Select user…</option>
            <option v-for="u in users" :key="u.id" :value="u.id">{{ u.username }}</option>
          </select>
          <label>Connection</label>
          <select v-model="form.conn_id" class="rbac-input">
            <option :value="-1">All connections</option>
            <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
          <label>Level</label>
          <select v-model="form.level" class="rbac-input">
            <option value="readonly">readonly</option>
            <option value="readwrite">readwrite</option>
            <option value="admin">admin</option>
          </select>
          <div class="rbac-form-actions">
            <button class="rbac-btn" @click="showForm = false">Cancel</button>
            <button class="rbac-btn rbac-btn--primary" @click="grantPermission">Grant</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'

interface PermissionView {
  id: number
  user_id: number
  conn_id: number
  level: string
  username: string
  conn_name: string
}
interface User { id: number; username: string }
interface Conn { id: number; name: string }

const permissions = ref<PermissionView[]>([])
const users = ref<User[]>([])
const connections = ref<Conn[]>([])
const showForm = ref(false)
const form = ref({ user_id: '' as string | number, conn_id: -1 as number, level: 'readonly' })

onMounted(() => {
  load()
  loadUsers()
  loadConnections()
})

async function load() {
  const { data } = await axios.get('/api/permissions')
  permissions.value = data
}
async function loadUsers() {
  try { const { data } = await axios.get('/api/admin/users'); users.value = data } catch {}
}
async function loadConnections() {
  try { const { data } = await axios.get('/api/connections'); connections.value = data } catch {}
}

async function grantPermission() {
  if (!form.value.user_id) return
  await axios.post('/api/permissions', form.value)
  showForm.value = false
  form.value = { user_id: '', conn_id: -1, level: 'readonly' }
  await load()
}

async function changeLevel(p: PermissionView, level: string) {
  await axios.post('/api/permissions', { user_id: p.user_id, conn_id: p.conn_id, level })
  await load()
}

async function deletePermission(id: number) {
  await axios.delete(`/api/permissions/${id}`)
  await load()
}
</script>

<style scoped>
.rbac-root { padding: 24px; max-width: 880px; display: flex; flex-direction: column; gap: 16px; }
.rbac-header { display: flex; align-items: center; justify-content: space-between; }
.rbac-header h2 { font-size: 20px; font-weight: 700; color: var(--text-primary); margin: 0; }
.rbac-desc { font-size: 13px; color: var(--text-muted); background: var(--bg-panel); border: 1px solid var(--border); border-radius: 8px; padding: 10px 14px; }
.rbac-table-wrap { overflow-x: auto; }
.rbac-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.rbac-table th { background: var(--bg-sidebar); padding: 8px 12px; text-align: left; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); }
.rbac-table td { padding: 8px 12px; border-bottom: 1px solid var(--border); color: var(--text-primary); }
.rbac-badge { font-size: 11px; font-weight: 700; padding: 2px 8px; border-radius: 4px; text-transform: uppercase; letter-spacing: .3px; }
.rbac-badge--readonly  { background: rgba(79,156,249,.15); color: #4f9cf9; }
.rbac-badge--readwrite { background: rgba(86,196,144,.15); color: #56c490; }
.rbac-badge--admin     { background: rgba(249,127,79,.15); color: #f97f4f; }
.rbac-select { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); font-size: 12px; padding: 2px 6px; margin-right: 6px; }
.rbac-del { background: none; border: none; color: var(--text-muted); cursor: pointer; padding: 2px 6px; border-radius: 4px; font-size: 13px; }
.rbac-del:hover { background: rgba(249,127,79,.15); color: #f97f4f; }
.rbac-empty { text-align: center; padding: 20px; color: var(--text-muted); font-size: 13px; }
.rbac-btn { padding: 7px 16px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg-panel); color: var(--text-primary); font-size: 13px; cursor: pointer; }
.rbac-btn--primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.rbac-modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,.6); z-index: 1000; display: flex; align-items: center; justify-content: center; }
.rbac-modal { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 10px; width: 380px; }
.rbac-modal-header { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--border); font-weight: 600; font-size: 14px; color: var(--text-primary); }
.rbac-modal-close { background: none; border: none; color: var(--text-muted); cursor: pointer; font-size: 16px; }
.rbac-form { display: flex; flex-direction: column; gap: 10px; padding: 16px; }
.rbac-form label { font-size: 12px; color: var(--text-muted); }
.rbac-input { background: var(--bg-sidebar); border: 1px solid var(--border); border-radius: 6px; color: var(--text-primary); font-size: 13px; padding: 6px 10px; }
.rbac-form-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px; }
</style>
