package api

import (
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

type Release struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Content string `json:"content"`
}

type VersionResponse struct {
	LatestVersion string    `json:"latestVersion"`
	Releases      []Release `json:"releases"`
}

func parseChangelog(content string) []Release {
	var releases []Release
	// Regex to capture version and date: ## [0.1.1] - 2025-12-11
	reHeader := regexp.MustCompile(`^## \[(\d+\.\d+\.\d+)\] - (\d{4}-\d{2}-\d{2})`)

	lines := strings.Split(content, "\n")

	var currentRelease *Release
	var currentContent strings.Builder

	for _, line := range lines {
		// Check for header line
		matches := reHeader.FindStringSubmatch(line)
		if len(matches) == 3 {
			// Save previous release if exists
			if currentRelease != nil {
				currentRelease.Content = strings.TrimSpace(currentContent.String())
				releases = append(releases, *currentRelease)
			}

			// Start new release
			currentRelease = &Release{
				Version: matches[1],
				Date:    matches[2],
			}
			currentContent.Reset()
			continue
		}

		if currentRelease != nil {
			currentContent.WriteString(line + "\n")
		}
	}

	// Add the last one
	if currentRelease != nil {
		currentRelease.Content = strings.TrimSpace(currentContent.String())
		releases = append(releases, *currentRelease)
	}

	return releases
}

func GetVersion(c *gin.Context) {
	// Try multiple locations
	paths := []string{"/app/CHANGELOG.md", "../CHANGELOG.md", "CHANGELOG.md"}
	var content []byte
	var err error

	for _, p := range paths {
		content, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}

	if err != nil {
		c.JSON(http.StatusOK, VersionResponse{
			LatestVersion: "v0.0.0",
			Releases:      []Release{},
		})
		return
	}

	releases := parseChangelog(string(content))
	latest := "v0.0.0"
	if len(releases) > 0 {
		latest = "v" + releases[0].Version
	}

	c.JSON(http.StatusOK, VersionResponse{
		LatestVersion: latest,
		Releases:      releases,
	})
}
