package dashboard

import "strings"

// VisibleGroup is one group after filtering, carrying the subset of paths to
// display and whether the group must be auto-expanded while the filter is
// active.
type VisibleGroup struct {
	Group
	// VisiblePaths holds the paths to display: all claimed paths when the key
	// matched (or no filter is active), otherwise only the matched paths.
	VisiblePaths []string
	// AutoExpand is set when a path matched the filter, so the group is
	// expanded while the filter is active regardless of its saved state.
	AutoExpand bool
}

// ApplyFilter returns the groups visible under a case-insensitive substring
// filter over keys and claimed paths.
//
//   - An empty query keeps every group with all of its paths.
//   - A key match keeps the group with all of its paths.
//   - A path match keeps the group with only the matched paths and marks it
//     for auto-expansion.
//   - A group with no key match and no path match is dropped.
func ApplyFilter(groups []Group, query string) []VisibleGroup {
	if query == "" {
		visible := make([]VisibleGroup, len(groups))
		for i, g := range groups {
			visible[i] = VisibleGroup{Group: g, VisiblePaths: g.Paths}
		}
		return visible
	}

	q := strings.ToLower(query)
	var visible []VisibleGroup
	for _, g := range groups {
		if strings.Contains(strings.ToLower(g.Key), q) {
			visible = append(visible, VisibleGroup{Group: g, VisiblePaths: g.Paths})
			continue
		}
		var matched []string
		for _, p := range g.Paths {
			if strings.Contains(strings.ToLower(p), q) {
				matched = append(matched, p)
			}
		}
		if len(matched) > 0 {
			visible = append(visible, VisibleGroup{Group: g, VisiblePaths: matched, AutoExpand: true})
		}
	}
	return visible
}
