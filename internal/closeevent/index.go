// Package closeevent handles tmux pane/window/session close hooks and the
// supporting live_index used to assemble manifests at the moment a unit dies.
package closeevent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UpsertIndex stores the latest known JSON payload for a session id.
func UpsertIndex(ctx context.Context, db *sql.DB, sessionID, payload string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO live_index (session_id, payload, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at
	`, sessionID, payload, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert index: %w", err)
	}
	return nil
}

// GetIndex returns the JSON payload for sessionID, or "" if absent.
func GetIndex(ctx context.Context, db *sql.DB, sessionID string) (string, error) {
	var p string
	err := db.QueryRowContext(ctx, `SELECT payload FROM live_index WHERE session_id=?`, sessionID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get index: %w", err)
	}
	return p, nil
}

// DeleteIndex removes the entry for sessionID. Missing rows are not an error.
func DeleteIndex(ctx context.Context, db *sql.DB, sessionID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM live_index WHERE session_id=?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete index: %w", err)
	}
	return nil
}
