package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

func newValidateCommand(registry *step.Registry) *cobra.Command {
	var version, stepName, tap string

	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate the release plan and write a machine-readable result file",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runValidate(registry, version, stepName, step.Options{Tap: tap})
		},
	}
	c.Flags().StringVar(&version, "version", "", "Target version (vMAJOR.MINOR.PATCH)")
	c.Flags().StringVar(&stepName, "step", "", stepFlagUsage())
	c.Flags().StringVar(&tap, "tap", "", "Homebrew tap repository root path (used by --step homebrew)")
	_ = c.MarkFlagRequired("version")
	_ = c.MarkFlagRequired("step")
	return c
}

func runValidate(registry *step.Registry, version, stepName string, options step.Options) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	s, h, err := parseStep(registry, stepName)
	if err != nil {
		return err
	}
	configureHandler(h, options)

	planPath := planFilePath(version, string(s))
	var plan schema.ReleasePlanEnvelope
	if err := readJSON(planPath, &plan); err != nil {
		return fmt.Errorf("load plan: %w", err)
	}

	// Re-compute payloadHash to detect payload tampering before validation.
	computed, err := computePayloadHash(plan.Payload)
	if err != nil {
		return fmt.Errorf("compute payload hash: %w", err)
	}
	if computed != plan.PayloadHash {
		return fmt.Errorf("plan payloadHash mismatch: payload may have been modified (stored: %s, computed: %s)", plan.PayloadHash, computed)
	}

	var errs, warnings []string
	var stepData json.RawMessage
	var internalErr error

	// Use ValidateWithData if the handler implements DetailedValidator so that
	// per-asset results are recorded in the result file's stepData field.
	if dv, ok := h.(step.DetailedValidator); ok {
		errs, warnings, stepData, internalErr = dv.ValidateWithData(&plan)
	} else {
		errs, warnings, internalErr = h.Validate(&plan)
	}

	status := schema.ValidateStatusSuccess
	if internalErr != nil {
		return fmt.Errorf("validate step %q: %w", s, internalErr)
	}
	if len(errs) > 0 {
		status = schema.ValidateStatusFailure
	}

	result := schema.ValidateResult{
		SchemaVersion: schema.ValidateSchemaVersion,
		Kind:          schema.ValidateKind,
		TargetVersion: plan.Payload.TargetVersion,
		StepName:      plan.Payload.StepName,
		Status:        status,
		Errors:        nullSafe(errs),
		Warnings:      nullSafe(warnings),
		PlanID:        plan.PlanID,
		PayloadHash:   plan.PayloadHash,
		StepData:      stepData,
	}

	outPath := validateResultPath(version, string(s))
	if err := writeJSON(outPath, result); err != nil {
		return err
	}

	fmt.Printf("validate result written: %s\n", outPath)

	if status == schema.ValidateStatusFailure {
		return fmt.Errorf("validation failed: %d error(s)", len(errs))
	}
	return nil
}

func nullSafe(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}
