package closeevent_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/store"
)

func TestUpsertIndexStoresJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := closeevent.UpsertIndex(ctx, db.DB(), "$1", `{"name":"foo"}`); err != nil {
		t.Fatal(err)
	}
	got, err := closeevent.GetIndex(ctx, db.DB(), "$1")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"name":"foo"}` {
		t.Errorf("payload = %q", got)
	}
}

func TestUpsertIndexSkipsWriteWhenPayloadUnchanged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	payload := `{"name":"foo"}`
	if err := closeevent.UpsertIndex(ctx, db.DB(), "$1", payload); err != nil {
		t.Fatal(err)
	}
	var firstUpdatedAt int64
	err = db.DB().QueryRowContext(ctx, "SELECT updated_at FROM live_index WHERE session_id=?", "$1").Scan(&firstUpdatedAt)
	if err != nil {
		t.Fatal(err)
	}

	// Sleep just enough that a real write would update the timestamp.
	time.Sleep(5 * time.Millisecond)

	if err := closeevent.UpsertIndex(ctx, db.DB(), "$1", payload); err != nil {
		t.Fatal(err)
	}
	var secondUpdatedAt int64
	err = db.DB().QueryRowContext(ctx, "SELECT updated_at FROM live_index WHERE session_id=?", "$1").Scan(&secondUpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if secondUpdatedAt != firstUpdatedAt {
		t.Errorf("updated_at changed despite identical payload: first=%d second=%d", firstUpdatedAt, secondUpdatedAt)
	}
}
