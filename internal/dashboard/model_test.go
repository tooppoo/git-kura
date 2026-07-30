package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tooppoo/git-kura/internal/seal"
)

var testTime = time.Date(2026, 7, 30, 12, 34, 56, 0, time.UTC)

func fixedNow() time.Time { return testTime }

func testSnapshot() Snapshot {
	return BuildSnapshot(
		[]string{"alpha", "idle"},
		[]seal.LsClaim{
			{Key: "alpha", Path: "src/a.go"},
			{Key: "alpha", Path: "src/b.go"},
			{Key: "gone", Path: "old/x.go"},
		},
		nil,
	)
}

func newLoadedModel(t *testing.T, snap Snapshot) Model {
	t.Helper()
	m := NewModel(func() (Snapshot, error) { return snap, nil }, fixedNow, time.Minute)
	return applyMsg(t, m, loadedMsg{snapshot: snap, at: testTime})
}

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	return model
}

func applyMsgCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	return model, cmd
}

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = applyMsg(t, m, keyRunes(string(r)))
	}
	return m
}

func TestInitialLoadErrorIsShownAndRetryable(t *testing.T) {
	loadErr := errors.New("read seal store: boom")
	m := NewModel(func() (Snapshot, error) { return Snapshot{}, loadErr }, fixedNow, time.Minute)

	m = applyMsg(t, m, loadedMsg{err: loadErr, at: testTime})
	view := m.View()
	if !strings.Contains(view, "error: read seal store: boom") {
		t.Fatalf("View() = %q, want initial error message", view)
	}
	if !strings.Contains(view, "r retry") {
		t.Fatalf("View() = %q, want retry hint", view)
	}

	// r triggers a reload command even in the error state.
	_, cmd := applyMsgCmd(t, m, keyRunes("r"))
	if cmd == nil {
		t.Fatalf("r returned nil cmd, want reload cmd")
	}
	msg := cmd()
	loaded, ok := msg.(loadedMsg)
	if !ok {
		t.Fatalf("reload cmd returned %T, want loadedMsg", msg)
	}
	if loaded.err == nil {
		t.Fatalf("loadedMsg.err = nil, want the loader error")
	}
}

func TestSuccessfulLoadRendersGroups(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())
	view := m.View()

	for _, want := range []string{
		"git-kura dashboard",
		"WORKTREES  2 open / 3 claimed paths",
		"alpha  (2 claims)",
		"src/a.go",
		"src/b.go",
		"idle  (0 claims)",
		"gone  (1 orphaned claims)",
		"q quit",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
}

func TestReloadFailureKeepsSnapshotAsStale(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, loadedMsg{err: errors.New("boom"), at: testTime.Add(time.Minute)})
	view := m.View()
	if !strings.Contains(view, "stale: last success 12:34:56") {
		t.Fatalf("View() = %q, want stale marker with last success time", view)
	}
	if !strings.Contains(view, "alpha  (2 claims)") {
		t.Fatalf("View() = %q, want previous snapshot retained", view)
	}

	// A later successful reload clears the stale state.
	m = applyMsg(t, m, loadedMsg{snapshot: testSnapshot(), at: testTime.Add(2 * time.Minute)})
	if strings.Contains(m.View(), "stale:") {
		t.Fatalf("View() still shows stale after successful reload:\n%s", m.View())
	}
}

func TestTickTriggersReloadAndNextTick(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())
	_, cmd := applyMsgCmd(t, m, tickMsg(testTime))
	if cmd == nil {
		t.Fatalf("tick returned nil cmd, want reload + next tick batch")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("tick cmd returned %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2 (reload + next tick)", len(batch))
	}
	// The first batched command is the reload; the second is the next tick,
	// which is not executed here because it sleeps for the poll interval.
	msg := batch[0]()
	if _, ok := msg.(loadedMsg); !ok {
		t.Fatalf("batch[0] returned %T, want loadedMsg", msg)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{keyRunes("q"), {Type: tea.KeyCtrlC}} {
		m := newLoadedModel(t, testSnapshot())
		_, cmd := applyMsgCmd(t, m, key)
		if cmd == nil {
			t.Fatalf("key %q returned nil cmd, want quit", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("key %q cmd returned %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
}

func TestCtrlCQuitsWhileFiltering(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())
	m = applyMsg(t, m, keyRunes("/"))
	_, cmd := applyMsgCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("ctrl+c returned nil cmd while filtering, want quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c cmd returned %T, want tea.QuitMsg", cmd())
	}
}

func TestCollapseAndExpandGroup(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// Cursor starts on the alpha group row; collapse hides its paths.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	view := m.View()
	if !strings.Contains(view, "▶ alpha") {
		t.Fatalf("View() = %q, want collapsed alpha marker", view)
	}
	if strings.Contains(view, "src/a.go") {
		t.Fatalf("View() = %q, want alpha paths hidden after collapse", view)
	}

	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRight})
	view = m.View()
	if !strings.Contains(view, "▼ alpha") || !strings.Contains(view, "src/a.go") {
		t.Fatalf("View() = %q, want alpha expanded again", view)
	}
}

func TestCollapseFromPathRowSelectsGroup(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown}) // src/a.go
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})

	id, ok := m.selectedRowID()
	if !ok || id.kind != rowGroup || id.key != "alpha" {
		t.Fatalf("selected row = %+v ok=%v, want alpha group row", id, ok)
	}
	if !strings.Contains(m.View(), "▶ alpha") {
		t.Fatalf("View() = %q, want alpha collapsed", m.View())
	}
}

func TestCursorMovesAndClamps(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after up at top, want 0", m.cursor)
	}
	for i := 0; i < 100; i++ {
		m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != len(m.rows())-1 {
		t.Fatalf("cursor = %d, want last row %d", m.cursor, len(m.rows())-1)
	}
}

func TestFilterByKeyShowsAllPathsOfMatchedKey(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "alpha")
	view := m.View()

	if !strings.Contains(view, "alpha") || !strings.Contains(view, "src/a.go") || !strings.Contains(view, "src/b.go") {
		t.Fatalf("View() = %q, want alpha with all claimed paths", view)
	}
	if strings.Contains(view, "idle") || strings.Contains(view, "old/x.go") {
		t.Fatalf("View() = %q, want non-matching groups hidden", view)
	}
}

func TestFilterByPathShowsOwnerKeyAndMatchedPathOnly(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "a.go")
	view := m.View()

	if !strings.Contains(view, "alpha") || !strings.Contains(view, "src/a.go") {
		t.Fatalf("View() = %q, want owner key with matched path", view)
	}
	if strings.Contains(view, "src/b.go") {
		t.Fatalf("View() = %q, want non-matching path hidden", view)
	}
}

func TestFilterAutoExpandsPathMatchedGroup(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// Collapse alpha first, then filter by one of its paths.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "a.go")

	view := m.View()
	if !strings.Contains(view, "src/a.go") {
		t.Fatalf("View() = %q, want collapsed group auto-expanded on path match", view)
	}
}

func TestFilterAutoExpandsKeyMatchedCollapsedGroup(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// Collapse alpha, then filter by its key: the full claim list must be
	// visible without a manual expand.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "alpha")

	view := m.View()
	if !strings.Contains(view, "src/a.go") || !strings.Contains(view, "src/b.go") {
		t.Fatalf("View() = %q, want collapsed group auto-expanded on key match", view)
	}

	// Esc restores the pre-filter collapsed state.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "▶ alpha") {
		t.Fatalf("View() = %q, want alpha collapsed again after filter cleared", m.View())
	}
}

func TestClearFilterRestoresExpansionState(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// Collapse alpha, then run a filter session that auto-expands it.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "a.go")
	if !strings.Contains(m.View(), "src/a.go") {
		t.Fatalf("precondition: filter should auto-expand alpha")
	}

	// Esc clears the filter and restores the pre-filter collapsed state.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	view := m.View()
	if strings.Contains(view, "filter:") && strings.Contains(view, "a.go▌") {
		t.Fatalf("View() = %q, want filter input cleared", view)
	}
	if !strings.Contains(view, "▶ alpha") || strings.Contains(view, "src/a.go") {
		t.Fatalf("View() = %q, want alpha collapsed again after filter cleared", view)
	}
	if !strings.Contains(view, "idle") {
		t.Fatalf("View() = %q, want all groups visible after filter cleared", view)
	}
}

func TestCollapseChangesDuringFilterAreDiscardedOnClear(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// alpha starts expanded; collapse it during the filter session.
	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "alpha")
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(m.View(), "▶ alpha") {
		t.Fatalf("precondition: alpha collapsed during filter")
	}

	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "▼ alpha") {
		t.Fatalf("View() = %q, want pre-filter expansion restored", m.View())
	}
}

func TestFilterBackspaceEditsQuery(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "alx")
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeString(t, m, "pha")

	if m.filter != "alpha" {
		t.Fatalf("filter = %q, want alpha", m.filter)
	}
	if !strings.Contains(m.View(), "filter: alpha") {
		t.Fatalf("View() = %q, want filter line", m.View())
	}
}

func TestEnterOnEmptyFilterEndsFilterSession(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, keyRunes("/"))
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.filtering || m.filter != "" || m.savedCollapsed != nil {
		t.Fatalf("model = filtering:%v filter:%q saved:%v, want session fully cleared",
			m.filtering, m.filter, m.savedCollapsed)
	}
}

func TestReloadPreservesSelectionByIdentity(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	// Select the src/b.go path row.
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	id, ok := m.selectedRowID()
	if !ok || id.path != "src/b.go" {
		t.Fatalf("precondition: selected = %+v, want src/b.go", id)
	}

	// A new snapshot inserts a group before alpha, shifting row indexes.
	next := BuildSnapshot(
		[]string{"aaa-new", "alpha", "idle"},
		[]seal.LsClaim{
			{Key: "aaa-new", Path: "new/file.go"},
			{Key: "alpha", Path: "src/a.go"},
			{Key: "alpha", Path: "src/b.go"},
		},
		nil,
	)
	m = applyMsg(t, m, loadedMsg{snapshot: next, at: testTime.Add(time.Minute)})

	id, ok = m.selectedRowID()
	if !ok || id.kind != rowPath || id.path != "src/b.go" {
		t.Fatalf("selected after reload = %+v, want src/b.go path row", id)
	}
}

func TestReloadKeepsFilterAndCollapseState(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())

	m = applyMsg(t, m, keyRunes("/"))
	m = typeString(t, m, "alpha")
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = applyMsg(t, m, loadedMsg{snapshot: testSnapshot(), at: testTime.Add(time.Minute)})
	if m.filter != "alpha" {
		t.Fatalf("filter = %q after reload, want alpha", m.filter)
	}
	if !strings.Contains(m.View(), "filter: alpha") {
		t.Fatalf("View() = %q, want filter retained after reload", m.View())
	}
}

func TestViolationsAreRenderedDistinctly(t *testing.T) {
	badPath := "a/../b.txt"
	snap := BuildSnapshot(nil,
		[]seal.LsClaim{{Key: "k", Path: badPath}},
		[]seal.DoctorFinding{{Severity: "error", Code: "non-normalized-path", Path: &badPath, Message: "not normalized"}},
	)
	m := newLoadedModel(t, snap)
	view := m.View()

	if !strings.Contains(view, "! non-normalized-path a/../b.txt") {
		t.Fatalf("View() = %q, want violation row", view)
	}
	if strings.Contains(view, "    a/../b.txt") {
		t.Fatalf("View() = %q, violated path must not render as a normal claim", view)
	}
}

func TestScrollFollowsCursorOnSmallTerminal(t *testing.T) {
	claims := make([]seal.LsClaim, 0, 20)
	for _, p := range []string{
		"p/a", "p/b", "p/c", "p/d", "p/e", "p/f", "p/g", "p/h", "p/i", "p/j",
	} {
		claims = append(claims, seal.LsClaim{Key: "big", Path: p})
	}
	m := newLoadedModel(t, BuildSnapshot([]string{"big"}, claims, nil))
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 9})

	for i := 0; i < 10; i++ {
		m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	view := m.View()
	if !strings.Contains(view, "p/j") {
		t.Fatalf("View() = %q, want cursor row p/j visible after scrolling", view)
	}
	if strings.Contains(view, "▼ big") {
		t.Fatalf("View() = %q, want top row scrolled out of the window", view)
	}

	// Scrolling back up brings the group row into view again.
	for i := 0; i < 10; i++ {
		m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	if !strings.Contains(m.View(), "▼ big") {
		t.Fatalf("View() = %q, want group row visible after scrolling up", m.View())
	}
}

func TestResizeKeepsModelUsable(t *testing.T) {
	m := newLoadedModel(t, testSnapshot())
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 20, Height: 6})

	for _, line := range strings.Split(m.View(), "\n") {
		if len([]rune(line)) > 20 {
			t.Fatalf("line %q longer than width 20", line)
		}
	}

	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(m.View(), "src/a.go") {
		t.Fatalf("View() = %q, want full content after growing the terminal", m.View())
	}
}

func TestInitReturnsCommand(t *testing.T) {
	m := NewModel(func() (Snapshot, error) { return Snapshot{}, nil }, fixedNow, time.Minute)
	if m.Init() == nil {
		t.Fatalf("Init() = nil, want initial load and tick commands")
	}
}
