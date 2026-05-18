package handlers

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

type CassandraDashboardData struct {
	Driver         string                 `json:"driver"`
	Keyspace       string                 `json:"keyspace"`
	ClusterName    string                 `json:"cluster_name"`
	Version        string                 `json:"version"`
	CQLVersion     string                 `json:"cql_version"`
	DataCenter     string                 `json:"data_center"`
	Rack           string                 `json:"rack"`
	HostID         string                 `json:"host_id"`
	Keyspaces      int                    `json:"keyspaces"`
	Tables         int                    `json:"tables"`
	NativeProtocol string                 `json:"native_protocol"`
	Local          map[string]interface{} `json:"local"`
}

type CassandraKeyspaceSummary struct {
	Name          string `json:"name"`
	Replication   string `json:"replication"`
	DurableWrites bool   `json:"durable_writes"`
	TableCount    int    `json:"table_count"`
}

type CassandraTableSummary struct {
	KeyspaceName  string `json:"keyspace_name"`
	Name          string `json:"name"`
	Comment       string `json:"comment"`
	Columns       int    `json:"columns"`
	PartitionKey  string `json:"partition_key"`
	ClusteringKey string `json:"clustering_key"`
}

type CassandraColumnSummary struct {
	KeyspaceName string `json:"keyspace_name"`
	TableName    string `json:"table_name"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	Position     int    `json:"position"`
}

type CassandraResult struct {
	Columns    []string                 `json:"columns"`
	Rows       []map[string]interface{} `json:"rows"`
	RowCount   int                      `json:"row_count"`
	Applied    bool                     `json:"applied"`
	DurationMS int64                    `json:"duration_ms"`
}

type cassandraQueryRequest struct {
	Keyspace string `json:"keyspace"`
	CQL      string `json:"cql"`
	Limit    int    `json:"limit"`
}

func testCassandraInput(ctx context.Context, in ConnectionInput) error {
	session, err := newCassandraSession(ctx, in, "")
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Query("SELECT now() FROM system.local").WithContext(ctx).Exec()
}

func CassandraPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		session, _, err := openCassandraSessionFromRequest(r, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		start := time.Now()
		if err := session.Query("SELECT now() FROM system.local").WithContext(r.Context()).Exec(); err != nil {
			http.Error(w, jsonError("Cassandra ping failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "latency_ms": time.Since(start).Milliseconds()})
	}
}

func CassandraDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		session, in, err := openCassandraSessionFromRequest(r, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()

		data := CassandraDashboardData{Driver: "cassandra", Keyspace: in.Database, Local: map[string]interface{}{}}
		local := map[string]interface{}{}
		iter := session.Query(`SELECT cluster_name, cql_version, release_version, data_center, rack, host_id, native_protocol_version FROM system.local`).WithContext(r.Context()).Iter()
		if iter.MapScan(local) {
			data.Local = cassandraJSONMap(local)
			data.ClusterName = fmt.Sprint(local["cluster_name"])
			data.CQLVersion = fmt.Sprint(local["cql_version"])
			data.Version = fmt.Sprint(local["release_version"])
			data.DataCenter = fmt.Sprint(local["data_center"])
			data.Rack = fmt.Sprint(local["rack"])
			data.HostID = fmt.Sprint(local["host_id"])
			data.NativeProtocol = fmt.Sprint(local["native_protocol_version"])
		}
		if err := iter.Close(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		data.Keyspaces = countCassandraRows(r.Context(), session, `SELECT keyspace_name FROM system_schema.keyspaces`)
		data.Tables = countCassandraRows(r.Context(), session, `SELECT keyspace_name, table_name FROM system_schema.tables`)
		json.NewEncoder(w).Encode(data)
	}
}

func CassandraKeyspaces() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		session, _, err := openCassandraSessionFromRequest(r, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		tableCounts := map[string]int{}
		iterTables := session.Query(`SELECT keyspace_name, table_name FROM system_schema.tables`).WithContext(r.Context()).Iter()
		row := map[string]interface{}{}
		for iterTables.MapScan(row) {
			tableCounts[fmt.Sprint(row["keyspace_name"])]++
			row = map[string]interface{}{}
		}
		if err := iterTables.Close(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		result := []CassandraKeyspaceSummary{}
		iter := session.Query(`SELECT keyspace_name, replication, durable_writes FROM system_schema.keyspaces`).WithContext(r.Context()).Iter()
		row = map[string]interface{}{}
		for iter.MapScan(row) {
			name := fmt.Sprint(row["keyspace_name"])
			result = append(result, CassandraKeyspaceSummary{
				Name:          name,
				Replication:   fmt.Sprint(cassandraJSONValue(row["replication"])),
				DurableWrites: boolValue(row["durable_writes"]),
				TableCount:    tableCounts[name],
			})
			row = map[string]interface{}{}
		}
		if err := iter.Close(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func CassandraTables() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		keyspace := strings.TrimSpace(r.URL.Query().Get("keyspace"))
		if keyspace == "" {
			http.Error(w, jsonError("keyspace is required"), http.StatusBadRequest)
			return
		}
		session, _, err := openCassandraSessionFromRequest(r, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		columns, err := cassandraColumns(r.Context(), session, keyspace, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		counts := map[string]int{}
		partitions := map[string][]string{}
		clustering := map[string][]string{}
		for _, col := range columns {
			counts[col.TableName]++
			if col.Kind == "partition_key" {
				partitions[col.TableName] = append(partitions[col.TableName], col.Name)
			}
			if col.Kind == "clustering" {
				clustering[col.TableName] = append(clustering[col.TableName], col.Name)
			}
		}
		result := []CassandraTableSummary{}
		iter := session.Query(`SELECT keyspace_name, table_name, comment FROM system_schema.tables WHERE keyspace_name = ?`, keyspace).WithContext(r.Context()).Iter()
		row := map[string]interface{}{}
		for iter.MapScan(row) {
			name := fmt.Sprint(row["table_name"])
			result = append(result, CassandraTableSummary{
				KeyspaceName:  fmt.Sprint(row["keyspace_name"]),
				Name:          name,
				Comment:       fmt.Sprint(row["comment"]),
				Columns:       counts[name],
				PartitionKey:  strings.Join(partitions[name], ", "),
				ClusteringKey: strings.Join(clustering[name], ", "),
			})
			row = map[string]interface{}{}
		}
		if err := iter.Close(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func CassandraColumns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		keyspace := strings.TrimSpace(r.URL.Query().Get("keyspace"))
		table := strings.TrimSpace(r.URL.Query().Get("table"))
		if keyspace == "" || table == "" {
			http.Error(w, jsonError("keyspace and table are required"), http.StatusBadRequest)
			return
		}
		session, _, err := openCassandraSessionFromRequest(r, "")
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		cols, err := cassandraColumns(r.Context(), session, keyspace, table)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(cols)
	}
}

func CassandraRows() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		keyspace := strings.TrimSpace(r.URL.Query().Get("keyspace"))
		table := strings.TrimSpace(r.URL.Query().Get("table"))
		limit := clampLimit(r.URL.Query().Get("limit"), 100)
		if keyspace == "" || table == "" {
			http.Error(w, jsonError("keyspace and table are required"), http.StatusBadRequest)
			return
		}
		session, _, err := openCassandraSessionFromRequest(r, keyspace)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		cql := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d", quoteCQLIdent(keyspace), quoteCQLIdent(table), limit)
		result, err := runCassandraQuery(r.Context(), session, cql, limit)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func CassandraQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req cassandraQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("bad request"), http.StatusBadRequest)
			return
		}
		req.CQL = strings.TrimSpace(req.CQL)
		if req.CQL == "" {
			http.Error(w, jsonError("CQL is required"), http.StatusBadRequest)
			return
		}
		if strings.Count(req.CQL, ";") > 1 {
			http.Error(w, jsonError("run one CQL statement at a time"), http.StatusBadRequest)
			return
		}
		session, _, err := openCassandraSessionFromRequest(r, req.Keyspace)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer session.Close()
		result, err := runCassandraQuery(r.Context(), session, strings.TrimSuffix(req.CQL, ";"), normalizeLimit(req.Limit))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func openCassandraSessionFromRequest(r *http.Request, keyspace string) (*gocql.Session, ConnectionInput, error) {
	connID, err := connectionIDFromPath(r.URL.Path)
	if err != nil {
		return nil, ConnectionInput{}, fmt.Errorf("invalid connection id")
	}
	in, err := cassandraConnectionInput(connID)
	if err != nil {
		return nil, ConnectionInput{}, err
	}
	session, err := newCassandraSession(r.Context(), in, keyspace)
	return session, in, err
}

func cassandraConnectionInput(connID int64) (ConnectionInput, error) {
	var in ConnectionInput
	var ssl, disconnected int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT COALESCE(name,''), driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl, COALESCE(disconnected,0) FROM connections WHERE id=?`), connID,
	).Scan(&in.Name, &in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl, &disconnected)
	if err != nil {
		return in, fmt.Errorf("connection not found")
	}
	if disconnected == 1 {
		return in, fmt.Errorf("connection is disconnected")
	}
	if in.Driver != "cassandra" {
		return in, fmt.Errorf("connection is not Cassandra")
	}
	password, err := decryptCredential(encPassword)
	if err != nil {
		return in, fmt.Errorf("decryption error")
	}
	in.Password = password
	in.SSL = ssl == 1
	return in, nil
}

func newCassandraSession(ctx context.Context, in ConnectionInput, keyspaceOverride string) (*gocql.Session, error) {
	hosts, port, keyspace, username, password, ssl, err := cassandraConfigParts(in)
	if err != nil {
		return nil, err
	}
	if keyspaceOverride != "" {
		keyspace = keyspaceOverride
	}
	cluster := gocql.NewCluster(hosts...)
	cluster.Port = port
	cluster.Keyspace = keyspace
	cluster.Timeout = 8 * time.Second
	cluster.ConnectTimeout = 8 * time.Second
	cluster.Consistency = gocql.LocalOne
	cluster.NumConns = 2
	if username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: username, Password: password}
	}
	if ssl {
		cluster.SslOpts = &gocql.SslOptions{Config: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	type result struct {
		session *gocql.Session
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		session, err := cluster.CreateSession()
		ch <- result{session: session, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.session, res.err
	}
}

func cassandraConfigParts(in ConnectionInput) ([]string, int, string, string, string, bool, error) {
	host := strings.TrimSpace(in.Host)
	if host == "" {
		return nil, 0, "", "", "", false, fmt.Errorf("Cassandra contact point or URI is required")
	}
	port := in.Port
	if port == 0 {
		port = 9042
	}
	keyspace := strings.TrimSpace(in.Database)
	username := in.Username
	password := in.Password
	ssl := in.SSL
	if strings.HasPrefix(host, "cassandra://") || strings.HasPrefix(host, "cql://") {
		u, err := url.Parse(host)
		if err != nil {
			return nil, 0, "", "", "", false, fmt.Errorf("invalid Cassandra URI: %w", err)
		}
		host = u.Hostname()
		if u.Port() != "" {
			if parsed, err := strconv.Atoi(u.Port()); err == nil {
				port = parsed
			}
		}
		if u.User != nil {
			username = u.User.Username()
			password, _ = u.User.Password()
		}
		if strings.Trim(u.Path, "/") != "" {
			keyspace = strings.Trim(u.Path, "/")
		}
		ssl = ssl || u.Query().Get("ssl") == "true" || u.Query().Get("tls") == "true"
	}
	hosts := []string{}
	for _, h := range strings.Split(host, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return nil, 0, "", "", "", false, fmt.Errorf("at least one Cassandra contact point is required")
	}
	return hosts, port, keyspace, username, password, ssl, nil
}

func cassandraColumns(ctx context.Context, session *gocql.Session, keyspace string, table string) ([]CassandraColumnSummary, error) {
	query := `SELECT keyspace_name, table_name, column_name, type, kind, position FROM system_schema.columns WHERE keyspace_name = ?`
	args := []interface{}{keyspace}
	if table != "" {
		query += ` AND table_name = ?`
		args = append(args, table)
	}
	iter := session.Query(query, args...).WithContext(ctx).Iter()
	result := []CassandraColumnSummary{}
	row := map[string]interface{}{}
	for iter.MapScan(row) {
		result = append(result, CassandraColumnSummary{
			KeyspaceName: fmt.Sprint(row["keyspace_name"]),
			TableName:    fmt.Sprint(row["table_name"]),
			Name:         fmt.Sprint(row["column_name"]),
			Type:         fmt.Sprint(row["type"]),
			Kind:         fmt.Sprint(row["kind"]),
			Position:     intValue(row["position"]),
		})
		row = map[string]interface{}{}
	}
	return result, iter.Close()
}

func runCassandraQuery(ctx context.Context, session *gocql.Session, cql string, limit int) (CassandraResult, error) {
	start := time.Now()
	result := CassandraResult{Columns: []string{}, Rows: []map[string]interface{}{}}
	iter := session.Query(cql).WithContext(ctx).PageSize(limit).Iter()
	cols := iter.Columns()
	for _, col := range cols {
		result.Columns = append(result.Columns, col.Name)
	}
	row := map[string]interface{}{}
	for iter.MapScan(row) {
		clean := cassandraJSONMap(row)
		result.Rows = append(result.Rows, clean)
		row = map[string]interface{}{}
		if len(result.Rows) >= limit {
			break
		}
	}
	if err := iter.Close(); err != nil {
		return result, err
	}
	result.RowCount = len(result.Rows)
	result.Applied = len(result.Columns) > 0 || strings.HasPrefix(strings.ToLower(strings.TrimSpace(cql)), "select")
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

func countCassandraRows(ctx context.Context, session *gocql.Session, cql string) int {
	count := 0
	iter := session.Query(cql).WithContext(ctx).Iter()
	row := map[string]interface{}{}
	for iter.MapScan(row) {
		count++
		row = map[string]interface{}{}
	}
	_ = iter.Close()
	return count
}

func quoteCQLIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func clampLimit(raw string, fallback int) int {
	if parsed, err := strconv.Atoi(raw); err == nil {
		return normalizeLimit(parsed)
	}
	return normalizeLimit(fallback)
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func cassandraJSONMap(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = cassandraJSONValue(v)
	}
	return out
}

func cassandraJSONValue(v interface{}) interface{} {
	switch t := v.(type) {
	case nil, bool, string, int, int8, int16, int32, int64, float32, float64:
		return t
	case []byte:
		return "0x" + hex.EncodeToString(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	case map[string]string:
		return t
	case map[string]interface{}:
		return cassandraJSONMap(t)
	default:
		return fmt.Sprint(t)
	}
}

func boolValue(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fmt.Sprint(v) == "true"
}

func intValue(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	default:
		n, _ := strconv.Atoi(fmt.Sprint(v))
		return n
	}
}
