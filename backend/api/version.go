package api

import (
	"net/http"
	"os"
	"regexp"

	"github.com/gin-gonic/gin"
)

type VersionResponse struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
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
			Version:   "v0.0.0",
			Changelog: "Changelog not found.",
		})
		return
	}

	changelog := string(content)
	version := "v0.0.0"

	// Parse version from the first header like "## [0.1.0]"
	// Regex to capture version in "## [X.Y.Z]"
	re := regexp.MustCompile(`## \[(\d+\.\d+\.\d+)\]`)
	matches := re.FindStringSubmatch(changelog)
	if len(matches) > 1 {
		version = "v" + matches[1]
	}

	c.JSON(http.StatusOK, VersionResponse{
		Version:   version,
		Changelog: changelog,
	})
}
