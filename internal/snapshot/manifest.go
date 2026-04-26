// Package snapshot defines the manifest schema and the save/restore-relevant
// data shape for tmux state.
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Manifest is the top-level snapshot envelope persisted in events.manifest_json.
type Manifest struct {
	V        int       `json:"v"`
	Host     string    `json:"host"`
	TmuxPID  int       `json:"tmux_pid,omitempty"`
	SavedAt  int64     `json:"saved_at"`
	Sessions []Session `json:"sessions"`
}

// Session captures one tmux session's structure.
type Session struct {
	Name         string   `json:"name"`
	LastAttached int64    `json:"last_attached"`
	Windows      []Window `json:"windows"`
}

// Window captures one tmux window's structure within a session.
type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Layout string `json:"layout"`
	Panes  []Pane `json:"panes"`
}

// Pane captures one tmux pane's state, including optional scrollback hash.
type Pane struct {
	Index         int      `json:"index"`
	Cwd           string   `json:"cwd"`
	Command       string   `json:"command"`
	CommandArgs   []string `json:"command_args,omitempty"`
	LastUsed      int64    `json:"last_used"`
	ChildCount    int      `json:"child_count"`
	ScrollbackSHA string   `json:"scrollback_sha,omitempty"`
}

// Fingerprint returns a sha256 hex of the manifest with timestamps zeroed,
// suitable for "did anything change since last save?" checks.
func (m Manifest) Fingerprint() string {
	cp := m
	cp.SavedAt = 0
	cp.Sessions = make([]Session, len(m.Sessions))
	for i, s := range m.Sessions {
		s2 := s
		s2.LastAttached = 0
		s2.Windows = make([]Window, len(s.Windows))
		for j, w := range s.Windows {
			w2 := w
			w2.Panes = make([]Pane, len(w.Panes))
			for k, p := range w.Panes {
				p2 := p
				p2.LastUsed = 0
				w2.Panes[k] = p2
			}
			s2.Windows[j] = w2
		}
		cp.Sessions[i] = s2
	}
	data, _ := json.Marshal(cp)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
