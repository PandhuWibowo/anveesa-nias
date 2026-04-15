package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type txEntry struct {
	tx     *sql.Tx
	driver string
}

var txPool struct {
	sync.RWMutex
	txs map[int64]*txEntry
}

func init() {
	txPool.txs = make(map[int64]*txEntry)
}

// GetActiveTx returns the active transaction for a connection, if any.
func GetActiveTx(connID int64) (*sql.Tx, string, bool) {
	txPool.RLock()
	defer txPool.RUnlock()
	e, ok := txPool.txs[connID]
	if !ok {
		return nil, "", false
	}
	return e.tx, e.driver, true
}

func BeginTransaction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		txPool.RLock()
		_, exists := txPool.txs[connID]
		txPool.RUnlock()
		if exists {
			http.Error(w, `{"error":"transaction already active"}`, http.StatusConflict)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		txPool.Lock()
		txPool.txs[connID] = &txEntry{tx: tx, driver: driver}
		txPool.Unlock()

		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Transaction started"})
	}
}

func CommitTransaction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		txPool.Lock()
		entry, ok := txPool.txs[connID]
		if !ok {
			txPool.Unlock()
			http.Error(w, `{"error":"no active transaction"}`, http.StatusBadRequest)
			return
		}
		delete(txPool.txs, connID)
		txPool.Unlock()

		if err := entry.tx.Commit(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Transaction committed"})
	}
}

func RollbackTransaction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)

		txPool.Lock()
		entry, ok := txPool.txs[connID]
		if !ok {
			txPool.Unlock()
			http.Error(w, `{"error":"no active transaction"}`, http.StatusBadRequest)
			return
		}
		delete(txPool.txs, connID)
		txPool.Unlock()

		entry.tx.Rollback()
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Transaction rolled back"})
	}
}

func TxStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		txPool.RLock()
		_, active := txPool.txs[connID]
		txPool.RUnlock()
		json.NewEncoder(w).Encode(map[string]any{"active": active})
	}
}
