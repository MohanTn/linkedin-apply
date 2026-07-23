package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

// ProfileHandler serves profile + login endpoints.
type ProfileHandler struct {
	profiles *service.ProfileService
	auth     *service.AuthSessionService
}

func NewProfileHandler(p *service.ProfileService, a *service.AuthSessionService) *ProfileHandler {
	return &ProfileHandler{profiles: p, auth: a}
}

// GetProfiles handles GET /api/profiles.
func (h *ProfileHandler) GetProfiles(c *gin.Context) {
	profiles, err := h.profiles.LoadProfiles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

type loginRequest struct {
	Platform string `json:"platform"`
}

// Login handles POST /api/profiles/:id/login. It does NOT perform a browser
// login (that is done once, interactively, via scripts/login.sh) — it reports
// whether a usable session already exists, so selecting a profile can never
// clobber the seeded session or trigger a bot-blocked headless login.
func (h *ProfileHandler) Login(c *gin.Context) {
	id := c.Param("id")
	var req loginRequest
	_ = c.ShouldBindJSON(&req)
	if req.Platform == "" {
		req.Platform = "linkedin"
	}

	status := h.auth.SessionStatus(c.Request.Context(), id, req.Platform)
	c.JSON(http.StatusOK, gin.H{"ok": status == "active", "status": status})
}

// scaffold:inject
