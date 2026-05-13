package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type execNode struct {
	id           int64
	nodeType     string
	connectionID *int64
	config       map[string]any
	label        string
}

// RunPipeline executes a pipeline asynchronously.
// It is called in a goroutine from TriggerPipelineRun.
func RunPipeline(pipelineID, runID int64, triggeredBy string) {
	ctx := context.Background()
	startedAt := time.Now()

	logStep := func(nodeID *int64, label, message string, rowsAffected, durationMs int64) {
		appdb.DB.ExecContext(ctx, appdb.ConvertQuery(
			`INSERT INTO pipeline_run_logs (run_id, node_id, node_label, message, rows_affected, duration_ms)
			 VALUES (?, ?, ?, ?, ?, ?)`),
			runID, nodeID, label, message, rowsAffected, durationMs)
	}

	finishRun := func(status, errMsg string, rowsProcessed int64) {
		_ = time.Since(startedAt).Milliseconds()

		var errMsgPtr *string
		if errMsg != "" {
			errMsgPtr = &errMsg
		}
		appdb.DB.ExecContext(ctx, appdb.ConvertQuery(
			`UPDATE pipeline_runs SET status=?, finished_at=CURRENT_TIMESTAMP, rows_processed=?, error_message=? WHERE id=?`),
			status, rowsProcessed, errMsgPtr, runID)
		appdb.DB.ExecContext(ctx, appdb.ConvertQuery(
			`UPDATE pipelines SET last_run_at=CURRENT_TIMESTAMP WHERE id=?`), pipelineID)

		severity := "info"
		title := fmt.Sprintf("Pipeline run #%d succeeded", runID)
		msg := fmt.Sprintf("Processed %d rows", rowsProcessed)
		if status == "failed" {
			severity = "error"
			title = fmt.Sprintf("Pipeline run #%d failed", runID)
			msg = errMsg
		}
		EmitNotification(NotificationEventInput{
			EventType:  "pipeline_run",
			Category:   "pipeline",
			Severity:   severity,
			Title:      title,
			Message:    msg,
			EntityType: "pipeline",
			EntityID:   pipelineID,
			Payload:    map[string]any{"run_id": runID, "pipeline_id": pipelineID},
		})
	}

	// Load nodes
	nodeRows, err := appdb.DB.QueryContext(ctx, appdb.ConvertQuery(
		`SELECT id, node_type, connection_id, config, label
		 FROM pipeline_nodes WHERE pipeline_id = ? ORDER BY id`), pipelineID)
	if err != nil {
		logStep(nil, "executor", "Failed to load pipeline nodes: "+err.Error(), 0, 0)
		finishRun("failed", err.Error(), 0)
		return
	}
	defer nodeRows.Close()

	nodeMap := map[int64]*execNode{}
	var nodeOrder []int64
	for nodeRows.Next() {
		n := &execNode{}
		var configJSON string
		if err := nodeRows.Scan(&n.id, &n.nodeType, &n.connectionID, &configJSON, &n.label); err != nil {
			continue
		}
		json.Unmarshal([]byte(configJSON), &n.config)
		if n.config == nil {
			n.config = map[string]any{}
		}
		nodeMap[n.id] = n
		nodeOrder = append(nodeOrder, n.id)
	}
	nodeRows.Close()

	// Load edges
	edgeRows, err := appdb.DB.QueryContext(ctx, appdb.ConvertQuery(
		`SELECT source_node_id, target_node_id FROM pipeline_edges WHERE pipeline_id = ?`), pipelineID)
	if err != nil {
		finishRun("failed", err.Error(), 0)
		return
	}
	defer edgeRows.Close()

	inEdges := map[int64][]int64{}
	for edgeRows.Next() {
		var src, tgt int64
		if err := edgeRows.Scan(&src, &tgt); err == nil {
			inEdges[tgt] = append(inEdges[tgt], src)
		}
	}
	edgeRows.Close()

	// Topological sort
	sorted, err := topoSort(nodeOrder, inEdges)
	if err != nil {
		logStep(nil, "executor", "Cycle detected: "+err.Error(), 0, 0)
		finishRun("failed", "cycle detected in pipeline graph", 0)
		return
	}

	type row = []any
	buffers := map[int64][]row{}
	colNames := map[int64][]string{}
	totalRows := int64(0)

	for _, nodeID := range sorted {
		n, ok := nodeMap[nodeID]
		if !ok {
			continue
		}

		stepStart := time.Now()
		nodeIDPtr := &n.id

		switch n.nodeType {
		case "source_query", "source_table":
			if err := execSourceNode(ctx, n, buffers, colNames); err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Fetched %d rows", len(buffers[n.id])),
				int64(len(buffers[n.id])), time.Since(stepStart).Milliseconds())

		case "sink_table":
			var upstreamID int64
			if srcs := inEdges[n.id]; len(srcs) > 0 {
				upstreamID = srcs[0]
			}
			rowsWritten, err := execSinkTable(ctx, n, buffers[upstreamID], colNames[upstreamID])
			if err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			totalRows += rowsWritten
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Wrote %d rows", rowsWritten),
				rowsWritten, time.Since(stepStart).Milliseconds())

		default:
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Skipping unsupported node type: %s", n.nodeType), 0, 0)
		}
	}

	finishRun("success", "", totalRows)
}

func execSourceNode(ctx context.Context, n *execNode, buffers map[int64][][]any, colNames map[int64][]string) error {
	if n.connectionID == nil {
		return fmt.Errorf("node %q: connection_id required", n.label)
	}

	db, _, err := GetDB(*n.connectionID)
	if err != nil {
		return fmt.Errorf("node %q: cannot connect: %w", n.label, err)
	}

	var sqlStr string
	switch n.nodeType {
	case "source_query":
		s, _ := n.config["sql"].(string)
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("node %q: sql required", n.label)
		}
		sqlStr = s
	case "source_table":
		table, _ := n.config["table"].(string)
		schema, _ := n.config["schema"].(string)
		if table == "" {
			return fmt.Errorf("node %q: table required", n.label)
		}
		if schema != "" {
			sqlStr = fmt.Sprintf("SELECT * FROM %s.%s", schema, table)
		} else {
			sqlStr = fmt.Sprintf("SELECT * FROM %s", table)
		}
		if limit, ok := n.config["limit"].(float64); ok && limit > 0 {
			sqlStr += fmt.Sprintf(" LIMIT %d", int(limit))
		}
	}

	rows, err := db.QueryContext(ctx, sqlStr)
	if err != nil {
		return fmt.Errorf("node %q: query failed: %w", n.label, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("node %q: get columns: %w", n.label, err)
	}

	colNames[n.id] = cols
	var result [][]any

	const maxRows = 500_000
	for rows.Next() {
		if len(result) >= maxRows {
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("node %q: scan row: %w", n.label, err)
		}
		result = append(result, vals)
	}

	buffers[n.id] = result
	return nil
}

func execSinkTable(ctx context.Context, n *execNode, rows [][]any, cols []string) (int64, error) {
	if n.connectionID == nil {
		return 0, fmt.Errorf("node %q: connection_id required", n.label)
	}
	if len(rows) == 0 || len(cols) == 0 {
		return 0, nil
	}

	db, driver, err := GetDB(*n.connectionID)
	if err != nil {
		return 0, fmt.Errorf("node %q: cannot connect: %w", n.label, err)
	}

	table, _ := n.config["table"].(string)
	schema, _ := n.config["schema"].(string)
	if table == "" {
		return 0, fmt.Errorf("node %q: table required", n.label)
	}

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = quoteIdent(driver, c)
	}

	placeholders := make([]string, len(cols))
	for i := range cols {
		if driver == "postgres" {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}

	tableRef := qualifiedTableName(driver, schema, table)
	stmt := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		tableRef, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("node %q: begin tx: %w", n.label, err)
	}
	defer tx.Rollback()

	prepared, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return 0, fmt.Errorf("node %q: prepare stmt: %w", n.label, err)
	}
	defer prepared.Close()

	inserted := int64(0)
	for _, row := range rows {
		args := make([]any, len(row))
		copy(args, row)
		if _, err := prepared.ExecContext(ctx, args...); err != nil {
			continue
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("node %q: commit: %w", n.label, err)
	}

	return inserted, nil
}

func topoSort(nodeIDs []int64, inEdges map[int64][]int64) ([]int64, error) {
	// Build out-edges from in-edges
	outEdges := map[int64][]int64{}
	for tgt, srcs := range inEdges {
		for _, src := range srcs {
			outEdges[src] = append(outEdges[src], tgt)
		}
	}

	inDegree := map[int64]int{}
	for _, id := range nodeIDs {
		inDegree[id] = len(inEdges[id])
	}

	queue := []int64{}
	for _, id := range nodeIDs {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	sorted := []int64{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		sorted = append(sorted, cur)
		for _, next := range outEdges[cur] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) != len(nodeIDs) {
		return nil, fmt.Errorf("graph has a cycle")
	}
	return sorted, nil
}
