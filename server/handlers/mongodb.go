package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type MongoDashboardData struct {
	Driver         string                 `json:"driver"`
	Database       string                 `json:"database"`
	Version        string                 `json:"version"`
	SizeBytes      int64                  `json:"size_bytes"`
	Collections    int                    `json:"collections"`
	Objects        int64                  `json:"objects"`
	Indexes        int64                  `json:"indexes"`
	StorageSize    int64                  `json:"storage_size"`
	DataSize       int64                  `json:"data_size"`
	Server         map[string]interface{} `json:"server"`
	CollectionsTop []MongoCollectionStat  `json:"collections_top"`
}

type MongoDatabaseSummary struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Empty     bool   `json:"empty"`
}

type MongoCollectionStat struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Count       int64  `json:"count"`
	SizeBytes   int64  `json:"size_bytes"`
	StorageSize int64  `json:"storage_size"`
	Indexes     int64  `json:"indexes"`
	IndexSize   int64  `json:"index_size"`
}

type MongoDocumentList struct {
	Documents []json.RawMessage `json:"documents"`
	Limit     int64             `json:"limit"`
	Page      int64             `json:"page"`
	Skip      int64             `json:"skip"`
	Count     int64             `json:"count"`
	HasNext   bool              `json:"has_next"`
}

type MongoIndexSummary struct {
	Name string          `json:"name"`
	Keys json.RawMessage `json:"keys"`
	Spec json.RawMessage `json:"spec"`
}

type MongoSchemaField struct {
	Path       string   `json:"path"`
	Count      int64    `json:"count"`
	Occurrence float64  `json:"occurrence"`
	Types      []string `json:"types"`
	Examples   []string `json:"examples"`
}

type MongoSchemaAnalysis struct {
	SampleSize int64              `json:"sample_size"`
	Fields     []MongoSchemaField `json:"fields"`
}

type MongoHealthData struct {
	Status        string                   `json:"status"`
	Version       string                   `json:"version"`
	StorageEngine string                   `json:"storage_engine"`
	Server        map[string]interface{}   `json:"server"`
	ReplicaSet    map[string]interface{}   `json:"replica_set"`
	Shards        []map[string]interface{} `json:"shards"`
	CurrentOps    []json.RawMessage        `json:"current_ops"`
}

type MongoQueryEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ConnID      *int64 `json:"conn_id"`
	Payload     string `json:"payload"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type MongoIndexRecommendation struct {
	Field     string  `json:"field"`
	Direction int     `json:"direction"`
	Reason    string  `json:"reason"`
	Score     float64 `json:"score"`
}

type mongoSchemaAccumulator struct {
	Path     string
	Count    int64
	Types    map[string]bool
	Examples []string
}

type mongoDocumentRequest struct {
	Database   string          `json:"database"`
	Collection string          `json:"collection"`
	Document   json.RawMessage `json:"document"`
	Filter     json.RawMessage `json:"filter"`
	Update     json.RawMessage `json:"update"`
	Mode       string          `json:"mode"`
	Limit      int64           `json:"limit"`
	Preview    bool            `json:"preview"`
}

type mongoIndexRequest struct {
	Database   string          `json:"database"`
	Collection string          `json:"collection"`
	Name       string          `json:"name"`
	Keys       json.RawMessage `json:"keys"`
	Unique     bool            `json:"unique"`
}

type mongoAggregateRequest struct {
	Database   string          `json:"database"`
	Collection string          `json:"collection"`
	Pipeline   json.RawMessage `json:"pipeline"`
	Limit      int64           `json:"limit"`
}

type mongoExplainRequest struct {
	Database   string          `json:"database"`
	Collection string          `json:"collection"`
	Filter     json.RawMessage `json:"filter"`
}

type mongoCollectionRequest struct {
	Database   string `json:"database"`
	Collection string `json:"collection"`
	NewName    string `json:"new_name"`
}

type mongoImportRequest struct {
	Database   string          `json:"database"`
	Collection string          `json:"collection"`
	Documents  json.RawMessage `json:"documents"`
}

type mongoSavedQueryRequest struct {
	Name        string          `json:"name"`
	Database    string          `json:"database"`
	Collection  string          `json:"collection"`
	Filter      json.RawMessage `json:"filter"`
	Sort        json.RawMessage `json:"sort"`
	Projection  json.RawMessage `json:"projection"`
	Pipeline    json.RawMessage `json:"pipeline"`
	Description string          `json:"description"`
}

func testMongoInput(ctx context.Context, in ConnectionInput) error {
	client, err := newMongoClient(ctx, in)
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	return client.Ping(ctx, readpref.Primary())
}

func MongoPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, _, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		start := time.Now()
		if err := client.Ping(r.Context(), readpref.Primary()); err != nil {
			http.Error(w, jsonError("MongoDB ping failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "latency_ms": time.Since(start).Milliseconds()})
	}
}

func MongoDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())

		dbName := mongoDatabaseName(in)
		data := MongoDashboardData{Driver: "mongodb", Database: dbName, Server: map[string]interface{}{}, CollectionsTop: []MongoCollectionStat{}}
		db := client.Database(dbName)

		var buildInfo bson.M
		if err := db.RunCommand(r.Context(), bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo); err == nil {
			data.Version, _ = buildInfo["version"].(string)
		}
		var serverStatus bson.M
		if err := db.RunCommand(r.Context(), bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err == nil {
			data.Server = compactMongoServerStatus(serverStatus)
		}
		var dbStats bson.M
		if err := db.RunCommand(r.Context(), bson.D{{Key: "dbStats", Value: 1}, {Key: "scale", Value: 1}}).Decode(&dbStats); err == nil {
			data.SizeBytes = mongoInt64(dbStats["totalSize"])
			data.StorageSize = mongoInt64(dbStats["storageSize"])
			data.DataSize = mongoInt64(dbStats["dataSize"])
			data.Collections = int(mongoInt64(dbStats["collections"]))
			data.Objects = mongoInt64(dbStats["objects"])
			data.Indexes = mongoInt64(dbStats["indexes"])
		}
		data.CollectionsTop = loadMongoCollectionStats(r.Context(), db, 20)
		json.NewEncoder(w).Encode(data)
	}
}

func MongoDatabases() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, _, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		result, err := client.ListDatabases(r.Context(), bson.D{})
		if err != nil {
			http.Error(w, jsonError("list MongoDB databases failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		items := make([]MongoDatabaseSummary, 0, len(result.Databases))
		for _, d := range result.Databases {
			items = append(items, MongoDatabaseSummary{Name: d.Name, SizeBytes: int64(d.SizeOnDisk), Empty: d.Empty})
		}
		json.NewEncoder(w).Encode(items)
	}
}

func MongoHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())

		db := client.Database(mongoDatabaseName(in))
		data := MongoHealthData{Status: "ok", Server: map[string]interface{}{}, ReplicaSet: map[string]interface{}{}, Shards: []map[string]interface{}{}, CurrentOps: []json.RawMessage{}}
		var buildInfo bson.M
		if err := db.RunCommand(r.Context(), bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo); err == nil {
			data.Version, _ = buildInfo["version"].(string)
		}
		var serverStatus bson.M
		if err := db.RunCommand(r.Context(), bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err == nil {
			data.Server = compactMongoServerStatus(serverStatus)
			if storage, ok := serverStatus["storageEngine"].(bson.M); ok {
				data.StorageEngine, _ = storage["name"].(string)
			}
		}
		var repl bson.M
		if err := client.Database("admin").RunCommand(r.Context(), bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&repl); err == nil {
			data.ReplicaSet = compactMongoMap(repl)
		}
		var shards bson.M
		if err := client.Database("admin").RunCommand(r.Context(), bson.D{{Key: "listShards", Value: 1}}).Decode(&shards); err == nil {
			if rawShards, ok := shards["shards"].(bson.A); ok {
				for _, shard := range rawShards {
					if item, ok := shard.(bson.M); ok {
						data.Shards = append(data.Shards, compactMongoMap(item))
					}
				}
			}
		}
		var currentOps bson.M
		if err := client.Database("admin").RunCommand(r.Context(), bson.D{{Key: "currentOp", Value: 1}, {Key: "$all", Value: false}}).Decode(&currentOps); err == nil {
			if ops, ok := currentOps["inprog"].(bson.A); ok {
				for i, op := range ops {
					if i >= 20 {
						break
					}
					raw, _ := bson.MarshalExtJSON(op, true, true)
					data.CurrentOps = append(data.CurrentOps, json.RawMessage(raw))
				}
			}
		}
		json.NewEncoder(w).Encode(data)
	}
}

func MongoCollections() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			mongoListCollections(w, r)
		case http.MethodPost:
			mongoCreateCollection(w, r)
		case http.MethodPut:
			mongoRenameCollection(w, r)
		case http.MethodDelete:
			mongoDropCollection(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func mongoListCollections(w http.ResponseWriter, r *http.Request) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	defer client.Disconnect(context.Background())
	dbName := strings.TrimSpace(r.URL.Query().Get("database"))
	if dbName == "" {
		dbName = mongoDatabaseName(in)
	}
	items := loadMongoCollectionStats(r.Context(), client.Database(dbName), 200)
	json.NewEncoder(w).Encode(items)
}

func mongoCreateCollection(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoCollectionRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	if err := client.Database(defaultMongoDatabase(req.Database, in)).CreateCollection(r.Context(), req.Collection); err != nil {
		writeMongoAudit(r, "mongo_create_collection", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("create MongoDB collection failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_create_collection", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
	json.NewEncoder(w).Encode(map[string]string{"collection": req.Collection})
}

func mongoDropCollection(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoCollectionRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	if err := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection).Drop(r.Context()); err != nil {
		writeMongoAudit(r, "mongo_drop_collection", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("drop MongoDB collection failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_drop_collection", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
	json.NewEncoder(w).Encode(map[string]string{"message": "collection dropped"})
}

func mongoRenameCollection(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoCollectionRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	newName := strings.TrimSpace(req.NewName)
	if newName == "" {
		http.Error(w, jsonError("new collection name is required"), http.StatusBadRequest)
		return
	}
	dbName := defaultMongoDatabase(req.Database, in)
	cmd := bson.D{{Key: "renameCollection", Value: dbName + "." + req.Collection}, {Key: "to", Value: dbName + "." + newName}}
	if err := client.Database("admin").RunCommand(r.Context(), cmd).Err(); err != nil {
		writeMongoAudit(r, "mongo_rename_collection", in, dbName+"."+req.Collection+" -> "+newName, err.Error())
		http.Error(w, jsonError("rename MongoDB collection failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_rename_collection", in, dbName+"."+req.Collection+" -> "+newName, "")
	json.NewEncoder(w).Encode(map[string]string{"collection": newName})
}

func MongoImport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		var req mongoImportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		dbName := strings.TrimSpace(req.Database)
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		collection := strings.TrimSpace(req.Collection)
		if collection == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		documents, err := parseMongoDocumentArray(req.Documents)
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB import JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		if len(documents) == 0 {
			http.Error(w, jsonError("documents array is empty"), http.StatusBadRequest)
			return
		}
		result, err := client.Database(dbName).Collection(collection).InsertMany(r.Context(), documents)
		if err != nil {
			writeMongoAudit(r, "mongo_import", in, dbName+"."+collection, err.Error())
			http.Error(w, jsonError("import MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeMongoAudit(r, "mongo_import", in, dbName+"."+collection, "")
		json.NewEncoder(w).Encode(map[string]interface{}{"inserted": len(result.InsertedIDs)})
	}
}

func MongoExport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		dbName := strings.TrimSpace(r.URL.Query().Get("database"))
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		collection := strings.TrimSpace(r.URL.Query().Get("collection"))
		if collection == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		limit := int64(queryInt(r, "limit", 1000, 1, 5000))
		format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
		if format == "" {
			format = "json"
		}
		filter, err := parseMongoDocumentString(r.URL.Query().Get("filter"))
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		cursor, err := client.Database(dbName).Collection(collection).Find(r.Context(), filter, options.Find().SetLimit(limit))
		if err != nil {
			http.Error(w, jsonError("export MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer cursor.Close(r.Context())
		out, err := mongoCursorDocuments(r.Context(), cursor, limit)
		if err != nil {
			http.Error(w, jsonError("read MongoDB export failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		switch format {
		case "ndjson":
			w.Header().Set("Content-Type", "application/x-ndjson")
			for _, doc := range out.Documents {
				w.Write(doc)
				w.Write([]byte("\n"))
			}
		case "csv":
			w.Header().Set("Content-Type", "text/csv")
			csvBytes, err := mongoDocumentsCSV(out.Documents)
			if err != nil {
				http.Error(w, jsonError("encode MongoDB CSV failed: "+err.Error()), http.StatusInternalServerError)
				return
			}
			w.Write(csvBytes)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("["))
			for i, doc := range out.Documents {
				if i > 0 {
					w.Write([]byte(","))
				}
				w.Write(doc)
			}
			w.Write([]byte("]"))
		}
		writeMongoAudit(r, "mongo_export_"+format, in, dbName+"."+collection, "")
	}
}

func MongoDocuments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			mongoListDocuments(w, r)
		case http.MethodPost:
			mongoInsertDocument(w, r)
		case http.MethodPut:
			mongoReplaceDocument(w, r)
		case http.MethodDelete:
			mongoDeleteDocument(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func MongoIndexes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			mongoListIndexes(w, r)
		case http.MethodPost:
			mongoCreateIndex(w, r)
		case http.MethodDelete:
			mongoDropIndex(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func MongoAggregate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())

		var req mongoAggregateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		dbName := strings.TrimSpace(req.Database)
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		if strings.TrimSpace(req.Collection) == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		pipeline, err := parseMongoPipeline(req.Pipeline)
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB pipeline JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		cursor, err := client.Database(dbName).Collection(req.Collection).Aggregate(r.Context(), pipeline)
		if err != nil {
			http.Error(w, jsonError("run MongoDB aggregation failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer cursor.Close(r.Context())
		out, err := mongoCursorDocuments(r.Context(), cursor, boundedMongoLimit(req.Limit))
		if err != nil {
			http.Error(w, jsonError("read MongoDB aggregation result failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(out)
	}
}

func MongoExplain() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())

		var req mongoExplainRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		dbName := strings.TrimSpace(req.Database)
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		if strings.TrimSpace(req.Collection) == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		filter, err := parseMongoDocument(req.Filter)
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		cmd := bson.D{
			{Key: "explain", Value: bson.D{
				{Key: "find", Value: req.Collection},
				{Key: "filter", Value: filter},
			}},
			{Key: "verbosity", Value: "executionStats"},
		}
		var result bson.M
		if err := client.Database(dbName).RunCommand(r.Context(), cmd).Decode(&result); err != nil {
			http.Error(w, jsonError("MongoDB explain failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		raw, err := bson.MarshalExtJSON(result, true, true)
		if err != nil {
			http.Error(w, jsonError("encode MongoDB explain failed: "+err.Error()), http.StatusInternalServerError)
			return
		}
		w.Write(raw)
	}
}

func MongoSchema() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		dbName := strings.TrimSpace(r.URL.Query().Get("database"))
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		collection := strings.TrimSpace(r.URL.Query().Get("collection"))
		if collection == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		limit := int64(queryInt(r, "limit", 100, 1, 1000))
		filter, err := parseMongoDocumentString(r.URL.Query().Get("filter"))
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		cursor, err := client.Database(dbName).Collection(collection).Find(r.Context(), filter, options.Find().SetLimit(limit))
		if err != nil {
			http.Error(w, jsonError("sample MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer cursor.Close(r.Context())
		analysis, err := analyzeMongoSchema(r.Context(), cursor)
		if err != nil {
			http.Error(w, jsonError("analyze MongoDB schema failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(analysis)
	}
}

func MongoIndexRecommendations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, in, err := openMongoClientFromRequest(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer client.Disconnect(context.Background())
		dbName := strings.TrimSpace(r.URL.Query().Get("database"))
		if dbName == "" {
			dbName = mongoDatabaseName(in)
		}
		collection := strings.TrimSpace(r.URL.Query().Get("collection"))
		if collection == "" {
			http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
			return
		}
		filter, err := parseMongoDocumentString(r.URL.Query().Get("filter"))
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		sortDoc, err := parseMongoDocumentString(r.URL.Query().Get("sort"))
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB sort JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		existing, _ := mongoExistingIndexFields(r.Context(), client.Database(dbName).Collection(collection))
		recs := mongoRecommendIndexes(filter, sortDoc, existing)
		json.NewEncoder(w).Encode(recs)
	}
}

func MongoSavedQueries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			mongoListSavedQueries(w, r)
		case http.MethodPost:
			mongoCreateSavedQuery(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func MongoSavedQueryItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			mongoDeleteSavedQuery(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func mongoListDocuments(w http.ResponseWriter, r *http.Request) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	defer client.Disconnect(context.Background())
	dbName := strings.TrimSpace(r.URL.Query().Get("database"))
	if dbName == "" {
		dbName = mongoDatabaseName(in)
	}
	collection := strings.TrimSpace(r.URL.Query().Get("collection"))
	if collection == "" {
		http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
		return
	}
	limit := int64(queryInt(r, "limit", 50, 1, 200))
	page := int64(queryInt(r, "page", 1, 1, 1000000))
	skip := int64(queryInt(r, "skip", int((page-1)*limit), 0, 100000000))
	filter, err := parseMongoDocumentString(r.URL.Query().Get("filter"))
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	findOpts := options.Find().SetLimit(limit + 1).SetSkip(skip)
	if sort, err := parseMongoDocumentString(r.URL.Query().Get("sort")); err != nil {
		http.Error(w, jsonError("invalid MongoDB sort JSON: "+err.Error()), http.StatusBadRequest)
		return
	} else if len(sort) > 0 {
		findOpts.SetSort(sort)
	}
	if projection, err := parseMongoDocumentString(r.URL.Query().Get("projection")); err != nil {
		http.Error(w, jsonError("invalid MongoDB projection JSON: "+err.Error()), http.StatusBadRequest)
		return
	} else if len(projection) > 0 {
		findOpts.SetProjection(projection)
	}
	coll := client.Database(dbName).Collection(collection)
	count, _ := coll.CountDocuments(r.Context(), filter)
	cursor, err := coll.Find(r.Context(), filter, findOpts)
	if err != nil {
		http.Error(w, jsonError("find MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	defer cursor.Close(r.Context())
	out, err := mongoCursorDocuments(r.Context(), cursor, limit+1)
	if err != nil {
		http.Error(w, jsonError("read MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	out.Page = page
	out.Skip = skip
	out.Count = count
	out.HasNext = int64(len(out.Documents)) > limit
	if out.HasNext {
		out.Documents = out.Documents[:limit]
	}
	out.Limit = limit
	json.NewEncoder(w).Encode(out)
}

func mongoInsertDocument(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoDocumentRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	doc, err := parseMongoDocument(req.Document)
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB document JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	result, err := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection).InsertOne(r.Context(), doc)
	if err != nil {
		writeMongoAudit(r, "mongo_insert_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("insert MongoDB document failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_insert_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
	json.NewEncoder(w).Encode(map[string]interface{}{"inserted_id": result.InsertedID})
}

func mongoReplaceDocument(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoDocumentRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	filter, err := parseMongoDocument(req.Filter)
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	coll := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection)
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "replace"
	}
	if req.Preview {
		limit := boundedMongoLimit(req.Limit)
		cursor, err := coll.Find(r.Context(), filter, options.Find().SetLimit(limit))
		if err != nil {
			http.Error(w, jsonError("preview MongoDB update failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer cursor.Close(r.Context())
		out, err := mongoCursorDocuments(r.Context(), cursor, limit)
		if err != nil {
			http.Error(w, jsonError("read MongoDB update preview failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		count, _ := coll.CountDocuments(r.Context(), filter)
		out.Count = count
		json.NewEncoder(w).Encode(out)
		return
	}
	var matched, modified int64
	if mode == "operators" || mode == "updateOne" || mode == "updateMany" {
		update, err := parseMongoDocument(req.Update)
		if err != nil {
			http.Error(w, jsonError("invalid MongoDB update JSON: "+err.Error()), http.StatusBadRequest)
			return
		}
		if !mongoUpdateUsesOperators(update) {
			http.Error(w, jsonError("update operator mode requires keys like $set, $unset, or $inc"), http.StatusBadRequest)
			return
		}
		if mode == "updateMany" {
			result, err := coll.UpdateMany(r.Context(), filter, update)
			if err == nil {
				matched, modified = result.MatchedCount, result.ModifiedCount
			}
			if err != nil {
				writeMongoAudit(r, "mongo_update_many", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
				http.Error(w, jsonError("update MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
				return
			}
		} else {
			result, err := coll.UpdateOne(r.Context(), filter, update)
			if err == nil {
				matched, modified = result.MatchedCount, result.ModifiedCount
			}
			if err != nil {
				writeMongoAudit(r, "mongo_update_one", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
				http.Error(w, jsonError("update MongoDB document failed: "+err.Error()), http.StatusBadGateway)
				return
			}
		}
		action := "mongo_update_one"
		if mode == "updateMany" {
			action = "mongo_update_many"
		}
		writeMongoAudit(r, action, in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
		json.NewEncoder(w).Encode(map[string]interface{}{"matched": matched, "modified": modified})
		return
	}
	doc, err := parseMongoDocument(req.Document)
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB document JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	result, err := coll.ReplaceOne(r.Context(), filter, doc)
	if err != nil {
		writeMongoAudit(r, "mongo_replace_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("update MongoDB document failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_replace_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
	json.NewEncoder(w).Encode(map[string]interface{}{"matched": result.MatchedCount, "modified": result.ModifiedCount})
}

func mongoDeleteDocument(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoDocumentRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	filter, err := parseMongoDocument(req.Filter)
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB filter JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	coll := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection)
	if req.Preview {
		limit := boundedMongoLimit(req.Limit)
		cursor, err := coll.Find(r.Context(), filter, options.Find().SetLimit(limit))
		if err != nil {
			http.Error(w, jsonError("preview MongoDB delete failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer cursor.Close(r.Context())
		out, err := mongoCursorDocuments(r.Context(), cursor, limit)
		if err != nil {
			http.Error(w, jsonError("read MongoDB delete preview failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		count, _ := coll.CountDocuments(r.Context(), filter)
		out.Count = count
		json.NewEncoder(w).Encode(out)
		return
	}
	if strings.TrimSpace(req.Mode) == "deleteMany" {
		result, err := coll.DeleteMany(r.Context(), filter)
		if err != nil {
			writeMongoAudit(r, "mongo_delete_many", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
			http.Error(w, jsonError("delete MongoDB documents failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeMongoAudit(r, "mongo_delete_many", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": result.DeletedCount})
		return
	}
	result, err := coll.DeleteOne(r.Context(), filter)
	if err != nil {
		writeMongoAudit(r, "mongo_delete_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("delete MongoDB document failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_delete_document", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, "")
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": result.DeletedCount})
}

func mongoListIndexes(w http.ResponseWriter, r *http.Request) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	defer client.Disconnect(context.Background())
	dbName := strings.TrimSpace(r.URL.Query().Get("database"))
	if dbName == "" {
		dbName = mongoDatabaseName(in)
	}
	collection := strings.TrimSpace(r.URL.Query().Get("collection"))
	if collection == "" {
		http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
		return
	}
	cursor, err := client.Database(dbName).Collection(collection).Indexes().List(r.Context())
	if err != nil {
		http.Error(w, jsonError("list MongoDB indexes failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	defer cursor.Close(r.Context())
	var specs []bson.M
	if err := cursor.All(r.Context(), &specs); err != nil {
		http.Error(w, jsonError("read MongoDB indexes failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	indexes := make([]MongoIndexSummary, 0, len(specs))
	for _, spec := range specs {
		name, _ := spec["name"].(string)
		keys, _ := bson.MarshalExtJSON(spec["key"], true, true)
		raw, _ := bson.MarshalExtJSON(spec, true, true)
		indexes = append(indexes, MongoIndexSummary{Name: name, Keys: keys, Spec: raw})
	}
	json.NewEncoder(w).Encode(indexes)
}

func mongoCreateIndex(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoIndexRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	keys, err := parseMongoDocument(req.Keys)
	if err != nil {
		http.Error(w, jsonError("invalid MongoDB index keys JSON: "+err.Error()), http.StatusBadRequest)
		return
	}
	opts := options.Index()
	if strings.TrimSpace(req.Name) != "" {
		opts.SetName(strings.TrimSpace(req.Name))
	}
	if req.Unique {
		opts.SetUnique(true)
	}
	name, err := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection).Indexes().CreateOne(r.Context(), mongo.IndexModel{Keys: keys, Options: opts})
	if err != nil {
		writeMongoAudit(r, "mongo_create_index", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection, err.Error())
		http.Error(w, jsonError("create MongoDB index failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_create_index", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection+"."+name, "")
	json.NewEncoder(w).Encode(map[string]string{"name": name})
}

func mongoDropIndex(w http.ResponseWriter, r *http.Request) {
	client, in, req, ok := mongoIndexRequestFromHTTP(w, r)
	if !ok {
		return
	}
	defer client.Disconnect(context.Background())
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, jsonError("index name is required"), http.StatusBadRequest)
		return
	}
	if name == "_id_" {
		http.Error(w, jsonError("_id index cannot be dropped"), http.StatusBadRequest)
		return
	}
	if err := client.Database(defaultMongoDatabase(req.Database, in)).Collection(req.Collection).Indexes().DropOne(r.Context(), name); err != nil {
		writeMongoAudit(r, "mongo_drop_index", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection+"."+name, err.Error())
		http.Error(w, jsonError("drop MongoDB index failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeMongoAudit(r, "mongo_drop_index", in, defaultMongoDatabase(req.Database, in)+"."+req.Collection+"."+name, "")
	json.NewEncoder(w).Encode(map[string]string{"message": "index dropped"})
}

func mongoDocumentRequestFromHTTP(w http.ResponseWriter, r *http.Request) (*mongo.Client, ConnectionInput, mongoDocumentRequest, bool) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return nil, ConnectionInput{}, mongoDocumentRequest{}, false
	}
	var req mongoDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoDocumentRequest{}, false
	}
	req.Collection = strings.TrimSpace(req.Collection)
	if req.Collection == "" {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoDocumentRequest{}, false
	}
	return client, in, req, true
}

func mongoIndexRequestFromHTTP(w http.ResponseWriter, r *http.Request) (*mongo.Client, ConnectionInput, mongoIndexRequest, bool) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return nil, ConnectionInput{}, mongoIndexRequest{}, false
	}
	var req mongoIndexRequest
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		req.Database = r.URL.Query().Get("database")
		req.Collection = r.URL.Query().Get("collection")
		req.Name = r.URL.Query().Get("name")
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoIndexRequest{}, false
	}
	req.Collection = strings.TrimSpace(req.Collection)
	if req.Collection == "" {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoIndexRequest{}, false
	}
	return client, in, req, true
}

func mongoCollectionRequestFromHTTP(w http.ResponseWriter, r *http.Request) (*mongo.Client, ConnectionInput, mongoCollectionRequest, bool) {
	client, in, err := openMongoClientFromRequest(r)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return nil, ConnectionInput{}, mongoCollectionRequest{}, false
	}
	var req mongoCollectionRequest
	if r.Method == http.MethodDelete {
		req.Database = r.URL.Query().Get("database")
		req.Collection = r.URL.Query().Get("collection")
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoCollectionRequest{}, false
	}
	req.Collection = strings.TrimSpace(req.Collection)
	if req.Collection == "" {
		_ = client.Disconnect(context.Background())
		http.Error(w, jsonError("collection is required"), http.StatusBadRequest)
		return nil, ConnectionInput{}, mongoCollectionRequest{}, false
	}
	return client, in, req, true
}

func defaultMongoDatabase(raw string, in ConnectionInput) string {
	dbName := strings.TrimSpace(raw)
	if dbName == "" {
		return mongoDatabaseName(in)
	}
	return dbName
}

func boundedMongoLimit(limit int64) int64 {
	if limit < 1 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func parseMongoDocumentString(raw string) (bson.D, error) {
	return parseMongoDocument(json.RawMessage(strings.TrimSpace(raw)))
}

func parseMongoDocument(raw json.RawMessage) (bson.D, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return bson.D{}, nil
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), true, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func mongoUpdateUsesOperators(doc bson.D) bool {
	if len(doc) == 0 {
		return false
	}
	for _, elem := range doc {
		if !strings.HasPrefix(elem.Key, "$") {
			return false
		}
	}
	return true
}

func parseMongoPipeline(raw json.RawMessage) (bson.A, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return bson.A{}, nil
	}
	var wrapper bson.M
	if err := bson.UnmarshalExtJSON([]byte(`{"pipeline":`+text+`}`), true, &wrapper); err != nil {
		return nil, err
	}
	pipeline, ok := wrapper["pipeline"].(bson.A)
	if !ok {
		return nil, fmt.Errorf("pipeline must be a JSON array")
	}
	return pipeline, nil
}

func parseMongoDocumentArray(raw json.RawMessage) ([]interface{}, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, fmt.Errorf("documents JSON is required")
	}
	var wrapper bson.M
	if err := bson.UnmarshalExtJSON([]byte(`{"documents":`+text+`}`), true, &wrapper); err != nil {
		return nil, err
	}
	values, ok := wrapper["documents"].(bson.A)
	if !ok {
		return nil, fmt.Errorf("documents must be a JSON array")
	}
	docs := make([]interface{}, 0, len(values))
	for _, value := range values {
		docs = append(docs, value)
	}
	return docs, nil
}

func mongoCursorDocuments(ctx context.Context, cursor *mongo.Cursor, limit int64) (MongoDocumentList, error) {
	out := MongoDocumentList{Documents: []json.RawMessage{}, Limit: limit}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return out, err
		}
		raw, err := bson.MarshalExtJSON(doc, true, true)
		if err != nil {
			return out, err
		}
		out.Documents = append(out.Documents, json.RawMessage(raw))
		if limit > 0 && int64(len(out.Documents)) >= limit {
			break
		}
	}
	if err := cursor.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func mongoDocumentsCSV(docs []json.RawMessage) ([]byte, error) {
	rows := make([]map[string]string, 0, len(docs))
	columnsSet := map[string]bool{}
	for _, raw := range docs {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		flat := map[string]string{}
		flattenMongoJSON("", value, flat)
		for key := range flat {
			columnsSet[key] = true
		}
		rows = append(rows, flat)
	}
	columns := make([]string, 0, len(columnsSet))
	for key := range columnsSet {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(columns); err != nil {
		return nil, err
	}
	for _, row := range rows {
		record := make([]string, len(columns))
		for i, col := range columns {
			record[i] = row[col]
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}

func flattenMongoJSON(prefix string, value any, out map[string]string) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 1 {
			if raw, ok := v["$oid"]; ok {
				out[prefix] = fmt.Sprint(raw)
				return
			}
			if raw, ok := v["$date"]; ok {
				out[prefix] = fmt.Sprint(raw)
				return
			}
			if raw, ok := v["$numberLong"]; ok {
				out[prefix] = fmt.Sprint(raw)
				return
			}
		}
		for key, nested := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flattenMongoJSON(path, nested, out)
		}
	case []any:
		raw, _ := json.Marshal(v)
		out[prefix] = string(raw)
	default:
		if prefix != "" {
			out[prefix] = fmt.Sprint(v)
		}
	}
}

func mongoExistingIndexFields(ctx context.Context, coll *mongo.Collection) (map[string]bool, error) {
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return map[string]bool{}, err
	}
	defer cursor.Close(ctx)
	existing := map[string]bool{}
	var specs []bson.M
	if err := cursor.All(ctx, &specs); err != nil {
		return existing, err
	}
	for _, spec := range specs {
		switch keys := spec["key"].(type) {
		case bson.M:
			for key := range keys {
				existing[key] = true
			}
		case bson.D:
			for _, elem := range keys {
				existing[elem.Key] = true
			}
		}
	}
	return existing, nil
}

func mongoRecommendIndexes(filter, sortDoc bson.D, existing map[string]bool) []MongoIndexRecommendation {
	scores := map[string]MongoIndexRecommendation{}
	add := func(field string, direction int, reason string, score float64) {
		if field == "" || strings.HasPrefix(field, "$") || existing[field] {
			return
		}
		current := scores[field]
		if current.Field == "" || score > current.Score {
			scores[field] = MongoIndexRecommendation{Field: field, Direction: direction, Reason: reason, Score: score}
		}
	}
	for _, elem := range filter {
		score := 0.7
		reason := "Used in filter"
		if nested, ok := elem.Value.(bson.D); ok {
			for _, op := range nested {
				if op.Key == "$eq" {
					score = 0.95
					reason = "Equality filter"
				} else if op.Key == "$gt" || op.Key == "$gte" || op.Key == "$lt" || op.Key == "$lte" {
					score = 0.85
					reason = "Range filter"
				}
			}
		}
		add(elem.Key, 1, reason, score)
	}
	for _, elem := range sortDoc {
		direction := int(mongoInt64(elem.Value))
		if direction == 0 {
			direction = 1
		}
		add(elem.Key, direction, "Used for sorting", 0.8)
	}
	out := make([]MongoIndexRecommendation, 0, len(scores))
	for _, rec := range scores {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func mongoListSavedQueries(w http.ResponseWriter, r *http.Request) {
	connID, err := connectionIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
		return
	}
	rows, err := appdb.DB.Query(appdb.ConvertQuery(
		`SELECT id, name, conn_id, sql, COALESCE(description,''), created_at, updated_at
		 FROM saved_queries WHERE conn_id=? AND description LIKE 'mongo:%' ORDER BY updated_at DESC`), connID)
	if err != nil {
		http.Error(w, jsonError("failed to list MongoDB saved queries"), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := []MongoQueryEntry{}
	for rows.Next() {
		var item MongoQueryEntry
		if err := rows.Scan(&item.ID, &item.Name, &item.ConnID, &item.Payload, &item.Description, &item.CreatedAt, &item.UpdatedAt); err != nil {
			http.Error(w, jsonError("failed to read MongoDB saved queries"), http.StatusInternalServerError)
			return
		}
		item.Description = strings.TrimPrefix(item.Description, "mongo:")
		items = append(items, item)
	}
	json.NewEncoder(w).Encode(items)
}

func mongoCreateSavedQuery(w http.ResponseWriter, r *http.Request) {
	connID, err := connectionIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
		return
	}
	var req mongoSavedQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, jsonError("name is required"), http.StatusBadRequest)
		return
	}
	payload, err := json.Marshal(req)
	if err != nil {
		http.Error(w, jsonError("encode MongoDB query failed"), http.StatusInternalServerError)
		return
	}
	var userID *int64
	if userIDStr := r.Header.Get("X-User-ID"); userIDStr != "" {
		if uid, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
			userID = &uid
		}
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := appdb.DB.Exec(appdb.ConvertQuery(`INSERT INTO saved_queries (name, conn_id, sql, description, user_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`),
		strings.TrimSpace(req.Name), connID, string(payload), "mongo:"+strings.TrimSpace(req.Description), userID, now, now)
	if err != nil {
		http.Error(w, jsonError("failed to save MongoDB query"), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func mongoDeleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
	if len(parts) < 4 {
		http.Error(w, jsonError("invalid saved query id"), http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		http.Error(w, jsonError("invalid saved query id"), http.StatusBadRequest)
		return
	}
	if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM saved_queries WHERE id=? AND description LIKE 'mongo:%'`), id); err != nil {
		http.Error(w, jsonError("failed to delete MongoDB query"), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func analyzeMongoSchema(ctx context.Context, cursor *mongo.Cursor) (MongoSchemaAnalysis, error) {
	fields := map[string]*mongoSchemaAccumulator{}
	var sampleSize int64
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return MongoSchemaAnalysis{}, err
		}
		sampleSize++
		seen := map[string]bool{}
		collectMongoSchemaFields("", doc, fields, seen)
		for path := range seen {
			fields[path].Count++
		}
	}
	if err := cursor.Err(); err != nil {
		return MongoSchemaAnalysis{}, err
	}
	out := MongoSchemaAnalysis{SampleSize: sampleSize, Fields: []MongoSchemaField{}}
	for _, field := range fields {
		types := make([]string, 0, len(field.Types))
		for t := range field.Types {
			types = append(types, t)
		}
		sortStrings(types)
		occurrence := float64(0)
		if sampleSize > 0 {
			occurrence = float64(field.Count) / float64(sampleSize) * 100
		}
		out.Fields = append(out.Fields, MongoSchemaField{
			Path:       field.Path,
			Count:      field.Count,
			Occurrence: occurrence,
			Types:      types,
			Examples:   field.Examples,
		})
	}
	for i := 0; i < len(out.Fields); i++ {
		for j := i + 1; j < len(out.Fields); j++ {
			if out.Fields[j].Path < out.Fields[i].Path {
				out.Fields[i], out.Fields[j] = out.Fields[j], out.Fields[i]
			}
		}
	}
	return out, nil
}

func collectMongoSchemaFields(prefix string, value interface{}, fields map[string]*mongoSchemaAccumulator, seen map[string]bool) {
	switch v := value.(type) {
	case bson.M:
		for key, nested := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			recordMongoSchemaField(path, nested, fields, seen)
			collectMongoSchemaFields(path, nested, fields, seen)
		}
	case bson.D:
		for _, elem := range v {
			path := elem.Key
			if prefix != "" {
				path = prefix + "." + elem.Key
			}
			recordMongoSchemaField(path, elem.Value, fields, seen)
			collectMongoSchemaFields(path, elem.Value, fields, seen)
		}
	case bson.A:
		arrayPath := prefix + "[]"
		recordMongoSchemaField(arrayPath, v, fields, seen)
		for _, item := range v {
			collectMongoSchemaFields(arrayPath, item, fields, seen)
		}
	case []interface{}:
		arrayPath := prefix + "[]"
		recordMongoSchemaField(arrayPath, v, fields, seen)
		for _, item := range v {
			collectMongoSchemaFields(arrayPath, item, fields, seen)
		}
	}
}

func recordMongoSchemaField(path string, value interface{}, fields map[string]*mongoSchemaAccumulator, seen map[string]bool) {
	if path == "" {
		return
	}
	field := fields[path]
	if field == nil {
		field = &mongoSchemaAccumulator{Path: path, Types: map[string]bool{}, Examples: []string{}}
		fields[path] = field
	}
	field.Types[mongoSchemaType(value)] = true
	seen[path] = true
	example := mongoSchemaExample(value)
	if example == "" {
		return
	}
	for _, existing := range field.Examples {
		if existing == example {
			return
		}
	}
	if len(field.Examples) < 3 {
		field.Examples = append(field.Examples, example)
	}
}

func mongoSchemaType(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int32, int64:
		return "integer"
	case float32, float64:
		return "number"
	case time.Time:
		return "date"
	case bson.ObjectID:
		return "objectId"
	case bson.M, bson.D:
		return "object"
	case bson.A, []interface{}:
		return "array"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func mongoSchemaExample(value interface{}) string {
	raw, err := bson.MarshalExtJSON(bson.M{"value": value}, true, true)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return string(raw)
	}
	example := strings.TrimSpace(string(wrapper["value"]))
	if len(example) > 140 {
		return example[:137] + "..."
	}
	return example
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func openMongoClientFromRequest(r *http.Request) (*mongo.Client, ConnectionInput, error) {
	connID, err := connectionIDFromPath(r.URL.Path)
	if err != nil {
		return nil, ConnectionInput{}, fmt.Errorf("invalid connection id")
	}
	in, err := mongoConnectionInput(connID)
	if err != nil {
		return nil, ConnectionInput{}, err
	}
	client, err := newMongoClient(r.Context(), in)
	return client, in, err
}

func mongoConnectionInput(connID int64) (ConnectionInput, error) {
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
	if in.Driver != "mongodb" {
		return in, fmt.Errorf("connection is not MongoDB")
	}
	password, err := decryptCredential(encPassword)
	if err != nil {
		return in, fmt.Errorf("decryption error")
	}
	in.Password = password
	in.SSL = ssl == 1
	return in, nil
}

func newMongoClient(ctx context.Context, in ConnectionInput) (*mongo.Client, error) {
	uri, err := buildMongoURI(in)
	if err != nil {
		return nil, err
	}
	opts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(8 * time.Second)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return client, nil
}

func buildMongoURI(in ConnectionInput) (string, error) {
	host := strings.TrimSpace(in.Host)
	if host == "" {
		return "", fmt.Errorf("MongoDB host or URI is required")
	}
	if strings.HasPrefix(host, "mongodb://") || strings.HasPrefix(host, "mongodb+srv://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("invalid MongoDB URI: %w", err)
		}
		if u.User == nil && in.Username != "" {
			u.User = url.UserPassword(in.Username, in.Password)
		}
		if u.Path == "" && strings.TrimSpace(in.Database) != "" {
			u.Path = "/" + url.PathEscape(strings.TrimSpace(in.Database))
		}
		q := u.Query()
		if in.SSL && q.Get("tls") == "" && q.Get("ssl") == "" {
			q.Set("tls", "true")
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	port := in.Port
	if port == 0 {
		port = 27017
	}
	u := url.URL{Scheme: "mongodb", Host: fmt.Sprintf("%s:%d", host, port)}
	if in.Username != "" {
		u.User = url.UserPassword(in.Username, in.Password)
	}
	if strings.TrimSpace(in.Database) != "" {
		u.Path = "/" + url.PathEscape(strings.TrimSpace(in.Database))
	}
	q := u.Query()
	if in.SSL {
		q.Set("tls", "true")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func mongoDatabaseName(in ConnectionInput) string {
	name := strings.Trim(strings.TrimSpace(in.Database), "/")
	if name == "" {
		return "admin"
	}
	return name
}

func loadMongoCollectionStats(ctx context.Context, db *mongo.Database, limit int) []MongoCollectionStat {
	specs, err := db.ListCollectionSpecifications(ctx, bson.D{})
	if err != nil {
		return []MongoCollectionStat{}
	}
	items := make([]MongoCollectionStat, 0, len(specs))
	for _, spec := range specs {
		item := MongoCollectionStat{Name: spec.Name, Type: spec.Type}
		var stats bson.M
		if err := db.RunCommand(ctx, bson.D{{Key: "collStats", Value: spec.Name}, {Key: "scale", Value: 1}}).Decode(&stats); err == nil {
			item.Count = mongoInt64(stats["count"])
			item.SizeBytes = mongoInt64(stats["size"])
			item.StorageSize = mongoInt64(stats["storageSize"])
			item.Indexes = mongoInt64(stats["nindexes"])
			item.IndexSize = mongoInt64(stats["totalIndexSize"])
		}
		items = append(items, item)
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].StorageSize > items[i].StorageSize {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func compactMongoServerStatus(status bson.M) map[string]interface{} {
	out := map[string]interface{}{}
	if connections, ok := status["connections"].(bson.M); ok {
		out["connections"] = connections
	}
	if opcounters, ok := status["opcounters"].(bson.M); ok {
		out["opcounters"] = opcounters
	}
	if network, ok := status["network"].(bson.M); ok {
		out["network"] = network
	}
	if mem, ok := status["mem"].(bson.M); ok {
		out["mem"] = mem
	}
	if uptime := mongoInt64(status["uptime"]); uptime > 0 {
		out["uptime"] = uptime
	}
	return out
}

func compactMongoMap(value bson.M) map[string]interface{} {
	raw, err := bson.MarshalExtJSON(value, true, true)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func writeMongoAudit(r *http.Request, action string, in ConnectionInput, target, errMsg string) {
	connID, _ := connectionIDFromPath(r.URL.Path)
	username := r.Header.Get("X-Username")
	if username == "" {
		username = r.Header.Get("X-User-ID")
	}
	connName := in.Name
	if connName == "" {
		connName = strings.TrimSpace(in.Host)
	}
	writeAuditEvent("mongodb", action, target, "", username, &connID, connName, "", 0, 0, errMsg)
}

func mongoInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := strconv.ParseInt(n.String(), 10, 64)
		return i
	default:
		return 0
	}
}
