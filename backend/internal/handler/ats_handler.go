package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

// AtsHandler triggers on-demand ATS scans for selected shortlist entries.
type AtsHandler struct {
	ats *service.AtsRunService
}

func NewAtsHandler(a *service.AtsRunService) *AtsHandler {
	return &AtsHandler{ats: a}
}

type atsRunRequest struct {
	ProfileID string   `json:"profileId"`
	IDs       []string `json:"ids"`
}

// Run handles POST /api/shortlist/ats — scores the resume against the JDs of the
// selected shortlist entries and writes each score back.
func (h *AtsHandler) Run(c *gin.Context) {
	var req atsRunRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ProfileID == "" || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profileId and ids are required"})
		return
	}
	results, err := h.ats.Run(c.Request.Context(), req.ProfileID, req.IDs)
	if errors.Is(err, service.ErrNoResume) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// scaffold:inject
