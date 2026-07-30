package dashboard

import (
	"reflect"
	"testing"
)

func filterFixture() []Group {
	return []Group{
		{Key: "dashboard-ui", Paths: []string{"crates/cli/src/dashboard.rs", "crates/cli/src/main.rs"}},
		{Key: "seal-refactor", Paths: []string{"core/seal.go", "core/store.go"}},
		{Key: "idle-task", Paths: nil},
	}
}

func TestApplyFilterEmptyQueryKeepsEverything(t *testing.T) {
	groups := filterFixture()
	visible := ApplyFilter(groups, "")

	if len(visible) != len(groups) {
		t.Fatalf("len(visible) = %d, want %d", len(visible), len(groups))
	}
	for i, v := range visible {
		if v.Key != groups[i].Key {
			t.Fatalf("visible[%d].Key = %q, want %q", i, v.Key, groups[i].Key)
		}
		if !reflect.DeepEqual(v.VisiblePaths, groups[i].Paths) {
			t.Fatalf("visible[%d].VisiblePaths = %v, want %v", i, v.VisiblePaths, groups[i].Paths)
		}
		if v.AutoExpand {
			t.Fatalf("visible[%d].AutoExpand = true, want false", i)
		}
	}
}

func TestApplyFilterKeyMatchKeepsAllPaths(t *testing.T) {
	visible := ApplyFilter(filterFixture(), "dashboard")

	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1: %+v", len(visible), visible)
	}
	v := visible[0]
	if v.Key != "dashboard-ui" {
		t.Fatalf("Key = %q, want dashboard-ui", v.Key)
	}
	if !reflect.DeepEqual(v.VisiblePaths, []string{"crates/cli/src/dashboard.rs", "crates/cli/src/main.rs"}) {
		t.Fatalf("VisiblePaths = %v, want all claimed paths", v.VisiblePaths)
	}
	if v.AutoExpand {
		t.Fatalf("AutoExpand = true, want false for key match")
	}
}

func TestApplyFilterPathMatchKeepsOnlyMatchedPaths(t *testing.T) {
	visible := ApplyFilter(filterFixture(), "store")

	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1: %+v", len(visible), visible)
	}
	v := visible[0]
	if v.Key != "seal-refactor" {
		t.Fatalf("Key = %q, want seal-refactor", v.Key)
	}
	if !reflect.DeepEqual(v.VisiblePaths, []string{"core/store.go"}) {
		t.Fatalf("VisiblePaths = %v, want only core/store.go", v.VisiblePaths)
	}
	if !v.AutoExpand {
		t.Fatalf("AutoExpand = false, want true for path match")
	}
}

func TestApplyFilterIsCaseInsensitive(t *testing.T) {
	visible := ApplyFilter(filterFixture(), "DASHBOARD")
	if len(visible) != 1 || visible[0].Key != "dashboard-ui" {
		t.Fatalf("visible = %+v, want dashboard-ui via case-insensitive key match", visible)
	}

	visible = ApplyFilter(filterFixture(), "STORE.GO")
	if len(visible) != 1 || !reflect.DeepEqual(visible[0].VisiblePaths, []string{"core/store.go"}) {
		t.Fatalf("visible = %+v, want core/store.go via case-insensitive path match", visible)
	}
}

func TestApplyFilterDropsNonMatchingGroups(t *testing.T) {
	visible := ApplyFilter(filterFixture(), "no-such-thing")
	if len(visible) != 0 {
		t.Fatalf("visible = %+v, want empty", visible)
	}
}

func TestApplyFilterKeyMatchWinsOverPathSubset(t *testing.T) {
	// "seal" matches the key seal-refactor, so all of its paths stay visible
	// even though only core/seal.go contains the substring.
	visible := ApplyFilter(filterFixture(), "seal")

	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1: %+v", len(visible), visible)
	}
	if !reflect.DeepEqual(visible[0].VisiblePaths, []string{"core/seal.go", "core/store.go"}) {
		t.Fatalf("VisiblePaths = %v, want all paths on key match", visible[0].VisiblePaths)
	}
}
