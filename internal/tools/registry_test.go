package tools

import (
	"strings"
	"testing"
)

// failingFixture always returns ActionFailed; used to test duplicate-ID detection.
type failingFixture struct{ componentID string }

func (c *failingFixture) ID() string { return c.componentID }
func (c *failingFixture) Status(_ Context) Outcome {
	return Outcome{Result: Result{Component: c.componentID, Action: ActionFailed, Reason: "boom"}}
}
func (c *failingFixture) Install(_ InstallContext) Outcome {
	return Outcome{Result: Result{Component: c.componentID, Action: ActionFailed, Reason: "boom"}}
}
func (c *failingFixture) Uninstall(_ Context) Outcome {
	return Outcome{Result: Result{Component: c.componentID, Action: ActionFailed, Reason: "boom"}}
}

func TestToolsProductionRegistryRecognizesComponents(t *testing.T) {
	reg, err := ProductionRegistry()
	if err != nil {
		t.Fatalf("ProductionRegistry: %v", err)
	}
	got := reg.IDs()
	want := []string{"pre-commit", "claude-skill", "codex-skill"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("production registry IDs = %v, want %v", got, want)
	}
	for _, id := range want {
		if _, ok := reg.Get(id); !ok {
			t.Fatalf("component %q not recognized", id)
		}
	}
	// No dummy/test components must leak into the production registry.
	for _, id := range got {
		if strings.Contains(id, "dummy") || strings.Contains(id, "test") || id == "alpha" {
			t.Fatalf("unexpected component %q in production registry", id)
		}
	}
}

func TestNewToolsRegistryRejectsDuplicateID(t *testing.T) {
	_, err := NewRegistry(&failingFixture{componentID: "dup"}, &failingFixture{componentID: "dup"})
	if err == nil {
		t.Fatal("duplicate component ID should return an error")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("error %q should mention the duplicate ID", err.Error())
	}
}

func TestPendingComponentAndResultFailureContract(t *testing.T) {
	comp := PendingComponent{ComponentID: "later", TrackingURL: "https://example.test/issue"}
	if comp.ID() != "later" {
		t.Fatalf("ID = %q", comp.ID())
	}

	status := comp.Status(Context{})
	if status.Result.Action != ActionNotInstalled || status.Result.Managed || !strings.Contains(status.Result.Reason, "https://example.test/issue") {
		t.Fatalf("status = %#v", status.Result)
	}
	install := comp.Install(InstallContext{ReleaseVersion: "1.2.3"})
	if install.Result.Action != ActionFailed || install.Result.ReleaseVersion != "1.2.3" || !install.Result.IsFailure() {
		t.Fatalf("install = %#v", install.Result)
	}
	uninstall := comp.Uninstall(Context{})
	if uninstall.Result.Action != ActionNotInstalled || uninstall.Result.IsFailure() {
		t.Fatalf("uninstall = %#v", uninstall.Result)
	}
	if (Result{Action: ActionSkipped}).IsFailure() {
		t.Fatal("skipped result must not be a failure")
	}
}
