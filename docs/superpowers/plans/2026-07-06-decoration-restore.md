# Decoration Option Capture/Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Snapshot a configured allow-list of per-window tmux options (default `@crew_name`, `@crew_color`) and re-apply them on restore, so persona decoration survives a tmux server restart.

**Architecture:** Capture happens on the save path only — the `tmux.Client` gets the decoration option names at construction and appends `#{@opt}` fields to its `list-windows` format; `ParseWindows` labels the trailing fields into a `Decoration` map carried through the manifest. Restore reads that map straight from the manifest and emits a new `SetOption` action per key between `CreateWindow` and `SetLayout`. The allow-list gates capture only; restore replays whatever was captured.

**Tech Stack:** Go 1.x, standard `testing`, tmux CLI, existing `internal/tmux` / `internal/snapshot` / `internal/restore` packages.

## Global Constraints

- tmux `-F` field separator is `FieldSep` = `\x1f` (`internal/tmux/parse.go`). Never use another separator.
- Decoration option values may be empty (option unset on a window) — empty values are dropped, never stored.
- Decoration is excluded from `Manifest.Fingerprint()` so a color change alone does not force a snapshot.
- Best-effort restore: individual action failures are swallowed (existing `Apply` contract).
- Match surrounding code style: no comments restating what code does; explain only non-obvious WHY.

---

### Task 1: Config allow-list

**Files:**
- Modify: `internal/config/config.go` (Config struct + `Default()`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.DecorationOptions []string`, default `["@crew_name", "@crew_color"]`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestDefaultDecorationOptions(t *testing.T) {
	got := Default().DecorationOptions
	want := []string{"@crew_name", "@crew_color"}
	if !slices.Equal(got, want) {
		t.Errorf("DecorationOptions = %v, want %v", got, want)
	}
}
```

Add `"slices"` to the test file imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDefaultDecorationOptions`
Expected: FAIL — `DecorationOptions` undefined (compile error).

- [ ] **Step 3: Add the field and default**

In `internal/config/config.go`, add to the `Config` struct after `CommandAllowList`:

```go
	// DecorationOptions is the allow-list of tmux window options snapshotted
	// and re-applied verbatim on restore. Restores persona decoration (agent
	// codename, tint) that a fan-out orchestrator stamped but that nothing
	// re-derives after a server restart.
	DecorationOptions []string
```

In `Default()`, add after the `CommandAllowList` literal:

```go
		DecorationOptions: []string{"@crew_name", "@crew_color"},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestDefaultDecorationOptions`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add DecorationOptions allow-list (#16)"
```

---

### Task 2: Parse decoration fields in ParseWindows

**Files:**
- Modify: `internal/tmux/parse.go` (`WindowRow`, `ParseWindows`)
- Test: `internal/tmux/parse_test.go`

**Interfaces:**
- Consumes: `FieldSep`.
- Produces:
  - `WindowRow.Decoration map[string]string`
  - `ParseWindows(s string, decorationOpts []string) ([]WindowRow, error)` — trailing `len(decorationOpts)` fields are zipped against `decorationOpts`; empty values omitted; `Decoration` is `nil` when no options given or all empty.

- [ ] **Step 1: Write the failing tests**

The existing `ParseWindows` tests call it with one arg and expect 6 fields. Update those call sites to pass `nil` as the second arg (they must keep passing). Then add:

```go
func TestParseWindowsDecoration(t *testing.T) {
	opts := []string{"@crew_name", "@crew_color"}
	line := strings.Join([]string{
		"sess", "2", "win", "layout", "@4", "1", "dispatcher", "colour141",
	}, tmux.FieldSep)
	rows, err := tmux.ParseWindows(line+"\n", opts)
	if err != nil {
		t.Fatalf("ParseWindows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	got := rows[0].Decoration
	want := map[string]string{"@crew_name": "dispatcher", "@crew_color": "colour141"}
	if !maps.Equal(got, want) {
		t.Errorf("Decoration = %v, want %v", got, want)
	}
}

func TestParseWindowsDecorationEmptyDropped(t *testing.T) {
	opts := []string{"@crew_name", "@crew_color"}
	// @crew_color unset -> empty trailing field
	line := strings.Join([]string{
		"sess", "2", "win", "layout", "@4", "1", "dispatcher", "",
	}, tmux.FieldSep)
	rows, err := tmux.ParseWindows(line+"\n", opts)
	if err != nil {
		t.Fatalf("ParseWindows: %v", err)
	}
	want := map[string]string{"@crew_name": "dispatcher"}
	if !maps.Equal(rows[0].Decoration, want) {
		t.Errorf("Decoration = %v, want %v", rows[0].Decoration, want)
	}
}

func TestParseWindowsNoDecoration(t *testing.T) {
	line := strings.Join([]string{"sess", "2", "win", "layout", "@4", "1"}, tmux.FieldSep)
	rows, err := tmux.ParseWindows(line+"\n", nil)
	if err != nil {
		t.Fatalf("ParseWindows: %v", err)
	}
	if rows[0].Decoration != nil {
		t.Errorf("Decoration = %v, want nil", rows[0].Decoration)
	}
}
```

Add `"maps"` and `"strings"` to the test imports if absent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/ -run TestParseWindows`
Expected: FAIL — `ParseWindows` takes 1 arg / `Decoration` undefined.

- [ ] **Step 3: Implement**

In `internal/tmux/parse.go`, add to `WindowRow`:

```go
	Decoration map[string]string // allow-listed @-options; nil when none set
```

Replace `ParseWindows`:

```go
// ParseWindows parses tmux list-windows -F output. decorationOpts names the
// trailing #{@opt} fields appended to the format (in order); each non-empty
// trailing field is stored in WindowRow.Decoration keyed by its option name.
func ParseWindows(s string, decorationOpts []string) ([]WindowRow, error) {
	if s == "" {
		return nil, nil
	}
	const fixed = 6
	want := fixed + len(decorationOpts)
	var out []WindowRow
	for i, line := range splitLines(s) {
		fields := strings.Split(line, FieldSep)
		if len(fields) != want {
			return nil, fmt.Errorf("window line %d: expected %d fields, got %d", i+1, want, len(fields))
		}
		idx, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("window line %d: index: %w", i+1, err)
		}
		row := WindowRow{
			Session: fields[0], Index: idx, Name: fields[2], Layout: fields[3], ID: fields[4],
			AutomaticRename: fields[5] == "1",
		}
		for j, name := range decorationOpts {
			if v := fields[fixed+j]; v != "" {
				if row.Decoration == nil {
					row.Decoration = map[string]string{}
				}
				row.Decoration[name] = v
			}
		}
		out = append(out, row)
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -run TestParseWindows`
Expected: PASS (all, including the updated existing cases).

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/parse.go internal/tmux/parse_test.go
git commit -m "feat(tmux): parse allow-listed window decoration options (#16)"
```

---

### Task 3: Build the decoration format in Client.ListWindows

**Files:**
- Modify: `internal/tmux/client.go` (`Client` struct, `NewClient`, `windowFormat`, `ListWindows`)
- Test: `internal/tmux/client_test.go`

**Interfaces:**
- Consumes: `ParseWindows(out, opts)` from Task 2.
- Produces:
  - `NewClient(binary string, decorationOpts ...string) *Client` (variadic — existing single-arg calls still compile).
  - `Client.ListWindows` appends `FieldSep + "#{@opt}"` per decoration option to the base window format.

- [ ] **Step 1: Write the failing test**

Add to `internal/tmux/client_test.go` (unit test on the format helper — no live tmux):

```go
func TestWindowFormatWithDecoration(t *testing.T) {
	c := tmux.NewClient("tmux", "@crew_name", "@crew_color")
	got := c.WindowFormat()
	if !strings.HasSuffix(got, tmux.FieldSep+"#{@crew_name}"+tmux.FieldSep+"#{@crew_color}") {
		t.Errorf("format missing decoration fields: %q", got)
	}
}

func TestWindowFormatNoDecoration(t *testing.T) {
	c := tmux.NewClient("tmux")
	if strings.Contains(c.WindowFormat(), "#{@") {
		t.Errorf("unexpected decoration field in %q", c.WindowFormat())
	}
}
```

Add `"strings"` to imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestWindowFormat`
Expected: FAIL — `NewClient` takes 1 arg / `WindowFormat` undefined.

- [ ] **Step 3: Implement**

In `internal/tmux/client.go`:

Add field to `Client`:

```go
type Client struct {
	binary          string
	decorationOpts  []string
}
```

Replace `NewClient`:

```go
// NewClient returns a Client that invokes binary; if empty, "tmux" is used.
// decorationOpts are appended to the list-windows format as #{@opt} fields and
// captured into WindowRow.Decoration.
func NewClient(binary string, decorationOpts ...string) *Client {
	if binary == "" {
		binary = "tmux"
	}
	return &Client{binary: binary, decorationOpts: decorationOpts}
}
```

Rename the `windowFormat` const to `baseWindowFormat` (keep the value), and add a method:

```go
// WindowFormat returns the list-windows -F format, with one #{@opt} field per
// decoration option appended in order.
func (c *Client) WindowFormat() string {
	f := baseWindowFormat
	for _, o := range c.decorationOpts {
		f += FieldSep + "#{" + o + "}"
	}
	return f
}
```

Update `ListWindows` to use the dynamic format and pass names to the parser:

```go
	out, err := c.Run(ctx, []string{"list-windows", "-a", "-F", c.WindowFormat()})
	...
	return ParseWindows(out, c.decorationOpts)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): append decoration options to list-windows format (#16)"
```

---

### Task 4: Carry Decoration through the manifest

**Files:**
- Modify: `internal/snapshot/manifest.go` (`Window`, `Fingerprint`)
- Modify: `internal/snapshot/build.go` (copy `w.Decoration`)
- Test: `internal/snapshot/manifest_test.go`, `internal/snapshot/build_test.go`

**Interfaces:**
- Consumes: `tmux.WindowRow.Decoration` (Task 2).
- Produces: `snapshot.Window.Decoration map[string]string` (`json:"decoration,omitempty"`); `Fingerprint()` ignores it.

- [ ] **Step 1: Write the failing tests**

Add to `internal/snapshot/manifest_test.go`:

```go
func TestFingerprintIgnoresDecoration(t *testing.T) {
	base := Manifest{V: 1, Sessions: []Session{{Name: "s", Windows: []Window{{Index: 1}}}}}
	withDecor := Manifest{V: 1, Sessions: []Session{{Name: "s", Windows: []Window{{
		Index: 1, Decoration: map[string]string{"@crew_color": "colour141"},
	}}}}}
	if base.Fingerprint() != withDecor.Fingerprint() {
		t.Error("Fingerprint changed when only Decoration differs")
	}
}
```

Add to `internal/snapshot/build_test.go` a case asserting a `WindowRow` with `Decoration` set produces a `Window` with the same map. (Follow the existing fake-`Lister` pattern in that file; set `Decoration` on the returned `WindowRow` and assert `m.Sessions[i].Windows[j].Decoration`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/snapshot/ -run 'Fingerprint|Build'`
Expected: FAIL — `Decoration` undefined.

- [ ] **Step 3: Implement**

In `internal/snapshot/manifest.go`, add to `Window`:

```go
	Decoration map[string]string `json:"decoration,omitempty"`
```

In `Fingerprint()`, inside the window copy loop, zero it alongside the pane reset:

```go
			w2 := w
			w2.Decoration = nil
			w2.Panes = make([]Pane, len(w.Panes))
```

In `internal/snapshot/build.go`, where the `Window` is constructed (`win := Window{...}`), add `Decoration: w.Decoration`:

```go
			win := Window{Index: w.Index, Name: w.Name, Layout: w.Layout, ID: w.ID, AutomaticRename: w.AutomaticRename, Decoration: w.Decoration}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/snapshot/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/snapshot/manifest.go internal/snapshot/build.go internal/snapshot/manifest_test.go internal/snapshot/build_test.go
git commit -m "feat(snapshot): carry window decoration through manifest (#16)"
```

---

### Task 5: SetOption action + restore

**Files:**
- Modify: `internal/restore/plan.go` (`SetOption` type, emit in `BuildPlan`)
- Modify: `internal/restore/apply.go` (handle `SetOption`)
- Test: `internal/restore/plan_test.go`, `internal/restore/apply_test.go`

**Interfaces:**
- Consumes: `snapshot.Window.Decoration` (Task 4).
- Produces: `SetOption{Target string; Pane bool; Name, Value string}` Action; emitted (keys sorted) after `CreateWindow`, before `SetLayout`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/restore/plan_test.go`:

```go
func TestBuildPlanEmitsSortedSetOptions(t *testing.T) {
	m := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Name: "s", Windows: []snapshot.Window{{
			Index: 1, Layout: "L",
			Decoration: map[string]string{"@crew_color": "colour141", "@crew_name": "dispatcher"},
			Panes:      []snapshot.Pane{{Index: 0, Cwd: "/tmp", Command: "bash"}},
		}},
	}}}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{DefaultShell: "/bin/sh"})

	var sets []restore.SetOption
	var setIdx, layoutIdx int
	for i, a := range plan {
		if so, ok := a.(restore.SetOption); ok {
			sets = append(sets, so)
			setIdx = i
		}
		if _, ok := a.(restore.SetLayout); ok {
			layoutIdx = i
		}
	}
	if len(sets) != 2 {
		t.Fatalf("got %d SetOption, want 2", len(sets))
	}
	// sorted by name: @crew_color before @crew_name
	if sets[0].Name != "@crew_color" || sets[1].Name != "@crew_name" {
		t.Errorf("SetOption not sorted by name: %+v", sets)
	}
	if sets[0].Target != "s:1" || sets[0].Value != "colour141" || sets[0].Pane {
		t.Errorf("unexpected SetOption: %+v", sets[0])
	}
	if setIdx > layoutIdx {
		t.Error("SetOption emitted after SetLayout; want before")
	}
}

func TestBuildPlanNoDecorationNoSetOption(t *testing.T) {
	m := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Name: "s", Windows: []snapshot.Window{{
			Index: 1, Layout: "L",
			Panes: []snapshot.Pane{{Index: 0, Cwd: "/tmp", Command: "bash"}},
		}},
	}}}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{DefaultShell: "/bin/sh"})
	for _, a := range plan {
		if _, ok := a.(restore.SetOption); ok {
			t.Error("unexpected SetOption for window with no decoration")
		}
	}
}
```

Add to `internal/restore/apply_test.go` a case (following its fake-`Runner` pattern) asserting `SetOption{Target: "s:1", Name: "@crew_color", Value: "colour141"}` runs args `["set-window-option", "-q", "-t", "s:1", "@crew_color", "colour141"]`, and with `Pane: true` runs `set-option -pq`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/restore/ -run 'SetOption|Decoration'`
Expected: FAIL — `SetOption` undefined.

- [ ] **Step 3: Implement plan.go**

Add the action type near the other action definitions in `internal/restore/plan.go`:

```go
// SetOption re-applies one captured window/pane option on restore via
// set-window-option / set-option. Emitted per decoration key so persona
// decoration (agent codename, tint) survives a server restart.
type SetOption struct {
	Target string // <session>:<window_index>
	Pane   bool   // set-option -p vs set-window-option
	Name   string
	Value  string
}

func (SetOption) isAction() {}
```

Add `"sort"` to the imports. In `BuildPlan`, after the `CreateWindow` append and before the `keptPanes[1:]` split loop, insert:

```go
			if len(win.Decoration) > 0 {
				names := make([]string, 0, len(win.Decoration))
				for k := range win.Decoration {
					names = append(names, k)
				}
				sort.Strings(names)
				target := fmt.Sprintf("%s:%d", sess.Name, win.Index)
				for _, k := range names {
					plan = append(plan, SetOption{Target: target, Name: k, Value: win.Decoration[k]})
				}
			}
```

- [ ] **Step 4: Implement apply.go**

Add a case to the `switch v := a.(type)` in `internal/restore/apply.go`:

```go
		case SetOption:
			cmd := "set-window-option"
			flags := "-q"
			if v.Pane {
				cmd = "set-option"
				flags = "-pq"
			}
			args = []string{cmd, flags, "-t", v.Target, v.Name, v.Value}
```

(Falls through to the shared `t.Run(ctx, args)` at the end of the loop.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/restore/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/restore/plan.go internal/restore/apply.go internal/restore/plan_test.go internal/restore/apply_test.go
git commit -m "feat(restore): re-apply captured decoration options (#16)"
```

---

### Task 6: Wire config into the save-path client

**Files:**
- Modify: `cmd/tmux-state/main.go` (save-path `NewClient` call, ~line 112)

**Interfaces:**
- Consumes: `Config.DecorationOptions` (Task 1), `NewClient(binary, opts...)` (Task 3).

- [ ] **Step 1: Locate the save-path client**

Run: `grep -n 'NewClient("tmux")' cmd/tmux-state/main.go`
The save path is the call inside the save subcommand (the one feeding `snapshot.Build`, ~line 112). Restore-path clients (ListWindows for post-restore verification, ~line 405) do NOT need decoration and stay as-is.

- [ ] **Step 2: Pass decoration options**

Change the save-path construction from:

```go
				t := tmux.NewClient("tmux")
```

to:

```go
				t := tmux.NewClient("tmux", cfg.DecorationOptions...)
```

- [ ] **Step 3: Build and run the full package test suite**

Run: `go build ./... && go test ./...`
Expected: build OK, all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/tmux-state/main.go
git commit -m "feat: capture decoration options on save (#16)"
```

---

### Task 7: Integration round-trip

**Files:**
- Modify: `integration_test.go`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the failing integration test**

Following the existing `integration_test.go` harness (it spins a real tmux server via `testutil/tmuxserver.go`): create a session/window, run `set-window-option -t <win> @crew_color colour141` and `@crew_name dispatcher`, build a snapshot via a `NewClient("tmux", "@crew_name", "@crew_color")`, assert the captured `Window.Decoration` matches, then apply a restore plan into a fresh server and assert `tmux show-options -w -v -t <win> @crew_color` returns `colour141`.

Mirror the assertion style and server setup already used by the nearest existing integration test in that file (e.g. the automatic-rename or relaunch round-trip).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test . -run TestIntegration -v` (integration tests are in the repo root package)
Expected: FAIL on the decoration assertion.

- [ ] **Step 3: Confirm it passes with the implemented feature**

Since Tasks 1–6 already implement capture + restore, the test should pass once written correctly. If it fails, debug against real tmux output (check field counts and `show-options` flags).

Run: `go test . -run TestIntegration -v`
Expected: PASS.

- [ ] **Step 4: Full suite + lint + commit**

```bash
go test ./...
golangci-lint run
git add integration_test.go
git commit -m "test: integration round-trip for decoration restore (#16)"
```

---

## Self-Review

- **Spec coverage:** config allow-list (T1), capture format+parse (T2/T3), manifest carry + fingerprint exclusion (T4), SetOption restore before SetLayout (T5), save-path wiring (T6), integration (T7). All spec sections covered.
- **Pane-scoped options:** spec open item — `SetOption.Pane` and the `set-option -pq` branch exist (T5) but no default option uses them; `paneFormat`/`PaneRow` are untouched. Extending to panes later mirrors T2/T3 on the pane path.
- **Type consistency:** `Decoration map[string]string` used identically in `WindowRow` (T2), `Window` (T4); `SetOption{Target,Pane,Name,Value}` defined T5 and asserted T5/T7; `NewClient` variadic signature T3 consumed T6.
- **Placeholder scan:** integration test (T7) describes assertions against the existing harness rather than pasting a full function, because it must mirror repo-specific server setup helpers; all unit-test code is complete inline.
