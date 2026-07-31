package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

type pipelineRuntime struct {
	params       map[string]any
	businessDate string
	payload      map[string]any
}

var pipelineTemplatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_\.\-]+)\s*\}\}`)

// RunPipeline executes a pipeline asynchronously.
// It is called in a goroutine from TriggerPipelineRun.
func RunPipeline(pipelineID, runID int64, triggeredBy string) {
	ctx := context.Background()
	startedAt := time.Now()
	rt := loadPipelineRuntime(ctx, runID)

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
		payloadJSON, _ := json.Marshal(rt.payload)
		appdb.DB.ExecContext(ctx, appdb.ConvertQuery(
			`UPDATE pipeline_runs SET status=?, finished_at=CURRENT_TIMESTAMP, rows_processed=?, error_message=?, return_payload=? WHERE id=?`),
			status, rowsProcessed, errMsgPtr, string(payloadJSON), runID)
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
			Payload:    map[string]any{"run_id": runID, "pipeline_id": pipelineID, "business_date": rt.businessDate},
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
			if err := execSourceNode(ctx, n, rt, buffers, colNames); err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Fetched %d rows", len(buffers[n.id])),
				int64(len(buffers[n.id])), time.Since(stepStart).Milliseconds())

		case "transform_sql":
			if err := execTransformSQLNode(ctx, n, rt, buffers, colNames, inEdges); err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Returned %d rows, payload ready", len(buffers[n.id])),
				int64(len(buffers[n.id])), time.Since(stepStart).Milliseconds())

		case "external_http":
			if err := execExternalHTTPNode(ctx, n, rt); err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			logStep(nodeIDPtr, n.label, "HTTP/API hook completed", 0, time.Since(stepStart).Milliseconds())

		case "sink_table":
			var upstreamID int64
			if srcs := inEdges[n.id]; len(srcs) > 0 {
				upstreamID = srcs[0]
			}
			rowsWritten, err := execSinkTable(ctx, n, rt, buffers[upstreamID], colNames[upstreamID])
			if err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			totalRows += rowsWritten
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Wrote %d rows", rowsWritten),
				rowsWritten, time.Since(stepStart).Milliseconds())

		case "sink_object_storage":
			var upstreamID int64
			if srcs := inEdges[n.id]; len(srcs) > 0 {
				upstreamID = srcs[0]
			}
			objectKey, rowsExported, err := execSinkObjectStorage(ctx, n, rt, buffers[upstreamID], colNames[upstreamID])
			if err != nil {
				logStep(nodeIDPtr, n.label, "Error: "+err.Error(), 0, time.Since(stepStart).Milliseconds())
				finishRun("failed", err.Error(), totalRows)
				return
			}
			totalRows += rowsExported
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Exported %d rows → %s", rowsExported, objectKey),
				rowsExported, time.Since(stepStart).Milliseconds())

		default:
			logStep(nodeIDPtr, n.label, fmt.Sprintf("Skipping unsupported node type: %s", n.nodeType), 0, 0)
		}
	}

	finishRun("success", "", totalRows)
}

func execSourceNode(ctx context.Context, n *execNode, rt *pipelineRuntime, buffers map[int64][][]any, colNames map[int64][]string) error {
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
		s = renderPipelineTemplate(s, rt)
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("node %q: sql required", n.label)
		}
		sqlStr = s
	case "source_table":
		table, _ := n.config["table"].(string)
		schema, _ := n.config["schema"].(string)
		table = renderPipelineTemplate(table, rt)
		schema = renderPipelineTemplate(schema, rt)
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
	setNodePayload(rt, n, cols, result)
	return nil
}

func execSinkTable(ctx context.Context, n *execNode, rt *pipelineRuntime, rows [][]any, cols []string) (int64, error) {
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
	table = renderPipelineTemplate(table, rt)
	schema = renderPipelineTemplate(schema, rt)
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

	if preSQL, _ := n.config["pre_sql"].(string); strings.TrimSpace(preSQL) != "" {
		if _, err := tx.ExecContext(ctx, renderPipelineTemplate(preSQL, rt)); err != nil {
			return 0, fmt.Errorf("node %q: pre_sql failed: %w", n.label, err)
		}
	} else if mode, _ := n.config["write_mode"].(string); mode == "replace" {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", tableRef)); err != nil {
			return 0, fmt.Errorf("node %q: replace delete failed: %w", n.label, err)
		}
	}

	prepared, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return 0, fmt.Errorf("node %q: prepare stmt: %w", n.label, err)
	}
	defer prepared.Close()

	inserted := int64(0)
	for rowIdx, row := range rows {
		if len(row) != len(cols) {
			return 0, fmt.Errorf("node %q: insert row %d has %d values but %d columns (%s)",
				n.label, rowIdx+1, len(row), len(cols), strings.Join(cols, ", "))
		}
		args := make([]any, len(row))
		copy(args, row)
		if _, err := prepared.ExecContext(ctx, args...); err != nil {
			return 0, fmt.Errorf("node %q: insert failed at row %d into %s (%s): %w",
				n.label, rowIdx+1, tableRef, strings.Join(cols, ", "), err)
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("node %q: commit: %w", n.label, err)
	}

	return inserted, nil
}

func execSinkObjectStorage(ctx context.Context, n *execNode, rt *pipelineRuntime, rows [][]any, cols []string) (objectKey string, rowsExported int64, err error) {
	if n.connectionID == nil {
		return "", 0, fmt.Errorf("node %q: connection_id required", n.label)
	}
	if len(rows) == 0 || len(cols) == 0 {
		return "", 0, nil
	}

	format, _ := n.config["format"].(string)
	if format == "" {
		format = "csv"
	}
	subfolder, _ := n.config["subfolder"].(string)
	prefix, _ := n.config["filename_prefix"].(string)
	subfolder = renderPipelineTemplate(subfolder, rt)
	prefix = renderPipelineTemplate(prefix, rt)
	if prefix == "" {
		prefix = "export"
	}
	tableName, _ := n.config["table_name"].(string)
	tableName = renderPipelineTemplate(tableName, rt)

	dest, err := fetchBucketConn(*n.connectionID)
	if err != nil {
		return "", 0, fmt.Errorf("node %q: %w", n.label, err)
	}

	var buf bytes.Buffer
	var ext string

	switch format {
	case "sql":
		ext = "sql"
		if tableName == "" {
			tableName = "exported_table"
		}
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			quotedCols[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
		}
		colList := strings.Join(quotedCols, ", ")
		for _, row := range rows {
			vals := make([]string, len(row))
			for i, v := range row {
				if v == nil {
					vals[i] = "NULL"
				} else {
					s := fmt.Sprintf("%v", v)
					s = strings.ReplaceAll(s, "'", "''")
					vals[i] = "'" + s + "'"
				}
			}
			fmt.Fprintf(&buf, "INSERT INTO %s (%s) VALUES (%s);\n",
				`"`+strings.ReplaceAll(tableName, `"`, `""`)+`"`,
				colList,
				strings.Join(vals, ", "),
			)
		}
	default: // csv
		ext = "csv"
		w := csv.NewWriter(&buf)
		w.Write(cols)
		for _, row := range rows {
			rec := make([]string, len(row))
			for i, v := range row {
				if v == nil {
					rec[i] = ""
				} else {
					rec[i] = fmt.Sprintf("%v", v)
				}
			}
			w.Write(rec)
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return "", 0, fmt.Errorf("node %q: csv write: %w", n.label, err)
		}
	}

	ts := time.Now().UTC().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.%s", prefix, ts, ext)
	key := filename
	if sf := strings.Trim(strings.TrimSpace(subfolder), "/"); sf != "" {
		key = sf + "/" + filename
	}

	if err := uploadToBucket(ctx, dest, key, buf.Bytes()); err != nil {
		return "", 0, fmt.Errorf("node %q: upload failed: %w", n.label, err)
	}

	rt.payload[payloadKey(n)] = map[string]any{"object_key": key, "rows": len(rows), "format": ext}
	rt.payload[fmt.Sprintf("node_%d", n.id)] = rt.payload[payloadKey(n)]
	return key, int64(len(rows)), nil
}

func execTransformSQLNode(ctx context.Context, n *execNode, rt *pipelineRuntime, buffers map[int64][][]any, colNames map[int64][]string, inEdges map[int64][]int64) error {
	sqlStr, _ := n.config["sql"].(string)
	sqlStr = renderPipelineTemplate(sqlStr, rt)

	// If no SQL is configured, this transform acts as an idempotent pass-through
	// and only returns a payload describing the upstream result.
	if strings.TrimSpace(sqlStr) == "" {
		var upstreamID int64
		if srcs := inEdges[n.id]; len(srcs) > 0 {
			upstreamID = srcs[0]
		}
		buffers[n.id] = buffers[upstreamID]
		colNames[n.id] = colNames[upstreamID]
		setNodePayload(rt, n, colNames[n.id], buffers[n.id])
		return nil
	}

	if n.connectionID == nil {
		return fmt.Errorf("node %q: connection_id required for SQL transform", n.label)
	}
	db, _, err := GetDB(*n.connectionID)
	if err != nil {
		return fmt.Errorf("node %q: cannot connect: %w", n.label, err)
	}

	rows, err := db.QueryContext(ctx, sqlStr)
	if err != nil {
		return fmt.Errorf("node %q: transform query failed: %w", n.label, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("node %q: get columns: %w", n.label, err)
	}

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
	colNames[n.id] = cols
	setNodePayload(rt, n, cols, result)
	return nil
}

func execExternalHTTPNode(ctx context.Context, n *execNode, rt *pipelineRuntime) error {
	urlStr, _ := n.config["url"].(string)
	urlStr = renderPipelineTemplate(urlStr, rt)
	if strings.TrimSpace(urlStr) == "" {
		return fmt.Errorf("node %q: url required", n.label)
	}

	method, _ := n.config["method"].(string)
	if method == "" {
		method = http.MethodPost
	}
	body, _ := n.config["body"].(string)
	body = renderPipelineTemplate(body, rt)

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), urlStr, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("node %q: build request: %w", n.label, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if headers, ok := n.config["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, renderPipelineTemplate(fmt.Sprint(v), rt))
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("node %q: request failed: %w", n.label, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("node %q: HTTP %d: %s", n.label, resp.StatusCode, string(respBody))
	}

	payload := map[string]any{
		"status": resp.StatusCode,
		"body":   string(respBody),
	}
	var parsed any
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		payload["json"] = parsed
	}
	rt.payload[payloadKey(n)] = payload
	rt.payload[fmt.Sprintf("node_%d", n.id)] = payload
	return nil
}

func loadPipelineRuntime(ctx context.Context, runID int64) *pipelineRuntime {
	rt := &pipelineRuntime{
		params:  map[string]any{},
		payload: map[string]any{},
	}
	var paramsJSON string
	_ = appdb.DB.QueryRowContext(ctx, appdb.ConvertQuery(
		`SELECT COALESCE(business_date,''), COALESCE(run_params,'{}') FROM pipeline_runs WHERE id=?`),
		runID).Scan(&rt.businessDate, &paramsJSON)
	_ = json.Unmarshal([]byte(paramsJSON), &rt.params)
	if rt.params == nil {
		rt.params = map[string]any{}
	}
	if rt.businessDate != "" {
		rt.params["business_date"] = rt.businessDate
	}
	return rt
}

func renderPipelineTemplate(input string, rt *pipelineRuntime) string {
	if input == "" || rt == nil {
		return input
	}
	return pipelineTemplatePattern.ReplaceAllStringFunc(input, func(token string) string {
		matches := pipelineTemplatePattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		path := matches[1]
		if path == "business_date" {
			return rt.businessDate
		}
		if strings.HasPrefix(path, "params.") {
			return fmt.Sprint(lookupPath(rt.params, strings.TrimPrefix(path, "params.")))
		}
		if strings.HasPrefix(path, "payload.") {
			return fmt.Sprint(lookupPath(rt.payload, strings.TrimPrefix(path, "payload.")))
		}
		return token
	})
}

func lookupPath(root any, path string) any {
	cur := root
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[part]
	}
	if cur == nil {
		return ""
	}
	return cur
}

func setNodePayload(rt *pipelineRuntime, n *execNode, cols []string, rows [][]any) {
	if rt == nil {
		return
	}
	first := map[string]any{}
	if len(rows) > 0 {
		for i, col := range cols {
			if i < len(rows[0]) {
				first[col] = rows[0][i]
			}
		}
	}
	payload := map[string]any{
		"rows":    len(rows),
		"columns": cols,
		"first":   first,
	}
	rt.payload[payloadKey(n)] = payload
	rt.payload[fmt.Sprintf("node_%d", n.id)] = payload
}

func payloadKey(n *execNode) string {
	key := strings.TrimSpace(n.label)
	if key == "" {
		key = fmt.Sprintf("node_%d", n.id)
	}
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, ".", "_")
	key = strings.ReplaceAll(key, "-", "_")
	return key
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
