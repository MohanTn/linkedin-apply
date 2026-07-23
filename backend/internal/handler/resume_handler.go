package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

const maxResumeBytes = 5 << 20 // 5MB

// ResumeHandler serves resume upload/retrieval per profile.
type ResumeHandler struct {
	resumes *service.ResumeService
}

func NewResumeHandler(r *service.ResumeService) *ResumeHandler {
	return &ResumeHandler{resumes: r}
}

// Upload handles POST /api/profiles/:id/resume (multipart form field "file").
func (h *ResumeHandler) Upload(c *gin.Context) {
	id := c.Param("id")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	if fileHeader.Size > maxResumeBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "resume exceeds 5MB"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read file"})
		return
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, maxResumeBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read file"})
		return
	}

	res, err := h.resumes.Upload(c.Request.Context(), id, fileHeader.Filename, content)
	if err != nil {
		if errors.Is(err, service.ErrUnsupportedResume) || errors.Is(err, service.ErrNoResumeText) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"filename":   res.Filename,
		"uploadedAt": res.UploadedAt,
		"textLen":    len(res.Text),
	})
}

// Get handles GET /api/profiles/:id/resume.
func (h *ResumeHandler) Get(c *gin.Context) {
	res, err := h.resumes.Get(c.Request.Context(), c.Param("id"))
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no resume uploaded"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"filename":   res.Filename,
		"uploadedAt": res.UploadedAt,
		"textLen":    len(res.Text),
	})
}

// scaffold:inject
