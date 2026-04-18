# Two-Factor Authentication (2FA) Implementation

## Overview
Comprehensive 2FA (Two-Factor Authentication) using TOTP (Time-based One-Time Password) for all users.

## Features Implemented

### 1. Home Menu Removal
- ✅ Removed "Home" from top navigation
- ✅ Root path (`/`) now redirects directly to Data Browser
- ✅ Applied to all users (admin and regular users)

### 2. Backend 2FA Implementation

#### Database Schema
New columns added to `users` table:
- `totp_secret` (TEXT) - TOTP secret key
- `totp_enabled` (INTEGER) - 0 or 1, indicates if 2FA is enabled
- `backup_codes` (TEXT) - JSON array of backup codes

#### API Endpoints

**Setup & Management:**
- `GET /api/auth/2fa/status` - Get 2FA status for current user
- `POST /api/auth/2fa/setup` - Generate new TOTP secret and QR code
- `POST /api/auth/2fa/enable` - Verify code and enable 2FA
- `POST /api/auth/2fa/disable` - Disable 2FA (requires password or backup code)

**Login Flow:**
- `POST /api/auth/login` - Updated to support 2FA
  - If 2FA enabled and no code: returns `{"requires_2fa": true}`
  - If 2FA enabled with code: verifies TOTP or backup code
  - If 2FA not enabled: normal login

**Verification:**
- `POST /api/auth/2fa/verify` - Verify TOTP code (for login)

#### Backup Codes
- 10 backup codes generated during setup
- Format: `ABC123-DEF456`
- Single-use (removed after use)
- Can be used instead of TOTP code

### 3. Frontend UI (To Be Completed)

#### Login Page Updates
- Add TOTP code input field
- Show when `requires_2fa: true` is returned
- Support for backup code entry

#### User Settings - 2FA Management
- Enable/Disable 2FA
- QR code display for setup
- Backup codes display and download
- Re-generate backup codes option

#### Components Needed
1. **TwoFactorSetup.vue** - Setup wizard with QR code
2. **TwoFactorVerify.vue** - Code verification during login
3. **TwoFactorSettings.vue** - Manage 2FA in user profile

## Security Features

### TOTP Configuration
- **Algorithm**: SHA1 (standard)
- **Digits**: 6
- **Period**: 30 seconds
- **Issuer**: "Singapay SQL"

### Backup Codes
- 10 codes generated using cryptographically secure random
- Stored as JSON array in database
- Single-use only (removed after verification)

### Login Security
- Password verified first
- TOTP code verified second
- Backup code as fallback
- Constant-time comparison for timing attack prevention

## Usage Flow

### For Users

**Enabling 2FA:**
1. Go to User Settings → Security → 2FA
2. Click "Enable 2FA"
3. Scan QR code with authenticator app (Google Authenticator, Authy, etc.)
4. Enter 6-digit code to verify
5. Download and save backup codes securely
6. 2FA is now active

**Logging In with 2FA:**
1. Enter username and password
2. System prompts for 2FA code
3. Enter 6-digit code from authenticator app
4. Or use a backup code if needed
5. Successfully logged in

**Disabling 2FA:**
1. Go to User Settings → Security → 2FA
2. Click "Disable 2FA"
3. Enter password or backup code
4. 2FA is disabled

### For Administrators

**No Special Admin Requirements:**
- All users (admin and regular) follow the same 2FA process
- Admins cannot disable 2FA for other users (users must disable themselves)
- Backup codes are the recovery method if authenticator is lost

## Testing

### Backend Testing
```bash
# Get 2FA status
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/api/auth/2fa/status

# Setup 2FA
curl -H "Authorization: Bearer YOUR_TOKEN" -X POST http://localhost:8080/api/auth/2fa/setup

# Enable 2FA (with code)
curl -H "Authorization: Bearer YOUR_TOKEN" -X POST http://localhost:8080/api/auth/2fa/enable \
  -H "Content-Type: application/json" \
  -d '{"code":"123456"}'

# Login with 2FA
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"andi","password":"andi","totp_code":"123456"}'

# Disable 2FA
curl -H "Authorization: Bearer YOUR_TOKEN" -X POST http://localhost:8080/api/auth/2fa/disable \
  -H "Content-Type: application/json" \
  -d '{"password":"andi"}'
```

## Dependencies

### Backend (Go)
- `github.com/pquerna/otp` - TOTP generation and validation
- `github.com/boombuler/barcode` - QR code generation (dependency of otp)

### Frontend (Vue)
- **Authenticator Apps:** Google Authenticator, Authy, Microsoft Authenticator
- **QR Code Library:** (To be added, e.g., `qrcode.vue`)

## Next Steps

### Frontend Implementation Remaining:
1. Create `TwoFactorSetup.vue` component
2. Create `TwoFactorVerify.vue` component  
3. Update `LoginView.vue` to handle 2FA flow
4. Add 2FA settings to user profile/settings page
5. Install QR code library for frontend
6. Add backup codes download functionality

### Optional Enhancements:
- Email notification when 2FA is enabled/disabled
- SMS-based 2FA as alternative
- Recovery email option
- Admin dashboard to see 2FA adoption rate
- Force 2FA for admin users
- WebAuthn/FIDO2 support (hardware keys)

## Database Migration

The schema changes are applied automatically via the migration system in `server/db/db.go`.

If manual migration is needed:
```sql
ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT NULL;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER DEFAULT 0;
ALTER TABLE users ADD COLUMN backup_codes TEXT DEFAULT NULL;
```

## Rollback

To disable 2FA for a specific user (emergency):
```sql
UPDATE users SET totp_enabled = 0, totp_secret = NULL, backup_codes = NULL WHERE username = 'username';
```

## Support

**Lost Authenticator Device:**
- Use backup codes to log in
- Disable 2FA in settings
- Re-enable with new device

**Lost Backup Codes:**
- Contact administrator to reset 2FA (requires direct database access)
- Or use still-working authenticator app to log in and get new codes
