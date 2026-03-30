package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var genesisHash = fmt.Sprintf("%x", sha256.Sum256([]byte("genesis")))

func computeHash(id int64, action, detail string, createdAt time.Time) string {
	raw := fmt.Sprintf("%d|%s|%s|%s", id, action, detail, createdAt.Format(time.RFC3339Nano))
	masterKey := os.Getenv("MASTER_KEY")
	if masterKey != "" {
		mac := hmac.New(sha256.New, []byte(masterKey))
		mac.Write([]byte(raw))
		return fmt.Sprintf("%x", mac.Sum(nil))
	}
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

func WriteAuditLog(ctx context.Context, pool *pgxpool.Pool, action, detail string, serverID *uuid.UUID, userID *string, sourceIP string) error {
	var prevHash string
	var lastID int64
	var lastAction, lastDetail string
	var lastCreated time.Time

	err := pool.QueryRow(ctx,
		`SELECT id, action, COALESCE(detail,''), created_at FROM audit_log ORDER BY id DESC LIMIT 1`,
	).Scan(&lastID, &lastAction, &lastDetail, &lastCreated)

	if err != nil {
		prevHash = genesisHash
	} else {
		prevHash = computeHash(lastID, lastAction, lastDetail, lastCreated)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO audit_log (user_id, server_id, action, detail, source_ip, prev_hash)
		 VALUES ((SELECT id FROM users WHERE username=$1), $2, $3, $4, $5, $6)`,
		userID, serverID, action, detail, sourceIP, prevHash,
	)
	return err
}

type AuditVerifyResult struct {
	Valid    bool   `json:"valid"`
	Total   int    `json:"total_entries"`
	BrokenAt *int64 `json:"broken_at"`
}

func VerifyAuditChain(ctx context.Context, pool *pgxpool.Pool) (*AuditVerifyResult, error) {
	rows, err := pool.Query(ctx, `SELECT id, action, COALESCE(detail,''), prev_hash, created_at FROM audit_log ORDER BY id ASC`)
	if err != nil { return nil, err }
	defer rows.Close()

	type entry struct { ID int64; Action, Detail, PrevHash string; CreatedAt time.Time }
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Action, &e.Detail, &e.PrevHash, &e.CreatedAt); err != nil { return nil, err }
		entries = append(entries, e)
	}

	if len(entries) == 0 { return &AuditVerifyResult{Valid: true, Total: 0}, nil }
	if entries[0].PrevHash != genesisHash {
		return &AuditVerifyResult{Valid: false, Total: len(entries), BrokenAt: &entries[0].ID}, nil
	}
	for i := 1; i < len(entries); i++ {
		expected := computeHash(entries[i-1].ID, entries[i-1].Action, entries[i-1].Detail, entries[i-1].CreatedAt)
		if entries[i].PrevHash != expected {
			return &AuditVerifyResult{Valid: false, Total: len(entries), BrokenAt: &entries[i].ID}, nil
		}
	}
	return &AuditVerifyResult{Valid: true, Total: len(entries)}, nil
}
