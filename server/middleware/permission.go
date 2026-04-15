package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/anveesa/nias/db"
	"github.com/anveesa/nias/handlers"
)

// Context keys
type contextKey string

const (
	userIDKey   contextKey = "user_id"
	userRoleKey contextKey = "user_role"
	connIDKey   contextKey = "conn_id"
)

// RequireAppPermission returns middleware that checks if the current user has the given application permission.
func RequireAppPermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			if db.HasUserAppPermission(userID, perm) {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "insufficient permissions: "+perm, http.StatusForbidden)
		})
	}
}

// RequireAnyAppPermission returns middleware that checks if the user has any of the given permissions.
func RequireAnyAppPermission(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			userPerms, err := db.GetUserAppPermissions(userID)
			if err != nil {
				http.Error(w, "failed to check permissions", http.StatusInternalServerError)
				return
			}

			for _, required := range perms {
				for _, has := range userPerms {
					if has == required {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			http.Error(w, "insufficient permissions", http.StatusForbidden)
		})
	}
}

// RequireConnectionAccess checks that the current user has access to the
// connection specified in the URL path.
func RequireConnectionAccess() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			// Extract connection ID from URL path (/api/connections/{id}/...)
			path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
			parts := strings.Split(path, "/")
			if len(parts) == 0 {
				http.Error(w, "connection id required", http.StatusBadRequest)
				return
			}

			connID, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				http.Error(w, "invalid connection id", http.StatusBadRequest)
				return
			}

			role, err := db.GetUserRole(userID)
			if err != nil {
				http.Error(w, "failed to get user role", http.StatusInternalServerError)
				return
			}

			// Admin bypass
			if role == "admin" {
				ctx := context.WithValue(r.Context(), userRoleKey, role)
				ctx = context.WithValue(ctx, connIDKey, connID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			ids, err := db.GetAccessibleConnectionIDs(userID, role)
			if err != nil {
				http.Error(w, "failed to check access", http.StatusInternalServerError)
				return
			}

			// nil means unrestricted
			if ids == nil {
				ctx := context.WithValue(r.Context(), userRoleKey, role)
				ctx = context.WithValue(ctx, connIDKey, connID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			for _, id := range ids {
				if id == connID {
					ctx := context.WithValue(r.Context(), userRoleKey, role)
					ctx = context.WithValue(ctx, connIDKey, connID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			http.Error(w, "you do not have access to this connection", http.StatusForbidden)
		})
	}
}

// RequireDbPermission checks that the current user has the required database operation permission
// for the specified connection. Should be used AFTER RequireConnectionAccess.
func RequireDbPermission(requiredPerm db.DbPerm) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			connID, ok := r.Context().Value(connIDKey).(int64)
			if !ok {
				http.Error(w, "connection id required", http.StatusBadRequest)
				return
			}

			role, ok := r.Context().Value(userRoleKey).(string)
			if !ok {
				var err error
				role, err = db.GetUserRole(userID)
				if err != nil {
					http.Error(w, "failed to get user role", http.StatusInternalServerError)
					return
				}
			}

			perms, err := db.GetUserConnectionPermissions(userID, role, connID)
			if err != nil {
				http.Error(w, "failed to check permissions", http.StatusInternalServerError)
				return
			}

			for _, p := range perms {
				if p == requiredPerm {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "insufficient database permissions: "+string(requiredPerm), http.StatusForbidden)
		})
	}
}

// RequireDbPermissionForSQL checks SQL statement and validates the required permission.
// Use this for query execution endpoints.
func RequireDbPermissionForSQL() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			connID, ok := r.Context().Value(connIDKey).(int64)
			if !ok {
				http.Error(w, "connection id required", http.StatusBadRequest)
				return
			}

			// Extract SQL from request body
			var body struct {
				SQL string `json:"sql"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			if body.SQL == "" {
				http.Error(w, "sql required", http.StatusBadRequest)
				return
			}

			// Detect required permission
			requiredPerm := handlers.DetectRequiredPerm(body.SQL)
			if requiredPerm == "" {
				// Unknown/unclassified statement - allow (or could be more strict)
				next.ServeHTTP(w, r)
				return
			}

			role, ok := r.Context().Value(userRoleKey).(string)
			if !ok {
				var err error
				role, err = db.GetUserRole(userID)
				if err != nil {
					http.Error(w, "failed to get user role", http.StatusInternalServerError)
					return
				}
			}

			perms, err := db.GetUserConnectionPermissions(userID, role, connID)
			if err != nil {
				http.Error(w, "failed to check permissions", http.StatusInternalServerError)
				return
			}

			// Convert string to db.DbPerm for comparison
			for _, p := range perms {
				if string(p) == string(requiredPerm) {
					next.ServeHTTP(w, r)
					return
				}
			}

			http.Error(w, "insufficient permissions for this SQL operation: "+string(requiredPerm), http.StatusForbidden)
		})
	}
}

// CheckConnectionListAccess filters connections based on user access.
// This is for list endpoints - adds accessible connection IDs to context.
func CheckConnectionListAccess() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(userIDKey).(int64)
			if !ok || userID == 0 {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}

			role, err := db.GetUserRole(userID)
			if err != nil {
				http.Error(w, "failed to get user role", http.StatusInternalServerError)
				return
			}

			ids, err := db.GetAccessibleConnectionIDs(userID, role)
			if err != nil {
				http.Error(w, "failed to check access", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), userRoleKey, role)
			ctx = context.WithValue(ctx, "accessible_connection_ids", ids)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helper: check if SQL is read-only (SELECT, SHOW, DESCRIBE, EXPLAIN)
func IsReadOnlySQL(sql string) bool {
	s := strings.TrimSpace(strings.ToUpper(sql))
	return strings.HasPrefix(s, "SELECT") ||
		strings.HasPrefix(s, "SHOW") ||
		strings.HasPrefix(s, "DESCRIBE") ||
		strings.HasPrefix(s, "DESC ") ||
		strings.HasPrefix(s, "EXPLAIN")
}
