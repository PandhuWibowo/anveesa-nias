<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import QrcodeVue from 'qrcode.vue'

const toast = useToast()

const loading = ref(false)
const enabled = ref(false)
const backupCodesCount = ref(0)

// Setup state
const showSetup = ref(false)
const setupData = ref<{ secret: string; qr_code: string; backup_codes: string[] } | null>(null)
const verifyCode = ref('')
const setupLoading = ref(false)

// Disable state
const showDisable = ref(false)
const disablePassword = ref('')
const disableBackupCode = ref('')
const disableLoading = ref(false)

async function fetchStatus() {
  loading.value = true
  try {
    const { data } = await axios.get('/api/auth/2fa/status')
    enabled.value = data.enabled
    backupCodesCount.value = data.backup_codes_count || 0
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load 2FA status')
  } finally {
    loading.value = false
  }
}

async function startSetup() {
  setupLoading.value = true
  try {
    const { data } = await axios.post('/api/auth/2fa/setup')
    setupData.value = data
    showSetup.value = true
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to setup 2FA')
  } finally {
    setupLoading.value = false
  }
}

async function enableTwoFA() {
  if (!verifyCode.value || verifyCode.value.length !== 6) {
    toast.error('Please enter a valid 6-digit code')
    return
  }

  setupLoading.value = true
  try {
    await axios.post('/api/auth/2fa/enable', { code: verifyCode.value })
    toast.success('2FA enabled successfully!')
    showSetup.value = false
    setupData.value = null
    verifyCode.value = ''
    await fetchStatus()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Invalid code')
  } finally {
    setupLoading.value = false
  }
}

async function disableTwoFA() {
  if (!disablePassword.value && !disableBackupCode.value) {
    toast.error('Please enter your password or a backup code')
    return
  }

  disableLoading.value = true
  try {
    await axios.post('/api/auth/2fa/disable', {
      password: disablePassword.value,
      backup_code: disableBackupCode.value,
    })
    toast.success('2FA disabled successfully')
    showDisable.value = false
    disablePassword.value = ''
    disableBackupCode.value = ''
    await fetchStatus()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to disable 2FA')
  } finally {
    disableLoading.value = false
  }
}

function downloadBackupCodes() {
  if (!setupData.value) return
  const text = `Singapay SQL - Backup Codes\n\n${setupData.value.backup_codes.join('\n')}\n\nKeep these codes safe! Each can only be used once.`
  const blob = new Blob([text], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'singapay-sql-backup-codes.txt'
  a.click()
  URL.revokeObjectURL(url)
  toast.success('Backup codes downloaded')
}

onMounted(fetchStatus)
</script>

<template>
  <div class="page-shell sec-view">
    <div class="page-scroll sec-scroll">
      <div class="page-stack">
      <section class="page-hero">
        <div class="page-hero__content">
          <div class="page-kicker">Account Security</div>
          <div class="page-title">Security Settings</div>
          <div class="page-subtitle">Manage two-factor authentication, backup codes, and the extra safeguards around your login flow.</div>
        </div>
      </section>

      <!-- Loading -->
      <div v-if="loading" class="sec-loading">
        <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M21 12a9 9 0 1 1-6.219-8.56"/>
        </svg>
        Loading...
      </div>

      <!-- 2FA Card -->
      <div v-else class="page-card sec-card">
        <div class="sec-card-header">
          <div class="sec-card-icon" :class="enabled ? 'sec-card-icon--enabled' : 'sec-card-icon--disabled'">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
          </div>
          <div class="sec-card-meta">
            <h3 class="sec-card-title">Two-Factor Authentication</h3>
            <p class="sec-card-desc">Add an extra layer of security to your account</p>
          </div>
          <div class="sec-card-status">
            <span class="sec-badge" :class="enabled ? 'sec-badge--enabled' : 'sec-badge--disabled'">
              {{ enabled ? 'Enabled' : 'Disabled' }}
            </span>
          </div>
        </div>

        <div class="sec-card-body">
          <div v-if="enabled" class="sec-info">
            <div class="sec-info-row">
              <span class="sec-info-label">Status:</span>
              <span class="sec-info-value">Active</span>
            </div>
            <div class="sec-info-row">
              <span class="sec-info-label">Backup codes remaining:</span>
              <span class="sec-info-value">{{ backupCodesCount }}</span>
            </div>
          </div>

          <div v-else class="sec-help">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--brand);flex-shrink:0">
              <circle cx="12" cy="12" r="10"/>
              <path d="M12 16v-4M12 8h.01"/>
            </svg>
            <p>Two-factor authentication adds an extra layer of security. You'll need your password and a code from your authenticator app to sign in.</p>
          </div>
        </div>

        <div class="sec-card-footer">
          <button v-if="!enabled" class="base-btn base-btn--primary" @click="startSetup" :disabled="setupLoading">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
            {{ setupLoading ? 'Setting up...' : 'Enable 2FA' }}
          </button>
          <button v-else class="base-btn base-btn--ghost" style="color:#f87171" @click="showDisable = true">
            Disable 2FA
          </button>
        </div>
      </div>
      </div>
    </div>

    <!-- Setup Modal -->
    <Teleport to="body">
      <div v-if="showSetup && setupData" class="sec-modal-overlay" @click.self="showSetup = false">
        <div class="page-modal sec-modal">
          <div class="sec-modal-header">
            <h2 class="sec-modal-title">Enable Two-Factor Authentication</h2>
            <button class="sec-modal-close" @click="showSetup = false">×</button>
          </div>

          <div class="sec-modal-body">
            <!-- Step 1: Scan QR -->
            <div class="sec-step">
              <div class="sec-step-num">1</div>
              <div class="sec-step-content">
                <h4 class="sec-step-title">Scan QR Code</h4>
                <p class="sec-step-desc">Use Google Authenticator, Authy, or any TOTP app</p>
                <div class="sec-qr-wrap">
                  <QrcodeVue :value="setupData.qr_code" :size="200" level="H" />
                </div>
                <div class="sec-secret">
                  <span class="sec-secret-label">Manual entry key:</span>
                  <code class="sec-secret-code">{{ setupData.secret }}</code>
                </div>
              </div>
            </div>

            <!-- Step 2: Verify -->
            <div class="sec-step">
              <div class="sec-step-num">2</div>
              <div class="sec-step-content">
                <h4 class="sec-step-title">Verify Code</h4>
                <p class="sec-step-desc">Enter the 6-digit code from your app</p>
                <input
                  v-model="verifyCode"
                  class="base-input sec-code-input"
                  type="text"
                  placeholder="000000"
                  maxlength="6"
                  inputmode="numeric"
                  pattern="[0-9]*"
                />
              </div>
            </div>

            <!-- Step 3: Backup Codes -->
            <div class="sec-step">
              <div class="sec-step-num">3</div>
              <div class="sec-step-content">
                <h4 class="sec-step-title">Save Backup Codes</h4>
                <p class="sec-step-desc">Each code can be used once if you lose your device</p>
                <div class="sec-backup-codes">
                  <code v-for="(code, i) in setupData.backup_codes" :key="i" class="sec-backup-code">{{ code }}</code>
                </div>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="downloadBackupCodes">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                    <polyline points="7 10 12 15 17 10"/>
                    <line x1="12" y1="15" x2="12" y2="3"/>
                  </svg>
                  Download Codes
                </button>
              </div>
            </div>
          </div>

          <div class="sec-modal-footer">
            <button class="base-btn base-btn--ghost" @click="showSetup = false">Cancel</button>
            <button class="base-btn base-btn--primary" @click="enableTwoFA" :disabled="setupLoading || verifyCode.length !== 6">
              {{ setupLoading ? 'Enabling...' : 'Enable 2FA' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Disable Modal -->
    <Teleport to="body">
      <div v-if="showDisable" class="sec-modal-overlay" @click.self="showDisable = false">
        <div class="page-modal sec-modal" style="max-width:440px">
          <div class="sec-modal-header">
            <h2 class="sec-modal-title">Disable Two-Factor Authentication</h2>
            <button class="sec-modal-close" @click="showDisable = false">×</button>
          </div>

          <div class="sec-modal-body">
            <div class="notice notice--warning" style="margin-bottom:16px">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                <line x1="12" y1="9" x2="12" y2="13"/>
                <line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
              Your account will be less secure without 2FA
            </div>

            <div class="form-group">
              <label class="form-label">Confirm with Password or Backup Code</label>
              <input
                v-model="disablePassword"
                class="base-input"
                type="password"
                placeholder="Enter your password"
                @input="disableBackupCode = ''"
              />
            </div>
            <div style="text-align:center;color:var(--text-muted);font-size:12px;margin:8px 0">or</div>
            <div class="form-group">
              <input
                v-model="disableBackupCode"
                class="base-input"
                type="text"
                placeholder="Enter a backup code"
                @input="disablePassword = ''"
              />
            </div>
          </div>

          <div class="sec-modal-footer">
            <button class="base-btn base-btn--ghost" @click="showDisable = false">Cancel</button>
            <button class="base-btn base-btn--primary" style="background:#f87171;border-color:#f87171" @click="disableTwoFA" :disabled="disableLoading || (!disablePassword && !disableBackupCode)">
              {{ disableLoading ? 'Disabling...' : 'Disable 2FA' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sec-view {
  background: var(--bg-body);
}

.sec-scroll {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.sec-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 40px;
  color: var(--text-muted);
  font-size: 13px;
}

/* Card */
.sec-card {
  overflow: hidden;
}

.sec-card-header {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--border);
}

.sec-card-icon {
  width: 44px;
  height: 44px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.sec-card-icon--enabled {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.sec-card-icon--disabled {
  background: var(--bg-surface);
  color: var(--text-muted);
}

.sec-card-meta {
  flex: 1;
}

.sec-card-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 2px;
}

.sec-card-desc {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
}

.sec-card-status {
  flex-shrink: 0;
}

.sec-badge {
  padding: 3px 10px;
  border-radius: 12px;
  font-size: 11px;
  font-weight: 600;
}

.sec-badge--enabled {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.sec-badge--disabled {
  background: var(--bg-surface);
  color: var(--text-muted);
}

.sec-card-body {
  padding: 18px 20px;
}

.sec-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sec-info-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
}

.sec-info-label {
  color: var(--text-muted);
}

.sec-info-value {
  color: var(--text-primary);
  font-weight: 600;
}

.sec-help {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.5;
}

.sec-card-footer {
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
}

/* Modal */
.sec-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  backdrop-filter: blur(2px);
}

.sec-modal {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 12px;
  width: min(620px, 94vw);
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.5);
}

.sec-modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 24px;
  border-bottom: 1px solid var(--border);
}

.sec-modal-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.sec-modal-close {
  background: transparent;
  border: none;
  font-size: 24px;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
  line-height: 1;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.12s;
}

.sec-modal-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.sec-modal-body {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}

.sec-modal-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 24px;
  border-top: 1px solid var(--border);
}

/* Setup Steps */
.sec-step {
  display: flex;
  gap: 14px;
  margin-bottom: 24px;
}

.sec-step:last-child {
  margin-bottom: 0;
}

.sec-step-num {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--brand);
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  flex-shrink: 0;
}

.sec-step-content {
  flex: 1;
}

.sec-step-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 4px;
}

.sec-step-desc {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0 0 12px;
}

.sec-qr-wrap {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: white;
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 12px;
}

.sec-secret {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
}

.sec-secret-label {
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
}

.sec-secret-code {
  font-family: var(--mono, monospace);
  font-size: 13px;
  color: var(--text-primary);
  word-break: break-all;
}

.sec-code-input {
  text-align: center;
  font-size: 24px;
  font-weight: 600;
  letter-spacing: 10px;
  font-family: var(--mono, monospace);
  max-width: 200px;
}

.sec-backup-codes {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
  margin-bottom: 12px;
}

.sec-backup-code {
  padding: 8px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: var(--mono, monospace);
  font-size: 12px;
  color: var(--text-primary);
  text-align: center;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
