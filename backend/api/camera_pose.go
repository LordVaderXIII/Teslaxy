package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"teslaxy/services"
)

// GetCameraPoses returns the generation rig the 3D stitch projects with.
// Intrinsics stay in the frontend. This route is pose only.
func GetCameraPoses(c *gin.Context) {
	c.JSON(http.StatusOK, services.GenerationCameraPoses())
}
