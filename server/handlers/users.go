package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/bcrypt"
)

type UserInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func ListUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(`SELECT id, username, role, created_at FROM users ORDER BY id`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var users []UserInfo
		for rows.Next() {
			var u UserInfo
			rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
			users = append(users, u)
		}
		if users == nil {
			users = []UserInfo{}
		}
		json.NewEncoder(w).Encode(users)
	}
}

func UpdateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var body struct {
			Role     string `json:"role"`
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		if body.Role != "" {
			appdb.DB.Exec(`UPDATE users SET role=? WHERE id=?`, body.Role, id)
		}
		if body.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
			if err == nil {
				appdb.DB.Exec(`UPDATE users SET password=? WHERE id=?`, string(hash), id)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func DeleteUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		appdb.DB.Exec(`DELETE FROM users WHERE id=?`, id)
		w.WriteHeader(http.StatusNoContent)
	}
}
