package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// loadUserConnCredential returns the stored per-user DB login for a connection.
// encPass is the AES-encrypted password (decrypt only at the point of use).
func loadUserConnCredential(userID, connID int64) (dbUser, encPass string, ok bool) {
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT db_username, COALESCE(db_password,'') FROM user_connection_credentials WHERE user_id=? AND conn_id=?`),
		userID, connID,
	).Scan(&dbUser, &encPass)
	if err != nil || strings.TrimSpace(dbUser) == "" {
		return "", "", false
	}
	return dbUser, encPass, true
}

// loadConnAuthMode returns 'shared' (default) or 'per_user'.
func loadConnAuthMode(connID int64) string {
	var mode string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT COALESCE(auth_mode,'shared') FROM connections WHERE id=?`), connID,
	).Scan(&mode)
	if err != nil || mode == "" {
		return "shared"
	}
	return mode
}

// parseUserConnPath extracts {userID, connID} from
// /api/users/{userID}/connections/{connID}/credential.
func parseUserConnPath(path string) (userID, connID int64, ok bool) {
	rest := strings.TrimPrefix(path, "/api/users/")
	parts := strings.Split(rest, "/")
	// {userID} connections {connID} credential
	if len(parts) < 4 || parts[1] != "connections" {
		return 0, 0, false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	cid, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return uid, cid, true
}

// GetUserConnCredential reports whether a per-user login is configured and, if
// so, the username. It NEVER returns the password.
func GetUserConnCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, connID, ok := parseUserConnPath(r.URL.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		dbUser, _, has := loadUserConnCredential(userID, connID)
		json.NewEncoder(w).Encode(map[string]any{
			"configured":  has,
			"db_username": dbUser,
		})
	}
}

// SetUserConnCredential stores (or replaces) a user's native DB login for a
// connection. The password is verified against the target DB before it is saved
// (so we never persist a login that can't connect), encrypted at rest, and any
// pooled session for that user is evicted.
func SetUserConnCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, connID, ok := parseUserConnPath(r.URL.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			DBUsername string `json:"db_username"`
			DBPassword string `json:"db_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		req.DBUsername = strings.TrimSpace(req.DBUsername)
		if req.DBUsername == "" {
			http.Error(w, `{"error":"db_username is required"}`, http.StatusBadRequest)
			return
		}

		// If the password is omitted on update, keep the existing one.
		encPass := ""
		if req.DBPassword == "" {
			if _, existing, has := loadUserConnCredential(userID, connID); has {
				encPass = existing
			}
		}
		plainPass := req.DBPassword
		if plainPass == "" && encPass != "" {
			if dec, err := decryptCredential(encPass); err == nil {
				plainPass = dec
			}
		}

		// Verify the credentials actually connect before persisting them.
		testDB, _, err := openRemoteDBWithCreds(connID, req.DBUsername, plainPass, true)
		if err != nil {
			http.Error(w, jsonError("could not open connection: "+err.Error()), http.StatusBadGateway)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if pingErr := testDB.PingContext(ctx); pingErr != nil {
			testDB.Close()
			http.Error(w, jsonError("credentials rejected by database: "+pingErr.Error()), http.StatusBadGateway)
			return
		}
		testDB.Close()

		if req.DBPassword != "" {
			enc, err := encryptCredential(req.DBPassword)
			if err != nil {
				http.Error(w, `{"error":"failed to secure password"}`, http.StatusInternalServerError)
				return
			}
			encPass = enc
		}

		_, err = appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM user_connection_credentials WHERE user_id=? AND conn_id=?`), userID, connID)
		if err == nil {
			_, err = appdb.DB.Exec(
				appdb.ConvertQuery(`INSERT INTO user_connection_credentials (user_id, conn_id, db_username, db_password, updated_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`),
				userID, connID, req.DBUsername, encPass,
			)
		}
		if err != nil {
			http.Error(w, jsonError("failed to save credential: "+err.Error()), http.StatusInternalServerError)
			return
		}

		EvictUserFromPool(userID, connID)

		cid := connID
		_, actor, _ := currentUserFromHeaders(r)
		writeAuditEvent("connection", "set_user_credential",
			"user:"+strconv.FormatInt(userID, 10), "db_username="+req.DBUsername, actor, &cid, "", "", 0, 0, "")

		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// DeleteUserConnCredential removes a user's native DB login for a connection.
func DeleteUserConnCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID, connID, ok := parseUserConnPath(r.URL.Path)
		if !ok {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM user_connection_credentials WHERE user_id=? AND conn_id=?`), userID, connID); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		EvictUserFromPool(userID, connID)
		cid := connID
		_, actor, _ := currentUserFromHeaders(r)
		writeAuditEvent("connection", "delete_user_credential",
			"user:"+strconv.FormatInt(userID, 10), "", actor, &cid, "", "", 0, 0, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// SetConnectionAuthMode toggles a connection between 'shared' and 'per_user'.
func SetConnectionAuthMode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// /api/connections/{id}/auth-mode
		rest := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.Split(rest, "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var req struct {
			AuthMode string `json:"auth_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if req.AuthMode != "shared" && req.AuthMode != "per_user" {
			http.Error(w, `{"error":"auth_mode must be 'shared' or 'per_user'"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET auth_mode=? WHERE id=?`), req.AuthMode, connID); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		// Shared login may have changed relevance — drop cached sessions.
		EvictFromPool(connID)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}
