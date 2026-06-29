package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

func newPlanCommand(registry *step.Registry) *cobra.Command {
	var version, stepName, bucket string

	c := &cobra.Command{
		Use:   "plan",
		Short: "Generate a release plan file for a given step",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPlan(registry, version, stepName, step.Options{Bucket: bucket})
		},
	}
	c.Flags().StringVar(&version, "version", "", "Target version (vMAJOR.MINOR.PATCH)")
	c.Flags().StringVar(&stepName, "step", "", stepFlagUsage())
	c.Flags().StringVar(&bucket, "bucket", "", "Scoop bucket repository root path (used by --step scoop)")
	_ = c.MarkFlagRequired("version")
	_ = c.MarkFlagRequired("step")
	return c
}

func runPlan(registry *step.Registry, version, stepName string, options step.Options) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	s, h, err := parseStep(registry, stepName)
	if err != nil {
		return err
	}
	configureHandler(h, options)

	stepData, err := h.BuildPayload(version)
	if err != nil {
		return fmt.Errorf("build payload for step %q: %w", s, err)
	}

	payload := schema.ReleasePlanPayload{
		TargetVersion: version,
		StepName:      string(s),
		StepData:      stepData,
	}

	payloadHash, err := computePayloadHash(payload)
	if err != nil {
		return err
	}

	planID, err := newPlanID()
	if err != nil {
		return err
	}

	envelope := schema.ReleasePlanEnvelope{
		SchemaVersion: schema.PlanSchemaVersion,
		Kind:          schema.PlanKind,
		PlanID:        planID,
		PayloadHash:   payloadHash,
		GeneratedAt:   nowISO8601(),
		Payload:       payload,
	}

	outPath := planFilePath(version, string(s))
	if err := writeJSON(outPath, envelope); err != nil {
		return err
	}

	fmt.Printf("plan written: %s\n", outPath)
	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
