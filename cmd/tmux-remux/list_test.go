package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/noamsto/tmux-remux/internal/store"
)

func testListEvents() []store.Event {
	parentID := int64(1)
	return []store.Event{
		{
			ID:            1,
			Ts:            1700000000000,
			Kind:          "snapshot",
			Scope:         "server",
			Reason:        "periodic",
			Host:          "host-a",
			ParentEventID: nil,
			ManifestJSON:  `{"v":1,"host":"host-a","saved_at":1700000000000,"sessions":[]}`,
		},
		{
			ID:            2,
			Ts:            1700000060000,
			Kind:          "close",
			Scope:         "pane",
			Reason:        "pane_closed",
			Host:          "host-a",
			ParentEventID: &parentID,
			ManifestJSON:  `{"v":1,"host":"host-a","saved_at":1700000060000,"sessions":[]}`,
		},
	}
}

func TestWriteListJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := writeListJSON(&buf, testListEvents()); err != nil {
		t.Fatalf("writeListJSON: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "list_json.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), string(want))
	}
}

func TestWriteListJSONMatchesStoreEvent(t *testing.T) {
	evs := testListEvents()
	var got bytes.Buffer
	if err := writeListJSON(&got, evs); err != nil {
		t.Fatalf("writeListJSON: %v", err)
	}
	var want bytes.Buffer
	enc := json.NewEncoder(&want)
	for _, ev := range evs {
		if err := enc.Encode(ev); err != nil {
			t.Fatalf("encode store.Event: %v", err)
		}
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("wire output drifted from store.Event:\ngot:\n%s\nwant:\n%s", got.String(), want.String())
	}
}
