package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type DiffColumn struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}

type TableDiff struct {
	Name    string       `json:"name"`
	Status  string       `json:"status"` // added | removed | changed | same
	ColsA   []DiffColumn `json:"cols_a,omitempty"`
	ColsB   []DiffColumn `json:"cols_b,omitempty"`
	Changes []string     `json:"changes,omitempty"`
}

type SchemaDiffResult struct {
	ConnA string      `json:"conn_a"`
	ConnB string      `json:"conn_b"`
	DbA   string      `json:"db_a"`
	DbB   string      `json:"db_b"`
	Diffs []TableDiff `json:"diffs"`
}

func GetSchemaDiff() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		q := r.URL.Query()
		connA, _ := strconv.ParseInt(q.Get("conn_a"), 10, 64)
		connB, _ := strconv.ParseInt(q.Get("conn_b"), 10, 64)
		dbA := q.Get("db_a")
		dbB := q.Get("db_b")

		if connA == 0 || connB == 0 {
			http.Error(w, `{"error":"conn_a and conn_b required"}`, http.StatusBadRequest)
			return
		}

		dbADB, driverA, err := GetDB(connA)
		if err != nil {
			http.Error(w, jsonError("connection A: "+err.Error()), http.StatusBadGateway)
			return
		}
		dbBDB, driverB, err := GetDB(connB)
		if err != nil {
			http.Error(w, jsonError("connection B: "+err.Error()), http.StatusBadGateway)
			return
		}

		tablesA, err := diffFetchSchema(dbADB, driverA, dbA)
		if err != nil {
			http.Error(w, jsonError("schema A: "+err.Error()), http.StatusInternalServerError)
			return
		}
		tablesB, err := diffFetchSchema(dbBDB, driverB, dbB)
		if err != nil {
			http.Error(w, jsonError("schema B: "+err.Error()), http.StatusInternalServerError)
			return
		}

		var diffs []TableDiff
		for name, colsA := range tablesA {
			if colsB, ok := tablesB[name]; ok {
				changes := diffCompareColumns(colsA, colsB)
				status := "same"
				if len(changes) > 0 {
					status = "changed"
				}
				diffs = append(diffs, TableDiff{Name: name, Status: status, ColsA: colsA, ColsB: colsB, Changes: changes})
			} else {
				diffs = append(diffs, TableDiff{Name: name, Status: "removed", ColsA: colsA})
			}
		}
		for name, colsB := range tablesB {
			if _, ok := tablesA[name]; !ok {
				diffs = append(diffs, TableDiff{Name: name, Status: "added", ColsB: colsB})
			}
		}
		diffSort(diffs)

		json.NewEncoder(w).Encode(SchemaDiffResult{
			ConnA: fmt.Sprintf("%d", connA),
			ConnB: fmt.Sprintf("%d", connB),
			DbA:   dbA, DbB: dbB, Diffs: diffs,
		})
	}
}

func diffFetchSchema(db *sql.DB, driver, dbName string) (map[string][]DiffColumn, error) {
	var tableQ string
	switch driver {
	case "postgres":
		schema := "public"
		if dbName != "" {
			schema = dbName
		}
		tableQ = fmt.Sprintf(`SELECT table_name FROM information_schema.tables WHERE table_schema='%s' AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	case "mysql":
		tableQ = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
	default:
		tableQ = `SELECT TABLE_NAME FROM information_schema.tables WHERE table_type='BASE TABLE' ORDER BY TABLE_NAME`
	}

	rows, err := db.Query(tableQ)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]DiffColumn)
	var tables []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tables = append(tables, t)
	}
	rows.Close()

	for _, t := range tables {
		cols, _ := diffFetchColumns(db, driver, dbName, t)
		result[t] = cols
	}
	return result, nil
}

func diffFetchColumns(db *sql.DB, driver, dbName, table string) ([]DiffColumn, error) {
	var q string
	switch driver {
	case "postgres":
		schema := "public"
		if dbName != "" {
			schema = dbName
		}
		q = fmt.Sprintf(`SELECT column_name, data_type FROM information_schema.columns WHERE table_schema='%s' AND table_name='%s' ORDER BY ordinal_position`, schema, table)
	case "mysql":
		q = fmt.Sprintf(`SELECT COLUMN_NAME, DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='%s' ORDER BY ORDINAL_POSITION`, table)
	default:
		q = fmt.Sprintf(`SELECT COLUMN_NAME, DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME='%s' ORDER BY ORDINAL_POSITION`, table)
	}

	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []DiffColumn
	for rows.Next() {
		var name, dataType string
		rows.Scan(&name, &dataType)
		cols = append(cols, DiffColumn{Name: name, DataType: dataType})
	}
	return cols, nil
}

func diffCompareColumns(a, b []DiffColumn) []string {
	var changes []string
	mapA := make(map[string]string)
	mapB := make(map[string]string)
	for _, c := range a {
		mapA[c.Name] = strings.ToLower(c.DataType)
	}
	for _, c := range b {
		mapB[c.Name] = strings.ToLower(c.DataType)
	}
	for name, typeA := range mapA {
		if typeB, ok := mapB[name]; !ok {
			changes = append(changes, "removed: "+name)
		} else if typeA != typeB {
			changes = append(changes, fmt.Sprintf("type changed: %s (%s → %s)", name, typeA, typeB))
		}
	}
	for name := range mapB {
		if _, ok := mapA[name]; !ok {
			changes = append(changes, "added: "+name)
		}
	}
	return changes
}

func diffSort(diffs []TableDiff) {
	order := map[string]int{"changed": 0, "added": 1, "removed": 2, "same": 3}
	for i := 0; i < len(diffs)-1; i++ {
		for j := i + 1; j < len(diffs); j++ {
			if order[diffs[i].Status] > order[diffs[j].Status] {
				diffs[i], diffs[j] = diffs[j], diffs[i]
			}
		}
	}
}
