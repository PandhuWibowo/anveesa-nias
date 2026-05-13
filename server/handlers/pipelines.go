package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// ── Models ───────────────────────────────────────────────────────────────────

type Pipeline struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CreatedBy   *int64     `json:"created_by"`
	Status      string     `json:"status"`
	Schedule    *string    `json:"schedule"`
	LastRunAt   *time.Time `json:"last_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Nodes       []PipelineNode `json:"nodes,omitempty"`
	Edges       []PipelineEdge `json:"edges,omitempty"`
}

type PipelineNode struct {
	ID           int64          `json:"id"`
	PipelineID   int64          `json:"pipeline_id"`
	NodeType     string         `json:"node_type"`
	ConnectionID *int64         `json:"connection_id"`
	Config       map[string]any `json:"config"`
	PositionX    float64        `json:"position_x"`
	PositionY    float64        `json:"position_y"`
	Label        string         `json:"label"`
}

type PipelineEdge struct {
	ID           int64 `json:"id"`
	PipelineID   int64 `json:"pipeline_id"`
	SourceNodeID int64 `json:"source_node_id"`
	TargetNodeID int64 `json:"target_node_id"`
}

type PipelineRun struct {
	ID            int64      `json:"id"`
	PipelineID    int64      `json:"pipeline_id"`
	TriggeredBy   string     `json:"triggered_by"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	RowsProcessed int64      `json:"rows_processed"`
	ErrorMessage  *string    `json:"error_message"`
}

type PipelineRunLog struct {
	ID           int64     `json:"id"`
	RunID        int64     `json:"run_id"`
	NodeID       *int64    `json:"node_id"`
	NodeLabel    string    `json:"node_label"`
	Message      string    `json:"message"`
	RowsAffected int64     `json:"rows_affected"`
	DurationMs   int64     `json:"duration_ms"`
	LoggedAt     time.Time `json:"logged_at"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func parsePipelineID(r *http.Request, prefix string) (int64, bool) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	return id, err == nil
}

func scanPipelineRun(rows interface{ Scan(...any) error }) (PipelineRun, error) {
	var run PipelineRun
	var finishedAt *string
	var errMsg *string
	err := rows.Scan(&run.ID, &run.PipelineID, &run.TriggeredBy, &run.Status,
		&run.StartedAt, &finishedAt, &run.RowsProcessed, &errMsg)
	if err != nil {
		return run, err
	}
	if finishedAt != nil {
		t, _ := time.Parse("2006-01-02 15:04:05", *finishedAt)
		run.FinishedAt = &t
	}
	run.ErrorMessage = errMsg
	return run, nil
}

// ── CRUD Handlers ─────────────────────────────────────────────────────────────

func ListPipelines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := appdb.DB.QueryContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, name, description, created_by, status, schedule, last_run_at, created_at, updated_at
			 FROM pipelines ORDER BY created_at DESC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		pipelines := []Pipeline{}
		for rows.Next() {
			var p Pipeline
			var lastRunAt *string
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.Status,
				&p.Schedule, &lastRunAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
				continue
			}
			if lastRunAt != nil {
				t, _ := time.Parse("2006-01-02 15:04:05", *lastRunAt)
				p.LastRunAt = &t
			}
			pipelines = append(pipelines, p)
		}
		json.NewEncoder(w).Encode(pipelines)
	}
}

func CreatePipeline() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		username := r.Header.Get("X-Username")

		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, jsonError("name required"), http.StatusBadRequest)
			return
		}

		var userID *int64
		var uid int64
		if err := appdb.DB.QueryRowContext(r.Context(),
			appdb.ConvertQuery(`SELECT id FROM users WHERE username = ? LIMIT 1`), username).Scan(&uid); err == nil {
			userID = &uid
		}

		result, err := appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(
			`INSERT INTO pipelines (name, description, created_by, status) VALUES (?, ?, ?, 'draft')`),
			body.Name, body.Description, userID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		id, _ := result.LastInsertId()
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	}
}

func GetPipeline() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		id, ok := parsePipelineID(r, "/api/pipelines/")
		if !ok {
			http.Error(w, jsonError("invalid id"), http.StatusBadRequest)
			return
		}

		var p Pipeline
		var lastRunAt *string
		err := appdb.DB.QueryRowContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, name, description, created_by, status, schedule, last_run_at, created_at, updated_at
			 FROM pipelines WHERE id = ?`), id).Scan(
			&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.Status,
			&p.Schedule, &lastRunAt, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			http.Error(w, jsonError("pipeline not found"), http.StatusNotFound)
			return
		}
		if lastRunAt != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *lastRunAt)
			p.LastRunAt = &t
		}

		// Load nodes
		nodeRows, err := appdb.DB.QueryContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, pipeline_id, node_type, connection_id, config, position_x, position_y, label
			 FROM pipeline_nodes WHERE pipeline_id = ? ORDER BY id`), id)
		if err == nil {
			defer nodeRows.Close()
			for nodeRows.Next() {
				var n PipelineNode
				var configJSON string
				if err := nodeRows.Scan(&n.ID, &n.PipelineID, &n.NodeType, &n.ConnectionID,
					&configJSON, &n.PositionX, &n.PositionY, &n.Label); err != nil {
					continue
				}
				json.Unmarshal([]byte(configJSON), &n.Config)
				p.Nodes = append(p.Nodes, n)
			}
		}
		if p.Nodes == nil {
			p.Nodes = []PipelineNode{}
		}

		// Load edges
		edgeRows, err := appdb.DB.QueryContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, pipeline_id, source_node_id, target_node_id
			 FROM pipeline_edges WHERE pipeline_id = ? ORDER BY id`), id)
		if err == nil {
			defer edgeRows.Close()
			for edgeRows.Next() {
				var e PipelineEdge
				edgeRows.Scan(&e.ID, &e.PipelineID, &e.SourceNodeID, &e.TargetNodeID)
				p.Edges = append(p.Edges, e)
			}
		}
		if p.Edges == nil {
			p.Edges = []PipelineEdge{}
		}

		json.NewEncoder(w).Encode(p)
	}
}

func UpdatePipeline() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		id, ok := parsePipelineID(r, "/api/pipelines/")
		if !ok {
			http.Error(w, jsonError("invalid id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Status      string         `json:"status"`
			Schedule    *string        `json:"schedule"`
			Nodes       []PipelineNode `json:"nodes"`
			Edges       []PipelineEdge `json:"edges"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid body"), http.StatusBadRequest)
			return
		}

		if _, err := appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(
			`UPDATE pipelines SET name=?, description=?, status=?, schedule=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`),
			body.Name, body.Description, body.Status, body.Schedule, id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		// Replace nodes and edges
		appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(`DELETE FROM pipeline_nodes WHERE pipeline_id=?`), id)

		nodeIDMap := map[int64]int64{} // temp_id → real_id
		for _, n := range body.Nodes {
			configJSON, _ := json.Marshal(n.Config)
			result, err := appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(
				`INSERT INTO pipeline_nodes (pipeline_id, node_type, connection_id, config, position_x, position_y, label)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`),
				id, n.NodeType, n.ConnectionID, string(configJSON), n.PositionX, n.PositionY, n.Label)
			if err == nil {
				newID, _ := result.LastInsertId()
				nodeIDMap[n.ID] = newID
			}
		}

		appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(`DELETE FROM pipeline_edges WHERE pipeline_id=?`), id)
		for _, e := range body.Edges {
			srcID := nodeIDMap[e.SourceNodeID]
			tgtID := nodeIDMap[e.TargetNodeID]
			if srcID == 0 || tgtID == 0 {
				continue
			}
			appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(
				`INSERT INTO pipeline_edges (pipeline_id, source_node_id, target_node_id) VALUES (?, ?, ?)`),
				id, srcID, tgtID)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func DeletePipeline() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		id, ok := parsePipelineID(r, "/api/pipelines/")
		if !ok {
			http.Error(w, jsonError("invalid id"), http.StatusBadRequest)
			return
		}

		if _, err := appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(`DELETE FROM pipelines WHERE id=?`), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func TriggerPipelineRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// path: /api/pipelines/{id}/run
		path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		pipelineID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid id"), http.StatusBadRequest)
			return
		}

		username := r.Header.Get("X-Username")

		result, err := appdb.DB.ExecContext(r.Context(), appdb.ConvertQuery(
			`INSERT INTO pipeline_runs (pipeline_id, triggered_by, status) VALUES (?, 'manual', 'running')`),
			pipelineID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		runID, _ := result.LastInsertId()

		go RunPipeline(pipelineID, runID, username)

		json.NewEncoder(w).Encode(map[string]any{"run_id": runID})
	}
}

func ListPipelineRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// path: /api/pipelines/{id}/runs
		path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
		parts := strings.Split(path, "/")
		pipelineID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid id"), http.StatusBadRequest)
			return
		}

		rows, err := appdb.DB.QueryContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, pipeline_id, triggered_by, status, started_at, finished_at, rows_processed, error_message
			 FROM pipeline_runs WHERE pipeline_id = ? ORDER BY started_at DESC LIMIT 50`), pipelineID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		runs := []PipelineRun{}
		for rows.Next() {
			run, err := scanPipelineRun(rows)
			if err == nil {
				runs = append(runs, run)
			}
		}
		json.NewEncoder(w).Encode(runs)
	}
}

func GetRunLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// path: /api/pipelines/{id}/runs/{runId}/logs
		path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
		parts := strings.Split(path, "/")
		if len(parts) < 4 {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		runID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid run id"), http.StatusBadRequest)
			return
		}

		rows, err := appdb.DB.QueryContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, run_id, node_id, node_label, message, rows_affected, duration_ms, logged_at
			 FROM pipeline_run_logs WHERE run_id = ? ORDER BY logged_at ASC`), runID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		logs := []PipelineRunLog{}
		for rows.Next() {
			var l PipelineRunLog
			if err := rows.Scan(&l.ID, &l.RunID, &l.NodeID, &l.NodeLabel, &l.Message,
				&l.RowsAffected, &l.DurationMs, &l.LoggedAt); err == nil {
				logs = append(logs, l)
			}
		}
		json.NewEncoder(w).Encode(logs)
	}
}

func GetPipelineRunStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// path: /api/pipelines/{id}/runs/{runId}
		path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
		parts := strings.Split(path, "/")
		if len(parts) < 3 {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		runID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid run id"), http.StatusBadRequest)
			return
		}

		var run PipelineRun
		var finishedAt *string
		var errMsg *string
		err = appdb.DB.QueryRowContext(r.Context(), appdb.ConvertQuery(
			`SELECT id, pipeline_id, triggered_by, status, started_at, finished_at, rows_processed, error_message
			 FROM pipeline_runs WHERE id = ?`), runID).Scan(
			&run.ID, &run.PipelineID, &run.TriggeredBy, &run.Status,
			&run.StartedAt, &finishedAt, &run.RowsProcessed, &errMsg)
		if err != nil {
			http.Error(w, jsonError("run not found"), http.StatusNotFound)
			return
		}
		if finishedAt != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *finishedAt)
			run.FinishedAt = &t
		}
		run.ErrorMessage = errMsg
		json.NewEncoder(w).Encode(run)
	}
}
