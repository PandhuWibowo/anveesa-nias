# User Management & Keamanan — Class Tree

## Backend

### POST /api/auth/setup
```
└── (No middleware — public)
    └── handlers.SetupHandler(cfg)                                    [handlers/auth.go]
        └── json.NewEncoder(w).Encode({auth_enabled: cfg.AuthEnabled})
```

### POST /api/auth/login
```
└── (No middleware — public)
    └── handlers.LoginHandler(cfg)                                    [handlers/auth.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── appdb.DB.QueryRow(SELECT id,username,password,role,is_active,totp_enabled,totp_secret FROM users WHERE username=?) [db/db.go]
        ├── bcrypt.CompareHashAndPassword(hash, password)
        ├── isActive check
        ├── [2FA enabled] totp.Validate(code, totpSecret)             [github.com/pquerna/otp/totp]
        │   └── backup code check → appdb.DB.Exec(UPDATE users SET backup_codes=?) [db/db.go]
        ├── newSessionID()                                            [handlers/auth.go — crypto/rand hex]
        ├── appdb.CreateAuthSession(userID, sessionID, clientIP, userAgent, expiresAt) [db/db.go]
        ├── jwt.NewWithClaims(HS256, &Claims{UserID, Username, Role, SessionID}) [github.com/golang-jwt/jwt]
        ├── token.SignedString(cfg.JWTSecret)
        ├── appdb.GetUserAppPermissions(id)                           [db/db.go]
        ├── appdb.RecordLoginEvent(&id, username, ip, ua, success, reason) [db/db.go]
        └── json.NewEncoder(w).Encode({token, user: {id, username, role, permissions}})
```

### POST /api/auth/register
```
└── (Public for first user; admin-only thereafter via admin UI)
    └── handlers.RegisterHandler(cfg)                                 [handlers/auth.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── validatePassword(password)                                [handlers/auth.go — strength check]
        ├── bcrypt.GenerateFromPassword(password, bcryptCost=12)
        ├── registerMu.Lock()                                         [sync.Mutex — first-user race guard]
        ├── appdb.DB.QueryRow(SELECT COUNT(*) FROM users)             [db/db.go]
        ├── appdb.DB.QueryRow(SELECT name FROM roles WHERE id=?)      [db/db.go]
        └── appdb.DB.QueryRow/Exec(INSERT INTO users ...)             [db/db.go]
```

### GET /api/auth/me
```
└── Middleware: mw.InjectUserContext
    └── handlers.MeHandler()                                          [handlers/auth.go]
        ├── r.Header.Get("Authorization") → jwt.ParseWithClaims()
        ├── appdb.DB.QueryRow(SELECT id,username,role,is_active FROM users WHERE id=?) [db/db.go]
        ├── appdb.IsSessionValid(claims.UserID, claims.SessionID)     [db/db.go]
        ├── appdb.GetUserAppPermissions(claims.UserID)                [db/db.go]
        └── json.NewEncoder(w).Encode({id, username, role, permissions})
```

### POST /api/auth/logout
```
└── Middleware: mw.InjectUserContext
    └── handlers.LogoutHandler()                                      [handlers/auth.go]
        ├── jwt.ParseWithClaims() → extract SessionID
        └── appdb.RevokeAuthSession(userID, sessionID)                [db/db.go]
```

### POST /api/auth/password/change
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSecuritySelf)
    └── handlers.ChangePasswordHandler()                              [handlers/auth.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── appdb.DB.QueryRow(SELECT password FROM users WHERE id=?)  [db/db.go]
        ├── bcrypt.CompareHashAndPassword(current hash, old password)
        ├── validatePassword(newPassword)
        ├── bcrypt.GenerateFromPassword(newPassword, bcryptCost)
        └── appdb.DB.Exec(UPDATE users SET password=? WHERE id=?)     [db/db.go]
```

### GET /api/auth/sessions
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSecuritySelf)
    └── handlers.ListSessionsHandler()                                [handlers/auth.go]
        └── appdb.DB.Query(SELECT FROM auth_sessions WHERE user_id=? ORDER BY created_at DESC) [db/db.go]
```

### POST /api/auth/sessions/revoke-all
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSecuritySelf)
    └── handlers.RevokeAllSessionsHandler()                           [handlers/auth.go]
        └── appdb.DB.Exec(DELETE FROM auth_sessions WHERE user_id=?) [db/db.go]
```

### POST /api/auth/sessions/{id}/revoke
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSecuritySelf)
    └── handlers.RevokeSessionHandler()                               [handlers/auth.go]
        └── appdb.DB.Exec(DELETE FROM auth_sessions WHERE id=? AND user_id=?) [db/db.go]
```

### GET /api/auth/activity
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSecuritySelf)
    └── handlers.LoginActivityHandler()                               [handlers/auth.go]
        └── appdb.DB.Query(SELECT FROM login_events WHERE user_id=? ORDER BY created_at DESC LIMIT 50) [db/db.go]
```

### GET /api/auth/2fa/status
```
└── Middleware: mw.InjectUserContext
    └── handlers.Get2FAStatus()                                       [handlers/2fa.go]
        └── appdb.DB.QueryRow(SELECT totp_enabled, backup_codes FROM users WHERE id=?) [db/db.go]
```

### POST /api/auth/2fa/setup
```
└── Middleware: mw.InjectUserContext
    └── handlers.Setup2FA()                                           [handlers/2fa.go]
        ├── appdb.DB.QueryRow(SELECT username FROM users WHERE id=?)  [db/db.go]
        ├── totp.Generate(GenerateOpts{Issuer, AccountName, Period, Digits, Algorithm}) [pquerna/otp/totp]
        ├── generate 10 backup codes via crypto/rand
        └── appdb.DB.Exec(UPDATE users SET totp_secret=?, backup_codes=?) [db/db.go]
```

### POST /api/auth/2fa/enable
```
└── Middleware: mw.InjectUserContext
    └── handlers.Enable2FA()                                          [handlers/2fa.go]
        ├── appdb.DB.QueryRow(SELECT totp_secret FROM users WHERE id=?) [db/db.go]
        ├── totp.Validate(code, secret)                               [pquerna/otp/totp]
        └── appdb.DB.Exec(UPDATE users SET totp_enabled=1 WHERE id=?) [db/db.go]
```

### POST /api/auth/2fa/disable
```
└── Middleware: mw.InjectUserContext
    └── handlers.Disable2FA()                                         [handlers/2fa.go]
        ├── appdb.DB.QueryRow(SELECT password, backup_codes FROM users WHERE id=?) [db/db.go]
        ├── bcrypt.CompareHashAndPassword() — password verify
        └── appdb.DB.Exec(UPDATE users SET totp_enabled=0, totp_secret=NULL, backup_codes=NULL) [db/db.go]
```

### POST /api/auth/2fa/verify
```
└── (Public — called during login flow)
    └── handlers.Verify2FA()                                          [handlers/2fa.go]
        ├── appdb.DB.QueryRow(SELECT id,totp_secret,totp_enabled,backup_codes FROM users WHERE username=?) [db/db.go]
        └── totp.Validate(code, secret) / backup code match
```

### GET/POST /api/admin/users
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    ├── handlers.ListUsers()                                          [handlers/users.go]
    │   └── appdb.DB.Query(SELECT u.id,u.username,r.name,u.role_id,u.is_active FROM users LEFT JOIN roles) [db/db.go]
    └── handlers.RegisterHandler(cfg)                                 [handlers/auth.go — reused]
```

### PUT /api/admin/users/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.UpdateUser()                                         [handlers/users.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── [role_id provided] appdb.DB.QueryRow(SELECT name FROM roles WHERE id=?) [db/db.go]
        ├── appdb.DB.Exec(UPDATE users SET role=?, role_id=?)         [db/db.go]
        ├── [password provided] bcrypt.GenerateFromPassword(password, bcryptCost)
        ├── appdb.DB.Exec(UPDATE users SET password=?)                [db/db.go]
        ├── appdb.DB.Exec(UPDATE users SET is_active=?)               [db/db.go]
        ├── appdb.DB.Exec(DELETE FROM user_connections WHERE user_id=?) [db/db.go — reset assignments]
        └── appdb.DB.Exec(INSERT INTO user_connections (user_id, conn_id)) [db/db.go — per conn]
```

### DELETE /api/admin/users/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.DeleteUser()                                         [handlers/users.go]
        └── appdb.DB.Exec(DELETE FROM users WHERE id=?)               [db/db.go]
```

### POST /api/admin/users/{id}/reset-password
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.ResetPasswordHandler()                               [handlers/auth.go]
        ├── validatePassword(newPassword)
        ├── bcrypt.GenerateFromPassword(newPassword, bcryptCost)
        └── appdb.DB.Exec(UPDATE users SET password=? WHERE id=?)     [db/db.go]
```

### GET/POST /api/roles
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRolesManage)
    ├── handlers.ListRoles()                                          [handlers/rbac.go]
    │   └── db.DB.Query(SELECT r.id,r.name,r.description,r.permissions,r.is_system,r.is_active,(SELECT COUNT user_count) FROM roles) [db/db.go]
    └── handlers.CreateRole()                                         [handlers/rbac.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── AppPermsToJSON(req.Permissions)                           [handlers/models.go]
        └── db.DB.QueryRow/Exec(INSERT INTO roles ...)                [db/db.go]
```

### GET/PUT/DELETE /api/roles/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRolesManage)
    ├── handlers.GetRole()                                            [handlers/rbac.go]
    │   └── db.DB.QueryRow(SELECT FROM roles WHERE id=?)              [db/db.go]
    ├── handlers.UpdateRole()                                         [handlers/rbac.go]
    │   ├── db.DB.QueryRow(SELECT is_system, name FROM roles WHERE id=?) [db/db.go]
    │   ├── AppPermsToJSON(req.Permissions)
    │   └── db.DB.Exec(UPDATE roles SET name=?,description=?,permissions=?)  [db/db.go]
    └── handlers.DeleteRole()                                         [handlers/rbac.go]
        ├── db.DB.QueryRow(SELECT is_system FROM roles WHERE id=?)    [db/db.go]
        ├── db.DB.QueryRow(SELECT COUNT(*) FROM users WHERE role_id=?) [db/db.go — guard]
        └── db.DB.Exec(DELETE FROM roles WHERE id=?)                  [db/db.go]
```

### GET /api/app-permissions
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRolesManage)
    └── handlers.ListAppPermissions()                                 [handlers/rbac.go]
        └── json.NewEncoder(w).Encode(AllAppPermissions)              [handlers/models.go — static list]
```

### GET /api/my-permissions
```
└── Middleware: mw.InjectUserContext
    └── handlers.GetMyPermissions()                                   [handlers/rbac.go]
        ├── strconv.ParseInt(r.Header.Get("X-User-ID"))
        └── db.GetUserAppPermissions(userID)                          [db/db.go]
```

### GET/POST /api/permissions
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    ├── handlers.ListPermissions()                                    [handlers/permissions_legacy.go]
    │   └── appdb.DB.Query(SELECT FROM user_permissions)              [db/db.go]
    └── handlers.UpsertPermission()                                   [handlers/permissions_legacy.go]
        └── appdb.DB.Exec(INSERT OR REPLACE INTO user_permissions)    [db/db.go]
```

### DELETE /api/permissions/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.DeletePermission()                                   [handlers/permissions_legacy.go]
        └── appdb.DB.Exec(DELETE FROM user_permissions WHERE id=?)    [db/db.go]
```

---

## Frontend

### Route: /login
```
router/index.ts
└── { path: '/login', meta: { guest: true } }
    └── views/LoginView.vue                                           (4.1 KB)
        └── useAuth()                                                 [composables/useAuth.ts]
            ├── axios.get('/api/auth/setup')
            └── axios.post('/api/auth/login', {username, password, totp_code})
```

### Route: /users
```
router/index.ts
└── { path: 'users', meta: { requiredPermissionsAny: ['users.manage'] } }
    └── views/UsersView.vue                                           (9.1 KB)
        ├── axios.get('/api/admin/users')
        ├── axios.post('/api/admin/users', body)
        ├── axios.put('/api/admin/users/{id}', body)
        ├── axios.delete('/api/admin/users/{id}')
        └── axios.post('/api/admin/users/{id}/reset-password')
```

### Route: /permissions
```
router/index.ts
└── { path: 'permissions', meta: { requiredPermissionsAny: ['roles.manage', 'folders.manage', 'users.manage'] } }
    └── views/PermissionsView.vue                                     (50 KB)
        ├── axios.get('/api/roles')
        ├── axios.post('/api/roles', body)
        ├── axios.put('/api/roles/{id}', body)
        ├── axios.delete('/api/roles/{id}')
        ├── axios.get('/api/app-permissions')
        ├── axios.get('/api/admin/users')
        ├── axios.get('/api/users/{id}/connections')
        └── axios.post('/api/users/{id}/connections', body)
```

### Route: /security (own account security)
```
router/index.ts
└── { path: 'security', meta: { requiredPermissionsAny: ['security.self'] } }
    └── views/SecurityView.vue                                        (23 KB)
        ├── axios.post('/api/auth/password/change')
        ├── axios.get('/api/auth/sessions')
        ├── axios.post('/api/auth/sessions/{id}/revoke')
        ├── axios.post('/api/auth/sessions/revoke-all')
        ├── axios.get('/api/auth/activity')
        ├── axios.get('/api/auth/2fa/status')
        ├── axios.post('/api/auth/2fa/setup')
        ├── axios.post('/api/auth/2fa/enable', {code})
        └── axios.post('/api/auth/2fa/disable', {password})
```

### Middleware: useAuth interceptors (global)
```
composables/useAuth.ts
├── axios.interceptors.request — adds Authorization: Bearer {token}, X-User-ID, X-User-Role, X-Username headers
└── axios.interceptors.response — on 401/423: clears token, resets user state
```
