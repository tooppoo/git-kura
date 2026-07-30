package dashboard

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickMsg triggers one periodic reload cycle.
type tickMsg time.Time

// loadedMsg carries the result of one snapshot load.
type loadedMsg struct {
	snapshot Snapshot
	err      error
	at       time.Time
}

// rowKind distinguishes the selectable line kinds of the scrollable list.
type rowKind int

const (
	rowGroup rowKind = iota
	rowPath
	rowViolation
)

// row is one selectable line of the scrollable list.
type row struct {
	kind      rowKind
	key       string
	path      string
	group     VisibleGroup
	violation Violation
}

// rowID identifies a row across rebuilds so selection survives reloads.
type rowID struct {
	kind rowKind
	key  string
	path string
}

// Model is the bubbletea model for the dashboard. Update and View are pure,
// so the whole interaction cycle is testable without a terminal.
type Model struct {
	loader   func() (Snapshot, error)
	now      func() time.Time
	interval time.Duration

	snapshot    Snapshot
	loaded      bool
	initialErr  error
	stale       bool
	lastSuccess time.Time

	cursor int
	scroll int
	// collapsed records groups explicitly collapsed by the user; a key that
	// is absent is expanded. Tracking only the collapsed side keeps newly
	// appearing keys expanded by default.
	collapsed map[string]bool
	// savedCollapsed snapshots collapsed when a filter session starts and is
	// restored when the filter is cleared. nil means no filter session.
	savedCollapsed map[string]bool
	// filterCollapsed holds collapse toggles made during the filter session
	// for auto-expanded groups. It starts empty so a matched group is
	// expanded regardless of its pre-filter state, while a collapse the user
	// performs during the session still takes effect.
	filterCollapsed map[string]bool
	filter          string
	filtering       bool

	width  int
	height int
}

// NewModel builds a dashboard model. loader performs one lock-free snapshot
// read, now supplies timestamps, and interval is the periodic reload cadence.
func NewModel(loader func() (Snapshot, error), now func() time.Time, interval time.Duration) Model {
	return Model{
		loader:    loader,
		now:       now,
		interval:  interval,
		collapsed: make(map[string]bool),
	}
}

// Init starts the first load and the periodic reload cycle.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), m.tickCmd())
}

func (m Model) loadCmd() tea.Cmd {
	loader, now := m.loader, m.now
	return func() tea.Msg {
		snapshot, err := loader()
		return loadedMsg{snapshot: snapshot, err: err, at: now()}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles one message. It never touches the terminal.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(m.loadCmd(), m.tickCmd())
	case loadedMsg:
		return m.applyLoaded(msg), nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampCursor()
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			return m.updateFiltering(msg)
		}
		return m.updateNavigation(msg)
	}
	return m, nil
}

// applyLoaded merges one load result into the model. A failure before the
// first success becomes the initial error screen; a failure after a success
// keeps the previous snapshot and marks it stale.
func (m Model) applyLoaded(msg loadedMsg) Model {
	if msg.err != nil {
		if !m.loaded {
			m.initialErr = msg.err
		} else {
			m.stale = true
		}
		return m
	}
	selected, ok := m.selectedRowID()
	m.snapshot = msg.snapshot
	m.loaded = true
	m.initialErr = nil
	m.stale = false
	m.lastSuccess = msg.at
	m.restoreSelection(selected, ok)
	return m
}

func (m Model) updateNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		return m, m.loadCmd()
	case "/":
		if m.savedCollapsed == nil {
			m.savedCollapsed = copyBoolMap(m.collapsed)
			m.filterCollapsed = make(map[string]bool)
		}
		m.filtering = true
		return m, nil
	case "esc":
		if m.filter != "" || m.savedCollapsed != nil {
			m = m.clearFilter()
		}
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "left", "h":
		m.collapseSelected()
		return m, nil
	case "right", "l":
		m.expandSelected()
		return m, nil
	}
	return m, nil
}

func (m Model) updateFiltering(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.clearFilter(), nil
	case "enter":
		if m.filter == "" {
			return m.clearFilter(), nil
		}
		m.filtering = false
		m.clampCursor()
		return m, nil
	case "backspace":
		if m.filter != "" {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
		}
		m.clampCursor()
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.filter += string(msg.Runes)
		m.clampCursor()
	}
	return m, nil
}

// clearFilter drops the filter text and restores the expand/collapse state
// captured when the filter session started.
func (m Model) clearFilter() Model {
	m.filter = ""
	m.filtering = false
	if m.savedCollapsed != nil {
		m.collapsed = m.savedCollapsed
		m.savedCollapsed = nil
	}
	m.filterCollapsed = nil
	m.clampCursor()
	return m
}

func copyBoolMap(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// expandedForDisplay reports whether the group's paths are shown. A matched
// group is auto-expanded while the filter is active so its matching paths
// are visible even when it was collapsed before the filter; a collapse the
// user performs during the filter session still wins. Without a filter the
// user's collapse state applies.
func (m Model) expandedForDisplay(g VisibleGroup) bool {
	if m.filter != "" && g.AutoExpand {
		return !m.filterCollapsed[g.Key]
	}
	return !m.collapsed[g.Key]
}

// rows builds the scrollable list: group rows, path rows of expanded groups,
// and store integrity violation rows.
func (m Model) rows() []row {
	var rows []row
	for _, g := range ApplyFilter(m.snapshot.Groups, m.filter) {
		rows = append(rows, row{kind: rowGroup, key: g.Key, group: g})
		if m.expandedForDisplay(g) {
			for _, p := range g.VisiblePaths {
				rows = append(rows, row{kind: rowPath, key: g.Key, path: p})
			}
		}
	}
	for _, v := range m.snapshot.Violations {
		rows = append(rows, row{kind: rowViolation, key: v.Code, path: v.Path, violation: v})
	}
	return rows
}

func (m Model) selectedRowID() (rowID, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return rowID{}, false
	}
	r := rows[m.cursor]
	return rowID{kind: r.kind, key: r.key, path: r.path}, true
}

// restoreSelection re-points the cursor at the row selected before a rebuild
// when that row still exists, and clamps it otherwise.
func (m *Model) restoreSelection(selected rowID, ok bool) {
	if ok {
		for i, r := range m.rows() {
			if (rowID{kind: r.kind, key: r.key, path: r.path}) == selected {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
}

func (m *Model) clampCursor() {
	n := len(m.rows())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll(n)
}

// clampScroll keeps the cursor inside the visible window.
func (m *Model) clampScroll(rowCount int) {
	vh := m.viewportHeight()
	if vh <= 0 {
		m.scroll = 0
		return
	}
	if m.scroll > m.cursor {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+vh {
		m.scroll = m.cursor - vh + 1
	}
	if maxScroll := rowCount - vh; m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// headerFooterLines is the number of fixed lines around the scrollable list.
const headerFooterLines = 5

// viewportHeight returns the row count of the scrollable window, or 0 when
// the terminal size is unknown (everything is rendered).
func (m Model) viewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	vh := m.height - headerFooterLines
	if vh < 1 {
		vh = 1
	}
	return vh
}

func (m *Model) collapseSelected() {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return
	}
	r := rows[m.cursor]
	if r.kind == rowViolation {
		return
	}
	// During a filter session the collapsed write is shadowed by
	// filterCollapsed for display and discarded on clearFilter; it is kept so
	// the non-session path stays uniform.
	m.collapsed[r.key] = true
	if m.savedCollapsed != nil {
		m.filterCollapsed[r.key] = true
	}
	if r.kind == rowPath {
		for i, cand := range m.rows() {
			if cand.kind == rowGroup && cand.key == r.key {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
}

func (m *Model) expandSelected() {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return
	}
	r := rows[m.cursor]
	if r.kind != rowGroup {
		return
	}
	delete(m.collapsed, r.key)
	delete(m.filterCollapsed, r.key)
}

// View renders the whole screen as a string. It never touches the terminal.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("git-kura dashboard\n")

	if !m.loaded {
		if m.initialErr != nil {
			b.WriteString("\n")
			fmt.Fprintf(&b, "error: %v\n", m.initialErr)
			b.WriteString("\n")
			b.WriteString("r retry   q quit\n")
			return m.clipToWidth(b.String())
		}
		b.WriteString("\nloading...\n")
		return m.clipToWidth(b.String())
	}

	status := fmt.Sprintf("WORKTREES  %d open / %d claimed paths", m.snapshot.OpenKeys, m.snapshot.ClaimedPaths)
	if m.stale {
		status += fmt.Sprintf("   [stale: last success %s]", m.lastSuccess.Format("15:04:05"))
	}
	b.WriteString(status)
	b.WriteString("\n")

	if m.filtering {
		fmt.Fprintf(&b, "filter: %s▌\n", m.filter)
	} else if m.filter != "" {
		fmt.Fprintf(&b, "filter: %s\n", m.filter)
	} else {
		b.WriteString("\n")
	}

	rows := m.rows()
	start, end := 0, len(rows)
	if vh := m.viewportHeight(); vh > 0 {
		start = m.scroll
		if start > len(rows) {
			start = len(rows)
		}
		end = start + vh
		if end > len(rows) {
			end = len(rows)
		}
	}
	for i := start; i < end; i++ {
		b.WriteString(m.renderRow(rows[i], i == m.cursor))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString("↑↓ select   ←→ collapse/expand   / filter   esc clear filter   r refresh   q quit\n")
	return m.clipToWidth(b.String())
}

func (m Model) renderRow(r row, selected bool) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	switch r.kind {
	case rowGroup:
		g := r.group
		symbol := "▼"
		if !m.expandedForDisplay(g) {
			symbol = "▶"
		}
		count := fmt.Sprintf("%d claims", len(g.Paths))
		if g.Orphaned {
			symbol = "!"
			count = fmt.Sprintf("%d orphaned claims", len(g.Paths))
		}
		return fmt.Sprintf("%s%s %s  (%s)", marker, symbol, g.Key, count)
	case rowPath:
		return fmt.Sprintf("%s    %s", marker, r.path)
	default:
		return fmt.Sprintf("%s! %s %s: %s", marker, r.violation.Code, r.violation.Path, r.violation.Message)
	}
}

// clipToWidth truncates every line to the terminal width so long paths never
// wrap and break the layout.
func (m Model) clipToWidth(s string) string {
	if m.width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if runes := []rune(line); len(runes) > m.width {
			lines[i] = string(runes[:m.width])
		}
	}
	return strings.Join(lines, "\n")
}
