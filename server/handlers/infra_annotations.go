package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

// InfraAnnotation represents an infrastructure annotation stored in the app DB.
type InfraAnnotation struct {
	ID          int64  `json:"id"`
	ConnID      int64  `json:"conn_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       string `json:"color"`
	EventTime   string `json:"event_time"`
	CreatedBy   *int64 `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// ListInfraAnnotations returns annotations for a connection, optionally filtered by time range.
// GET /api/connections/{id}/infra-annotations?from=...&to=...
func ListInfraAnnotations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		fromStr := strings.TrimSpace(r.URL.Query().Get("from"))
		toStr := strings.TrimSpace(r.URL.Query().Get("to"))

		var rows interface {
			Close() error
			Next() bool
			Scan(dest ...any) error
			Err() error
		}

		const baseQ = `SELECT id, conn_id, title, description, color, event_time, created_by, created_at
		               FROM infra_annotations WHERE conn_id = ?`

		if fromStr != "" && toStr != "" {
			rows, err = appdb.DB.Query(appdb.ConvertQuery(
				baseQ+` AND event_time >= ? AND event_time <= ? ORDER BY event_time ASC`),
				connID, fromStr, toStr,
			)
		} else {
			rows, err = appdb.DB.Query(appdb.ConvertQuery(
				baseQ+` ORDER BY event_time ASC`),
				connID,
			)
		}
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []InfraAnnotation
		for rows.Next() {
			var a InfraAnnotation
			if err := rows.Scan(
				&a.ID, &a.ConnID, &a.Title, &a.Description, &a.Color,
				&a.EventTime, &a.CreatedBy, &a.CreatedAt,
			); err != nil {
				http.Error(w, jsonError("failed to read annotations"), http.StatusInternalServerError)
				return
			}
			list = append(list, a)
		}
		if list == nil {
			list = []InfraAnnotation{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

// CreateInfraAnnotation creates a new annotation for a connection.
// POST /api/connections/{id}/infra-annotations
func CreateInfraAnnotation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Color       string `json:"color"`
			EventTime   string `json:"event_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Title == "" || body.EventTime == "" {
			http.Error(w, jsonError("title and event_time are required"), http.StatusBadRequest)
			return
		}
		if body.Color == "" {
			body.Color = "#6366f1"
		}

		userID, _, _ := currentUserFromHeaders(r)
		var createdBy *int64
		if userID != 0 {
			createdBy = &userID
		}

		res, err := appdb.DB.Exec(appdb.ConvertQuery(
			`INSERT INTO infra_annotations (conn_id, title, description, color, event_time, created_by)
			 VALUES (?, ?, ?, ?, ?, ?)`),
			connID, body.Title, body.Description, body.Color, body.EventTime, createdBy,
		)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()

		var annotation InfraAnnotation
		appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, conn_id, title, description, color, event_time, created_by, created_at
			 FROM infra_annotations WHERE id = ?`), id).Scan(
			&annotation.ID, &annotation.ConnID, &annotation.Title, &annotation.Description,
			&annotation.Color, &annotation.EventTime, &annotation.CreatedBy, &annotation.CreatedAt,
		)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(annotation)
	}
}

// DeleteInfraAnnotation deletes an annotation.
// DELETE /api/connections/{id}/infra-annotations/{annotationID}
func DeleteInfraAnnotation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		annotationID, err := infraAnnotationIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid annotation id"), http.StatusBadRequest)
			return
		}

		res, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM infra_annotations WHERE id = ?`), annotationID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.Error(w, jsonError("annotation not found"), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// infraAnnotationIDFromPath extracts the annotation ID from a path like
// /api/connections/{connID}/infra-annotations/{annotationID}
func infraAnnotationIDFromPath(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/connections/")
	parts := strings.Split(trimmed, "/")
	// parts[0] = connID, parts[1] = "infra-annotations", parts[2] = annotationID
	if len(parts) < 3 {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseInt(parts[2], 10, 64)
}
