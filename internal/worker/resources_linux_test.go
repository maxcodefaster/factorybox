//go:build linux

package worker

import "testing"

func TestWorkerBudgetIsNotTheSingleSandboxQualificationCeiling(t *testing.T) {
	for _, row := range []struct {
		name, memory, cpu string
	}{
		{"existing qualification fixture", "3221225472\n", "300000 100000\n"},
		{"shared worker for multiple bounded sandboxes", "12884901888\n", "800000 100000\n"},
		{"CPU sharing above the fixture ceiling", "3221225472\n", "800000 100000\n"},
		{"small owned fixture", "134217728\n", "50000 100000\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if err := validateWorkerResourceLimits([]byte(row.memory), []byte(row.cpu)); err != nil {
				t.Fatal("finite aggregate worker budget was confused with one sandbox's test ceiling:", err)
			}
		})
	}
}

func TestWorkerStillRequiresFinitePositiveResourceLimits(t *testing.T) {
	for _, row := range []struct {
		name, memory, cpu string
	}{
		{"unbounded memory", "max", "300000 100000"},
		{"zero memory", "0", "300000 100000"},
		{"negative memory", "-1", "300000 100000"},
		{"overflow memory", "18446744073709551616", "300000 100000"},
		{"signed memory overflow", "18446744073709551615", "300000 100000"},
		{"unbounded CPU", "3221225472", "max 100000"},
		{"zero quota", "3221225472", "0 100000"},
		{"negative quota", "3221225472", "-1 100000"},
		{"zero period", "3221225472", "300000 0"},
		{"missing period", "3221225472", "300000"},
		{"extra CPU field", "3221225472", "300000 100000 1"},
		{"overflow quota", "3221225472", "18446744073709551616 100000"},
		{"signed quota overflow", "3221225472", "18446744073709551615 100000"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if err := validateWorkerResourceLimits([]byte(row.memory), []byte(row.cpu)); err == nil {
				t.Fatal("unbounded or malformed worker resource limit was accepted")
			}
		})
	}
}
