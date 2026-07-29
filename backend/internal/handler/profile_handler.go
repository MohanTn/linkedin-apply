package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
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

// GetProfiles handles GET /api/profiles, including each profile's portal logins
// and their current session status.
func (h *ProfileHandler) GetProfiles(c *gin.Context) {
	profiles, err := h.profiles.LoadProfiles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for i := range profiles {
		h.attachSessionStatus(c, &profiles[i])
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
	viewer := browser.ViewerURL()
	switch {
	case errors.Is(err, service.ErrNeeds2FA):
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": "needs_2fa", "viewerUrl": viewer})
	case errors.Is(err, service.ErrInvalidCreds):
		c.JSON(http.StatusOK, gin.H{"ok": false, "status": "invalid_creds", "viewerUrl": viewer})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "viewerUrl": viewer})
	default:
		c.JSON(http.StatusOK, gin.H{"ok": sess.Status == "active", "status": sess.Status, "viewerUrl": viewer})
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

// Create handles POST /api/profiles: a new cockpit-managed profile with its
// portal credentials. Passwords are encrypted before they reach the database.
func (h *ProfileHandler) Create(c *gin.Context) {
	var in service.ProfileInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile"})
		return
	}
	p, err := h.profiles.CreateProfile(c.Request.Context(), in)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.attachSessionStatus(c, p)
	c.JSON(http.StatusCreated, p)
}

// Update handles PUT /api/profiles/:id.
func (h *ProfileHandler) Update(c *gin.Context) {
	var in service.ProfileInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile"})
		return
	}
	p, err := h.profiles.UpdateProfile(c.Request.Context(), c.Param("id"), in)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
	case err != nil:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		h.attachSessionStatus(c, p)
		c.JSON(http.StatusOK, p)
	}
}

// Delete handles DELETE /api/profiles/:id, removing the profile along with its
// credentials, sessions, resume, and shortlist.
func (h *ProfileHandler) Delete(c *gin.Context) {
	err := h.profiles.DeleteProfile(c.Request.Context(), c.Param("id"))
	switch {
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
	case err != nil:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
	}
}

// attachSessionStatus fills in the profile's per-platform session state so the
// cockpit can show what is connected and when it expires.
func (h *ProfileHandler) attachSessionStatus(c *gin.Context, p *models.Profile) {
	p.Sessions = h.auth.Sessions(c.Request.Context(), p.ID)
}

// SignInViewer handles GET /api/signin-viewer: where to watch and complete a
// sign-in. The cockpit shows this as a link the moment Sign in is clicked,
// because the window lives in the container, not on the user's desktop.
func (h *ProfileHandler) SignInViewer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"viewerUrl": browser.ViewerURL()})
}

// scaffold:inject
