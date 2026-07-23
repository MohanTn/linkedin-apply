package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

// ShortlistReader is the store surface the cockpit needs.
type ShortlistReader interface {
	List(ctx context.Context, f repository.ShortlistFilter) ([]models.ShortlistRow, error)
	UpdateStatus(ctx context.Context, id, status string) (*models.ShortlistEntry, error)
	Stats(ctx context.Context, profileID string) (map[string]int, error)
}

// ShortlistHandler serves the curated shortlist (no apply).
type ShortlistHandler struct {
	store ShortlistReader
}

func NewShortlistHandler(s ShortlistReader) *ShortlistHandler {
	return &ShortlistHandler{store: s}
}

// GetShortlist handles GET /api/shortlist.
func (h *ShortlistHandler) GetShortlist(c *gin.Context) {
	profileID := c.Query("profileId")
	if profileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profileId is required"})
		return
	}
	f := repository.ShortlistFilter{
		ProfileID:    profileID,
		Status:       c.Query("status"),
		IncludeGhost: c.Query("includeGhost") == "true",
		MinScore:     atoiDefault(c.Query("minScore"), 0),
		Limit:        atoiDefault(c.Query("limit"), 50),
		Offset:       (atoiDefault(c.Query("page"), 1) - 1) * atoiDefault(c.Query("limit"), 50),
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	items, err := h.store.List(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = []models.ShortlistRow{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

type statusRequest struct {
	Status string `json:"status"`
}

// UpdateStatus handles PATCH /api/shortlist/:id.
func (h *ShortlistHandler) UpdateStatus(c *gin.Context) {
	var req statusRequest
	if err := c.ShouldBindJSON(&req); err != nil || !models.IsValidStatus(req.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	entry, err := h.store.UpdateStatus(c.Request.Context(), c.Param("id"), req.Status)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": entry.ID, "status": entry.Status})
}

// GetStats handles GET /api/shortlist/stats/:profileId.
func (h *ShortlistHandler) GetStats(c *gin.Context) {
	stats, err := h.store.Stats(c.Request.Context(), c.Param("profileId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// scaffold:inject
