package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

// DiscoveryHandler triggers and monitors discovery runs.
type DiscoveryHandler struct {
	runs *service.DiscoveryRunService
}

func NewDiscoveryHandler(r *service.DiscoveryRunService) *DiscoveryHandler {
	return &DiscoveryHandler{runs: r}
}

type runRequest struct {
	ProfileID  string   `json:"profileId"`
	Platforms  []string `json:"platforms"`
	SinceHours int      `json:"sinceHours"`
}

// StartRun handles POST /api/discovery/run.
func (h *DiscoveryHandler) StartRun(c *gin.Context) {
	var req runRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ProfileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profileId is required"})
		return
	}
	if len(req.Platforms) == 0 {
		req.Platforms = []string{"linkedin", "glassdoor"}
	}
	if req.SinceHours <= 0 {
		req.SinceHours = 24
	}
	runID := h.runs.StartRun(req.ProfileID, req.Platforms, req.SinceHours)
	c.JSON(http.StatusOK, gin.H{"runId": runID})
}

// GetStatus handles GET /api/discovery/:runId/status.
func (h *DiscoveryHandler) GetStatus(c *gin.Context) {
	st, err := h.runs.GetStatus(c.Param("runId"))
	if errors.Is(err, service.ErrRunNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	c.JSON(http.StatusOK, st)
}

// scaffold:inject
