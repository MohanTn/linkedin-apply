package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/models"
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

// SignIn handles POST /api/profiles/:id/signin. Unlike Login (a passive status
// check), this actively launches the browser to authenticate and persists the
// resulting session cookies. In headful mode (HEADLESS=false) a real window
// opens so the user can complete any 2FA/CAPTCHA; the request blocks until a
// terminal login state is reached.
func (h *ProfileHandler) SignIn(c *gin.Context) {
	id := c.Param("id")
	var req loginRequest
	_ = c.ShouldBindJSON(&req)
	if req.Platform == "" {
		req.Platform = "linkedin"
	}

	sess, err := h.auth.Login(c.Request.Context(), id, req.Platform)
	switch {
	case errors.Is(err, service.ErrNeeds2FA):
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": "needs_2fa"})
	case errors.Is(err, service.ErrInvalidCreds):
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": "invalid_creds"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusOK, gin.H{"ok": sess.Status == "active", "status": sess.Status})
	}
}

// GetPrefs handles GET /api/profiles/:id/prefs.
func (h *ProfileHandler) GetPrefs(c *gin.Context) {
	prefs, err := h.profiles.GetSearchPrefs(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, prefs)
}

// UpdatePrefs handles PUT /api/profiles/:id/prefs.
func (h *ProfileHandler) UpdatePrefs(c *gin.Context) {
	var prefs models.SearchPrefs
	if err := c.ShouldBindJSON(&prefs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preferences"})
		return
	}
	if err := h.profiles.SavePrefs(c.Request.Context(), c.Param("id"), prefs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, prefs)
}

// scaffold:inject
