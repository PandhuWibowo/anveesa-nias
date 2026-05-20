<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { readableError } from '@/utils/httpError'

const toast = useToast()

interface User {
  id: number
  username: string
  role: string
  role_id: number
  is_active: boolean
  created_at: string
}

interface Role {
  id: number
  name: string
  description: string
  is_system: boolean
  is_active: boolean
}

const users = ref<User[]>([])
const roles = ref<Role[]>([])
const loading = ref(false)
const error = ref('')

async function loadUsers() {
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

async function loadRoles() {
  try {
    const { data } = await axios.get<Role[]>('/api/roles')
    roles.value = data ?? []
  } catch (e: any) {
    console.error('Failed to load roles:', e)
  }
}

onMounted(async () => {
  await Promise.all([loadUsers(), loadRoles()])
})

// ── Filter ──────────────────────────────────────────────────────
const filterSearch = ref('')
const filterRole = ref('')
const filterStatus = ref('')

const filteredUsers = computed(() => {
  let list = users.value
  if (filterSearch.value.trim()) {
    const q = filterSearch.value.toLowerCase()
    list = list.filter(u => u.username.toLowerCase().includes(q))
  }
  if (filterRole.value) {
    list = list.filter(u => u.role === filterRole.value)
  }
  if (filterStatus.value) {
    const active = filterStatus.value === 'active'
    list = list.filter(u => u.is_active === active)
  }
  return list
})

const availableRoles = computed(() => [...new Set(users.value.map(u => u.role))].sort())

function clearFilters() {
  filterSearch.value = ''
  filterRole.value = ''
  filterStatus.value = ''
}

const hasActiveFilters = computed(() => filterSearch.value || filterRole.value || filterStatus.value)

// ── Sort ────────────────────────────────────────────────────────
type SortKey = 'id' | 'username' | 'role' | 'is_active' | 'created_at'
const sortKey = ref<SortKey>('id')
const sortDir = ref<'asc' | 'desc'>('asc')

function setSort(key: SortKey) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortKey.value = key
    sortDir.value = 'asc'
  }
}

const sortedUsers = computed(() => {
  const list = [...filteredUsers.value]
  list.sort((a, b) => {
    let av: any = a[sortKey.value]
    let bv: any = b[sortKey.value]
    if (typeof av === 'string') av = av.toLowerCase()
    if (typeof bv === 'string') bv = bv.toLowerCase()
    if (av < bv) return sortDir.value === 'asc' ? -1 : 1
    if (av > bv) return sortDir.value === 'asc' ? 1 : -1
    return 0
  })
  return list
})

// ── Pagination ──────────────────────────────────────────────────
const pageSize = ref(10)
const currentPage = ref(1)

const totalPages = computed(() => Math.max(1, Math.ceil(sortedUsers.value.length / pageSize.value)))

const pagedUsers = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return sortedUsers.value.slice(start, start + pageSize.value)
})

function setPage(p: number) {
  currentPage.value = Math.max(1, Math.min(p, totalPages.value))
}

// Reset to first page when filters change
function resetPage() { currentPage.value = 1 }

// ── Custom fields (column visibility) ───────────────────────────
const visibleColumns = ref({
  id: true,
  username: true,
  role: true,
  is_active: true,
  created_at: true,
})
const showColumnMenu = ref(false)

const columnDefs = [
  { key: 'id' as const, label: 'ID' },
  { key: 'username' as const, label: 'Username' },
  { key: 'role' as const, label: 'Role' },
  { key: 'is_active' as const, label: 'Status' },
  { key: 'created_at' as const, label: 'Created' },
]

// ── Edit modal ──────────────────────────────────────────────────
const editTarget = ref<User | null>(null)
const editRoleId = ref<number | null>(null)
const editPassword = ref('')
const editActive = ref(true)
const editSaving = ref(false)

function openEdit(u: User) {
  editTarget.value = u
  editRoleId.value = u.role_id || (roles.value.find((role) => role.name === u.role)?.id ?? null)
  editPassword.value = ''
  editActive.value = u.is_active
}

async function saveEdit() {
  if (!editTarget.value || !editRoleId.value) return
  editSaving.value = true
  try {
    await axios.put(`/api/admin/users/${editTarget.value.id}`, {
      role_id: editRoleId.value,
      is_active: editActive.value,
      password: editPassword.value || undefined,
    })
    editTarget.value = null
    await loadUsers()
  } catch (e) {
    toast.error(readableError(e, { action: 'Save user', fallback: 'Failed to save user' }))
  } finally {
    editSaving.value = false
  }
}

async function deleteUser(u: User) {
  if (!confirm(`Delete user "${u.username}"?`)) return
  await axios.delete(`/api/admin/users/${u.id}`)
  await loadUsers()
}

const roleColors: Record<string, string> = {
  admin: '#f59e0b',
  poweruser: '#a855f7',
  viewer: '#60a5fa',
  editor: '#4ade80',
  user: '#94a3b8',
}

function sortIcon(key: SortKey) {
  if (sortKey.value !== key) return 'none'
  return sortDir.value === 'asc' ? 'asc' : 'desc'
}
</script>

<template>
  <div class="page-shell u-root" @click="showColumnMenu = false">
    <div class="page-scroll u-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Administration</div>
            <div class="page-title">User Management</div>
            <div class="page-subtitle">Manage user accounts, adjust access roles, and keep the operator roster under control.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadUsers">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <section class="page-panel u-table-wrap">
          <div class="u-panel-head">
            <div>
              <div class="u-panel-title">Accounts</div>
              <div class="u-panel-sub">
                {{ filteredUsers.length }} of {{ users.length }} users
              </div>
            </div>
          </div>

          <!-- Filter bar -->
          <div class="u-filter-bar">
            <div class="u-filter-search">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
              <input
                class="u-filter-input"
                v-model="filterSearch"
                placeholder="Search username…"
                @input="resetPage"
              />
            </div>
            <select class="u-filter-select" v-model="filterRole" @change="resetPage">
              <option value="">All roles</option>
              <option v-for="r in availableRoles" :key="r" :value="r">{{ r }}</option>
            </select>
            <select class="u-filter-select" v-model="filterStatus" @change="resetPage">
              <option value="">All status</option>
              <option value="active">Active</option>
              <option value="locked">Locked</option>
            </select>
            <button v-if="hasActiveFilters" class="u-filter-clear" @click="clearFilters(); resetPage()">Clear</button>

            <!-- Column visibility -->
            <div class="u-col-toggle" @click.stop>
              <button class="base-btn base-btn--ghost base-btn--xs u-col-btn" @click="showColumnMenu = !showColumnMenu">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18"/></svg>
                Columns
              </button>
              <div v-if="showColumnMenu" class="u-col-menu">
                <div class="u-col-menu-title">Visible Columns</div>
                <label v-for="col in columnDefs" :key="col.key" class="u-col-item">
                  <input type="checkbox" v-model="visibleColumns[col.key]" />
                  {{ col.label }}
                </label>
              </div>
            </div>

            <!-- Page size -->
            <div class="u-pagesize">
              <select class="u-filter-select" v-model="pageSize" @change="resetPage">
                <option :value="10">10 / page</option>
                <option :value="25">25 / page</option>
                <option :value="50">50 / page</option>
                <option :value="100">100 / page</option>
              </select>
            </div>
          </div>

          <div v-if="error" class="notice notice--error u-error">{{ error }}</div>

          <div v-if="loading" class="u-loading">
            <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          </div>
          <table v-else class="u-table">
            <thead>
              <tr>
                <th v-if="visibleColumns.id" @click="setSort('id')" class="u-th-sort">
                  ID
                  <span class="u-sort-icon" :class="{ 'u-sort-active': sortKey === 'id' }">
                    {{ sortIcon('id') === 'asc' ? '↑' : sortIcon('id') === 'desc' ? '↓' : '↕' }}
                  </span>
                </th>
                <th v-if="visibleColumns.username" @click="setSort('username')" class="u-th-sort">
                  Username
                  <span class="u-sort-icon" :class="{ 'u-sort-active': sortKey === 'username' }">
                    {{ sortIcon('username') === 'asc' ? '↑' : sortIcon('username') === 'desc' ? '↓' : '↕' }}
                  </span>
                </th>
                <th v-if="visibleColumns.role" @click="setSort('role')" class="u-th-sort">
                  Role
                  <span class="u-sort-icon" :class="{ 'u-sort-active': sortKey === 'role' }">
                    {{ sortIcon('role') === 'asc' ? '↑' : sortIcon('role') === 'desc' ? '↓' : '↕' }}
                  </span>
                </th>
                <th v-if="visibleColumns.is_active" @click="setSort('is_active')" class="u-th-sort">
                  Status
                  <span class="u-sort-icon" :class="{ 'u-sort-active': sortKey === 'is_active' }">
                    {{ sortIcon('is_active') === 'asc' ? '↑' : sortIcon('is_active') === 'desc' ? '↓' : '↕' }}
                  </span>
                </th>
                <th v-if="visibleColumns.created_at" @click="setSort('created_at')" class="u-th-sort">
                  Created
                  <span class="u-sort-icon" :class="{ 'u-sort-active': sortKey === 'created_at' }">
                    {{ sortIcon('created_at') === 'asc' ? '↑' : sortIcon('created_at') === 'desc' ? '↓' : '↕' }}
                  </span>
                </th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in pagedUsers" :key="u.id">
                <td v-if="visibleColumns.id" class="u-td-dim">{{ u.id }}</td>
                <td v-if="visibleColumns.username" class="u-td-name">{{ u.username }}</td>
                <td v-if="visibleColumns.role">
                  <span class="u-role" :style="{ color: roleColors[u.role] ?? 'var(--text-muted)', borderColor: roleColors[u.role] ?? 'var(--border)' }">
                    {{ u.role }}
                  </span>
                </td>
                <td v-if="visibleColumns.is_active" class="u-td-dim">{{ u.is_active ? 'Active' : 'Locked' }}</td>
                <td v-if="visibleColumns.created_at" class="u-td-dim">{{ new Date(u.created_at).toLocaleDateString() }}</td>
                <td>
                  <div class="u-row-actions">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="openEdit(u)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--xs u-btn-del" @click="deleteUser(u)">Delete</button>
                  </div>
                </td>
              </tr>
              <tr v-if="pagedUsers.length === 0">
                <td :colspan="Object.values(visibleColumns).filter(Boolean).length + 1" class="u-empty">
                  {{ hasActiveFilters ? 'No users match the current filters' : 'No users found' }}
                </td>
              </tr>
            </tbody>
          </table>

          <!-- Pagination -->
          <div v-if="!loading && totalPages > 1" class="u-pagination">
            <span class="u-page-info">
              {{ (currentPage - 1) * pageSize + 1 }}–{{ Math.min(currentPage * pageSize, sortedUsers.length) }} of {{ sortedUsers.length }}
            </span>
            <div class="u-page-controls">
              <button class="u-page-btn" :disabled="currentPage === 1" @click="setPage(1)">«</button>
              <button class="u-page-btn" :disabled="currentPage === 1" @click="setPage(currentPage - 1)">‹</button>
              <template v-for="p in totalPages" :key="p">
                <button
                  v-if="Math.abs(p - currentPage) <= 2 || p === 1 || p === totalPages"
                  class="u-page-btn"
                  :class="{ 'u-page-btn--active': p === currentPage }"
                  @click="setPage(p)"
                >{{ p }}</button>
                <span v-else-if="Math.abs(p - currentPage) === 3" class="u-page-ellipsis">…</span>
              </template>
              <button class="u-page-btn" :disabled="currentPage === totalPages" @click="setPage(currentPage + 1)">›</button>
              <button class="u-page-btn" :disabled="currentPage === totalPages" @click="setPage(totalPages)">»</button>
            </div>
          </div>
        </section>
      </div>
    </div>

    <!-- Edit modal -->
    <Teleport to="body">
      <div v-if="editTarget" class="u-overlay" @click.self="editTarget=null">
        <div class="u-dialog">
          <div class="u-dialog-title">Edit User: <strong>{{ editTarget.username }}</strong></div>

          <label class="u-label">Role</label>
          <select class="u-select" v-model="editRoleId">
            <option v-for="role in roles" :key="role.id" :value="role.id">
              {{ role.name }}{{ role.is_system ? ' (system)' : '' }}
            </option>
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
.u-table-wrap { overflow: hidden; }
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

/* Filter bar */
.u-filter-bar {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 20px 12px;
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
}
.u-filter-search {
  display: flex; align-items: center; gap: 6px;
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 5px; padding: 5px 10px; flex: 1; min-width: 180px;
}
.u-filter-search svg { color: var(--text-muted); flex-shrink: 0; }
.u-filter-input {
  border: none; outline: none; background: transparent;
  color: var(--text-primary); font-size: 13px; width: 100%;
  font-family: inherit;
}
.u-filter-input::placeholder { color: var(--text-muted); }
.u-filter-select {
  padding: 5px 28px 5px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--bg-body); color: var(--text-primary);
  font-size: 12px; font-family: inherit; outline: none; cursor: pointer;
  appearance: auto;
}
.u-filter-clear {
  padding: 5px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: transparent; color: var(--text-muted);
  font-size: 12px; cursor: pointer; font-family: inherit;
  transition: color 0.15s, border-color 0.15s;
}
.u-filter-clear:hover { color: var(--danger); border-color: var(--danger); }

/* Sort */
.u-th-sort { cursor: pointer; user-select: none; white-space: nowrap; }
.u-th-sort:hover { color: var(--text-primary); }
.u-sort-icon { margin-left: 4px; font-size: 10px; color: var(--text-muted); opacity: 0.5; }
.u-sort-icon.u-sort-active { opacity: 1; color: var(--brand); }

/* Column toggle */
.u-col-toggle { position: relative; }
.u-col-btn { gap: 5px; }
.u-col-menu {
  position: absolute; top: calc(100% + 6px); right: 0;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; padding: 10px 12px; min-width: 160px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3); z-index: 100;
}
.u-col-menu-title {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.1em; color: var(--text-muted); margin-bottom: 8px;
}
.u-col-item {
  display: flex; align-items: center; gap: 8px;
  font-size: 13px; color: var(--text-primary);
  padding: 4px 0; cursor: pointer;
}
.u-col-item input { cursor: pointer; }

/* Pagination */
.u-pagination {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 20px; border-top: 1px solid var(--border);
  flex-wrap: wrap; gap: 8px;
}
.u-page-info { font-size: 12px; color: var(--text-muted); }
.u-page-controls { display: flex; align-items: center; gap: 3px; }
.u-page-btn {
  min-width: 30px; height: 30px; padding: 0 6px;
  border: 1px solid var(--border); border-radius: 5px;
  background: transparent; color: var(--text-primary);
  font-size: 12px; cursor: pointer; font-family: inherit;
  transition: background 0.12s, border-color 0.12s;
}
.u-page-btn:hover:not(:disabled) { background: rgba(255,255,255,0.06); border-color: var(--brand); }
.u-page-btn:disabled { opacity: 0.35; cursor: default; }
.u-page-btn--active { background: var(--brand) !important; border-color: var(--brand) !important; color: #fff !important; }
.u-page-ellipsis { padding: 0 4px; color: var(--text-muted); font-size: 12px; }
.u-pagesize { margin-left: auto; }

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
