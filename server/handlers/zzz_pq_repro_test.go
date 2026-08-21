package handlers

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	"github.com/joho/godotenv"
)

func TestZZZPQRepro(t *testing.T) {
	if err := godotenv.Load("../../.env"); err != nil {
		t.Skip("no .env")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := appdb.Init(cfg); err != nil {
		t.Fatalf("db: %v", err)
	}
	SetEncryptionKey(cfg.EncryptionKey)

	f, err := os.Open("/private/tmp/claude-501/-Users-pandhuwibowo-Portfolio-anveesa-anveesa-nias/b3b505ec-4c56-4000-a99e-ad9aede9deee/scratchpad/seqs.txt")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var seqStmts []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), ";")
		if line != "" {
			seqStmts = append(seqStmts, line)
		}
	}
	t.Logf("loaded %d real sequence statements", len(seqStmts))

	db, _, err := GetDB(16)
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmts := []string{
		`SET search_path TO "public"`,
		`SET session_replication_role = replica`,
	}
	stmts = append(stmts, seqStmts...)
	stmts = append(stmts, `CREATE TABLE IF NOT EXISTS "public"."account_balance_logs" (
    "id" bigint NOT NULL DEFAULT nextval('account_balance_logs_id_seq'),
    "account_id" bigint NOT NULL,
    "merchant_id" bigint NOT NULL,
    PRIMARY KEY ("id")
)`)

	for i, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			t.Fatalf("stmt %d/%d (%.100s) via ExecContext failed: %v", i, len(stmts), s, err)
		}
	}
	tx.Rollback()
	t.Log("ExecContext path: all statements succeeded with REAL sequence names")
}
