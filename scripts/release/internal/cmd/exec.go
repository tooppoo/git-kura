package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

// supportedPlanSchemaVersions lists the plan schemaVersions exec can process.
var supportedPlanSchemaVersions = map[string]bool{
	schema.PlanSchemaVersion: true,
}

// ExecRelease runs the exec command logic. Exposed for testing.
func ExecRelease(registry *step.Registry, version, stepName string) error {
	return runExec(registry, version, stepName, step.Options{})
}

// ExecReleaseWithOptions runs exec with step-specific command-line options.
func ExecReleaseWithOptions(registry *step.Registry, version, stepName string, options step.Options) error {
	return runExec(registry, version, stepName, options)
}

func newExecCommand(registry *step.Registry) *cobra.Command {
	var version, stepName, bucket string

	c := &cobra.Command{
		Use:   "exec",
		Short: "Execute a validated release plan",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runExec(registry, version, stepName, step.Options{Bucket: bucket})
		},
	}
	c.Flags().StringVar(&version, "version", "", "Target version (vMAJOR.MINOR.PATCH)")
	c.Flags().StringVar(&stepName, "step", "", "Release step name")
	c.Flags().StringVar(&bucket, "bucket", "", "Scoop bucket repository root path (used by --step scoop)")
	_ = c.MarkFlagRequired("version")
	_ = c.MarkFlagRequired("step")
	return c
}

func runExec(registry *step.Registry, version, stepName string, options step.Options) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	s, h, err := parseStep(registry, stepName)
	if err != nil {
		return err
	}
	configureHandler(h, options)

	var plan schema.ReleasePlanEnvelope
	if err := readJSON(planFilePath(version, string(s)), &plan); err != nil {
		return fmt.Errorf("load plan: %w", err)
	}

	var result schema.ValidateResult
	if err := readJSON(validateResultPath(version, string(s)), &result); err != nil {
		return fmt.Errorf("load validate result (run validate first): %w", err)
	}

	if err := safetyGate(&plan, &result, version, string(s)); err != nil {
		return err
	}

	if rp, ok := h.(step.ResultPreflighter); ok {
		if err := rp.PreflightWithResult(&plan, &result); err != nil {
			return fmt.Errorf("preflight check failed: %w", err)
		}
	} else if err := h.Preflight(&plan); err != nil {
		return fmt.Errorf("preflight check failed: %w", err)
	}

	if err := h.Exec(&plan); err != nil {
		return fmt.Errorf("exec step %q: %w", s, err)
	}

	fmt.Printf("exec completed: %s %s\n", version, string(s))
	return nil
}

// safetyGate checks that the validate result matches the plan and is successful.
// Any mismatch or non-success status causes exec to fail immediately.
func safetyGate(plan *schema.ReleasePlanEnvelope, result *schema.ValidateResult, version, stepName string) error {
	if !supportedPlanSchemaVersions[plan.SchemaVersion] {
		return fmt.Errorf("unsupported plan schemaVersion %q", plan.SchemaVersion)
	}

	// Re-compute payloadHash to detect payload tampering since plan was generated.
	computed, err := computePayloadHash(plan.Payload)
	if err != nil {
		return fmt.Errorf("compute payload hash: %w", err)
	}
	if computed != plan.PayloadHash {
		return fmt.Errorf("plan payload was modified after the plan was generated (re-run plan and validate)")
	}

	if result.SchemaVersion != schema.ValidateSchemaVersion {
		return fmt.Errorf("unsupported validate result schemaVersion %q", result.SchemaVersion)
	}
	if result.Kind != schema.ValidateKind {
		return fmt.Errorf("unexpected validate result kind %q, want %q", result.Kind, schema.ValidateKind)
	}
	if result.Status != schema.ValidateStatusSuccess {
		return fmt.Errorf("validate result status is %q, not %q: re-run validate after fixing errors", result.Status, schema.ValidateStatusSuccess)
	}
	// Verify that the payload's targetVersion and stepName match the CLI args so
	// that a tampered payload with a recomputed hash cannot bypass the gate.
	if plan.Payload.TargetVersion != version {
		return fmt.Errorf("plan payload targetVersion %q does not match requested version %q", plan.Payload.TargetVersion, version)
	}
	if plan.Payload.StepName != stepName {
		return fmt.Errorf("plan payload stepName %q does not match requested step %q", plan.Payload.StepName, stepName)
	}
	if result.TargetVersion != version {
		return fmt.Errorf("version mismatch: validate result has %q, expected %q", result.TargetVersion, version)
	}
	if result.StepName != stepName {
		return fmt.Errorf("step mismatch: validate result has %q, expected %q", result.StepName, stepName)
	}
	// Cross-check payload against result so the result cannot claim a different
	// version or step than what the plan payload actually specifies.
	if result.TargetVersion != plan.Payload.TargetVersion {
		return fmt.Errorf("validate result targetVersion %q does not match plan payload targetVersion %q", result.TargetVersion, plan.Payload.TargetVersion)
	}
	if result.StepName != plan.Payload.StepName {
		return fmt.Errorf("validate result stepName %q does not match plan payload stepName %q", result.StepName, plan.Payload.StepName)
	}
	if result.PlanID != plan.PlanID {
		return fmt.Errorf("planId mismatch: validate result was for plan %q but current plan is %q (re-run validate)", result.PlanID, plan.PlanID)
	}
	if result.PayloadHash != plan.PayloadHash {
		return fmt.Errorf("payloadHash mismatch: plan was modified after validation (re-run validate)")
	}
	return nil
}
