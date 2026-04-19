<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'

interface User {
  id: number
  username: string
  role: string
  is_active: boolean
  created_at: string
}

const users = ref<User[]>([])
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await axios.get<User[]>('/api/admin/users')
    users.value = data ?? []
  } catch (e: any) {
    error.value = e?.response?.data?.error ?? 'Failed to load users'
  } finally {
    loading.value = false
  }
}

onMounted(load)

// Edit modal
const editTarget = ref<User | null>(null)
const editRole = ref('')
const editPassword = ref('')
const editActive = ref(true)
const editSaving = ref(false)

function openEdit(u: User) {
  editTarget.value = u
  editRole.value = u.role
  editPassword.value = ''
  editActive.value = u.is_active
}

async function saveEdit() {
  if (!editTarget.value) return
  editSaving.value = true
  try {
    await axios.put(`/api/admin/users/${editTarget.value.id}`, {
      role: editRole.value,
      is_active: editActive.value,
      password: editPassword.value || undefined,
    })
    editTarget.value = null
    await load()
  } finally {
    editSaving.value = false
  }
}

async function deleteUser(u: User) {
  if (!confirm(`Delete user "${u.username}"?`)) return
  await axios.delete(`/api/admin/users/${u.id}`)
  await load()
}

const roleColors: Record<string, string> = {
  admin: '#f59e0b',
  viewer: '#60a5fa',
  editor: '#4ade80',
}
</script>

<template>
  <div class="page-shell u-root">
    <div class="page-scroll u-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Administration</div>
            <div class="page-title">User Management</div>
            <div class="page-subtitle">Manage user accounts, adjust access roles, and keep the operator roster under control.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="load">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <section class="page-panel u-table-wrap">
          <div class="u-panel-head">
            <div>
              <div class="u-panel-title">Accounts</div>
              <div class="u-panel-sub">{{ users.length }} total users</div>
            </div>
          </div>

          <div v-if="error" class="notice notice--error u-error">{{ error }}</div>

          <div v-if="loading" class="u-loading">
            <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          </div>
          <table v-else class="u-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Username</th>
                <th>Role</th>
                <th>Status</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id">
                <td class="u-td-dim">{{ u.id }}</td>
                <td class="u-td-name">{{ u.username }}</td>
                <td>
                  <span class="u-role" :style="{ color: roleColors[u.role] ?? 'var(--text-muted)', borderColor: roleColors[u.role] ?? 'var(--border)' }">
                    {{ u.role }}
                  </span>
                </td>
                <td class="u-td-dim">{{ u.is_active ? 'Active' : 'Locked' }}</td>
                <td class="u-td-dim">{{ new Date(u.created_at).toLocaleDateString() }}</td>
                <td>
                  <div class="u-row-actions">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openEdit(u)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs u-btn-del" @click="deleteUser(u)">Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="users.length === 0">
                <td colspan="6" class="u-empty">No users found</td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>
    </div>
    
    <!-- Edit modal -->
    <Teleport to="body">
      <div v-if="editTarget" class="u-overlay" @click.self="editTarget=null">
        <div class="u-dialog">
          <div class="u-dialog-title">Edit User: <strong>{{ editTarget.username }}</strong></div>

          <label class="u-label">Role</label>
          <select class="u-select" v-model="editRole">
            <option value="admin">admin</option>
            <option value="editor">editor</option>
            <option value="viewer">viewer</option>
          </select>

          <label class="u-label" style="margin-top:12px">New Password <span style="color:var(--text-muted);font-weight:400">(leave blank to keep)</span></label>
          <input class="u-input" type="password" v-model="editPassword" placeholder="New password…" autocomplete="new-password" />

          <label class="u-label" style="margin-top:12px">Account Status</label>
          <select class="u-select" v-model="editActive">
            <option :value="true">Active</option>
            <option :value="false">Locked</option>
          </select>

          <div class="u-dialog-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="editTarget=null">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveEdit" :disabled="editSaving">
              {{ editSaving ? 'Saving…' : 'Save Changes' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.u-root { background: var(--bg-body); }
.u-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 20px 10px;
}
.u-panel-title { font-size: 15px; font-weight: 700; color: var(--text-primary); }
.u-panel-sub { margin-top: 4px; font-size: 12px; color: var(--text-muted); }
.u-error { margin: 0 20px 12px; }
.u-loading {
  display: flex; align-items: center; justify-content: center;
  padding: 40px; color: var(--text-muted);
}
.u-table-wrap {
  overflow: hidden;
}
.u-table {
  width: 100%; border-collapse: collapse; font-size: 13px;
}
.u-table th {
  background: rgba(255, 255, 255, 0.02); padding: 11px 18px;
  border-bottom: 1px solid var(--border);
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.12em; color: var(--text-muted); text-align: left;
}
.u-table td {
  padding: 12px 18px; border-bottom: 1px solid var(--border);
  color: var(--text-primary);
}
.u-table tr:last-child td { border-bottom: none; }
.u-table tr:hover td { background: rgba(255, 255, 255, 0.03); }
.u-td-dim { color: var(--text-muted); font-size: 12px; }
.u-td-name { font-weight: 600; }
.u-role {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  padding: 4px 10px; border-radius: 999px; border: 1px solid;
  letter-spacing: 0.12em;
}
.u-row-actions { display: flex; gap: 6px; }
.u-btn-del { color: var(--danger) !important; }
.u-btn-del:hover { background: rgba(239, 68, 68, 0.1) !important; }
.u-empty { text-align:center;color:var(--text-muted);font-size:13px;padding:24px; }

/* Dialog */
.u-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center; z-index: 1000;
}
.u-dialog {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 8px; width: min(400px, 90vw); padding: 24px;
  box-shadow: 0 20px 60px rgba(0,0,0,0.5);
}
.u-dialog-title { font-size: 15px; color: var(--text-primary); margin-bottom: 16px; }
.u-label {
  display: block; font-size: 11px; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.4px;
  color: var(--text-muted); margin-bottom: 5px;
}
.u-select, .u-input {
  width: 100%; padding: 7px 10px;
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 5px; color: var(--text-primary);
  font-size: 13px; font-family: inherit;
  box-sizing: border-box; outline: none;
  transition: border-color 0.15s;
}
.u-select:focus, .u-input:focus { border-color: var(--brand); }
.u-dialog-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px; }
</style>
