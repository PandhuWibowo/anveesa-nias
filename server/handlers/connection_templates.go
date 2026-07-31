package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// ConnectionTemplate stores reusable host/port/database presets without credentials.
type ConnectionTemplate struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	SSL         bool   `json:"ssl"`
	Tags        string `json:"tags"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	OwnerID     int64  `json:"owner_id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ConnectionTemplateInput struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	SSL         bool   `json:"ssl"`
	Tags        string `json:"tags"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
}

func scanTemplate(row interface {
	Scan(...any) error
}) (ConnectionTemplate, error) {
	var t ConnectionTemplate
	var sslV int
	err := row.Scan(
		&t.ID, &t.Name, &t.Driver, &t.Host, &t.Port, &t.Database,
		&sslV, &t.Tags, &t.SSHHost, &t.SSHPort, &t.SSHUser,
		&t.Description, &t.Visibility, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
	)
	t.SSL = sslV == 1
	return t, err
}

func ListConnectionTemplates() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")
		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		var rows interface {
			Scan(...any) error
			Next() bool
			Close() error
			Err() error
		}
		var err error

		if userRole == "admin" {
			r2, e := appdb.DB.Query(appdb.ConvertQuery(
				`SELECT id, name, driver, host, port, database, ssl, tags,
				        ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at
				 FROM connection_templates ORDER BY name ASC`))
			rows, err = r2, e
		} else {
			r2, e := appdb.DB.Query(appdb.ConvertQuery(
				`SELECT id, name, driver, host, port, database, ssl, tags,
				        ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at
				 FROM connection_templates
				 WHERE visibility = 'shared' OR owner_id = ?
				 ORDER BY name ASC`), userID)
			rows, err = r2, e
		}
		if err != nil {
			http.Error(w, `{"error":"failed to query templates"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var result []ConnectionTemplate
		for rows.Next() {
			t, err := scanTemplate(rows)
			if err != nil {
				continue
			}
			result = append(result, t)
		}
		if result == nil {
			result = []ConnectionTemplate{}
		}
		json.NewEncoder(w).Encode(result)
	}
}

func CreateConnectionTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var in ConnectionTemplateInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if !allowedDrivers[in.Driver] {
			http.Error(w, `{"error":"invalid driver"}`, http.StatusBadRequest)
			return
		}
		if in.Visibility == "" {
			in.Visibility = "shared"
		}
		if in.SSHPort == 0 {
			in.SSHPort = 22
		}

		var ownerID int64
		if s := r.Header.Get("X-User-ID"); s != "" {
			ownerID, _ = strconv.ParseInt(s, 10, 64)
		}

		ssl := 0
		if in.SSL {
			ssl = 1
		}

		now := time.Now().UTC().Format("2006-01-02 15:04:05")

		var id int64
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err := appdb.DB.QueryRow(appdb.ConvertQuery(
				`INSERT INTO connection_templates
				 (name, driver, host, port, database, ssl, tags, ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at)
				 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`),
				in.Name, in.Driver, in.Host, in.Port, in.Database, ssl, in.Tags,
				in.SSHHost, in.SSHPort, in.SSHUser, in.Description, in.Visibility, ownerID, now, now,
			).Scan(&id)
			if err != nil {
				http.Error(w, `{"error":"failed to create template"}`, http.StatusInternalServerError)
				return
			}
		} else {
			res, err := appdb.DB.Exec(appdb.ConvertQuery(
				`INSERT INTO connection_templates
				 (name, driver, host, port, database, ssl, tags, ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at)
				 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
				in.Name, in.Driver, in.Host, in.Port, in.Database, ssl, in.Tags,
				in.SSHHost, in.SSHPort, in.SSHUser, in.Description, in.Visibility, ownerID, now, now,
			)
			if err != nil {
				http.Error(w, `{"error":"failed to create template"}`, http.StatusInternalServerError)
				return
			}
			id, _ = res.LastInsertId()
		}

		t, err := scanTemplate(appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, name, driver, host, port, database, ssl, tags,
			        ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at
			 FROM connection_templates WHERE id = ?`), id))
		if err != nil {
			http.Error(w, `{"error":"failed to fetch template"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(t)
	}
}

func UpdateConnectionTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		idStr := strings.TrimPrefix(r.URL.Path, "/api/connection-templates/")
		tmplID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")
		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		// Only owner or admin may update
		var ownerID int64
		appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT owner_id FROM connection_templates WHERE id = ?`), tmplID).Scan(&ownerID)
		if userRole != "admin" && ownerID != userID {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		var in ConnectionTemplateInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if !allowedDrivers[in.Driver] {
			http.Error(w, `{"error":"invalid driver"}`, http.StatusBadRequest)
			return
		}
		if in.Visibility == "" {
			in.Visibility = "shared"
		}
		if in.SSHPort == 0 {
			in.SSHPort = 22
		}

		ssl := 0
		if in.SSL {
			ssl = 1
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")

		_, err = appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE connection_templates
			 SET name=?, driver=?, host=?, port=?, database=?, ssl=?, tags=?,
			     ssh_host=?, ssh_port=?, ssh_user=?, description=?, visibility=?, updated_at=?
			 WHERE id=?`),
			in.Name, in.Driver, in.Host, in.Port, in.Database, ssl, in.Tags,
			in.SSHHost, in.SSHPort, in.SSHUser, in.Description, in.Visibility, now, tmplID,
		)
		if err != nil {
			http.Error(w, `{"error":"failed to update template"}`, http.StatusInternalServerError)
			return
		}

		t, err := scanTemplate(appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, name, driver, host, port, database, ssl, tags,
			        ssh_host, ssh_port, ssh_user, description, visibility, owner_id, created_at, updated_at
			 FROM connection_templates WHERE id = ?`), tmplID))
		if err != nil {
			http.Error(w, `{"error":"failed to fetch template"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(t)
	}
}

func DeleteConnectionTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		idStr := strings.TrimPrefix(r.URL.Path, "/api/connection-templates/")
		tmplID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")
		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		var ownerID int64
		appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT owner_id FROM connection_templates WHERE id = ?`), tmplID).Scan(&ownerID)
		if userRole != "admin" && ownerID != userID {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		if _, err := appdb.DB.Exec(appdb.ConvertQuery(
			`DELETE FROM connection_templates WHERE id = ?`), tmplID); err != nil {
			http.Error(w, `{"error":"failed to delete template"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
