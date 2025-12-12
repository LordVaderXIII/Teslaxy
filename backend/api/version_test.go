package api

import (
	"testing"
)

func TestParseChangelog(t *testing.T) {
	raw := `# Changelog

All notable changes to this project will be documented in this file.

## [0.1.1] - 2025-12-11
### Added
- New favicon.

## [0.1.0] - 2025-12-10
### Added
- Initial version.
`
	releases := parseChangelog(raw)

	if len(releases) != 2 {
		t.Fatalf("Expected 2 releases, got %d", len(releases))
	}

	if releases[0].Version != "0.1.1" {
		t.Errorf("Expected version 0.1.1, got %s", releases[0].Version)
	}
	if releases[0].Date != "2025-12-11" {
		t.Errorf("Expected date 2025-12-11, got %s", releases[0].Date)
	}

	expectedContent := "### Added\n- New favicon."
	if releases[0].Content != expectedContent {
		t.Errorf("Expected content %q, got %q", expectedContent, releases[0].Content)
	}

	if releases[1].Version != "0.1.0" {
		t.Errorf("Expected version 0.1.0, got %s", releases[1].Version)
	}
}
