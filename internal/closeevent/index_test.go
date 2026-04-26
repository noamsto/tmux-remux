package closeevent_test

import (
	"context"
	"path/filepath"
	"testing"

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
