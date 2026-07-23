// Command server wires the Job Discovery API: profiles, discovery runs, and the
// cockpit shortlist. It never applies to jobs.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mohan/linkedin-apply-backend/internal/browser"
	"github.com/mohan/linkedin-apply-backend/internal/database"
	"github.com/mohan/linkedin-apply-backend/internal/handler"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
	"github.com/mohan/linkedin-apply-backend/internal/service"
)

func main() {
	ctx := context.Background()

	dsn := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/linkedin_apply?sslmode=disable")
	db, err := database.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Repositories
	profileRepo := repository.NewProfileRepo(db)
	jobRepo := repository.NewJobRepo(db)
	verRepo := repository.NewCompanyVerificationRepo(db)
	shortlistRepo := repository.NewShortlistRepo(db)
	sessionRepo := repository.NewSessionRepo(db)
	runRepo := repository.NewDiscoveryRunRepo(db)

	// Browser + services
	headless := env("HEADLESS", "true") != "false"
	br := browser.New(headless)
	profileSvc := service.NewProfileService(profileRepo, env("DATA_DIR", "."))
	authSvc := service.NewAuthSessionService(profileSvc, sessionRepo, br)
	scraperSvc := service.NewJobScraperService(authSvc, br, jobRepo)
	verSvc := service.NewCompanyVerificationService(verRepo, service.NewHTTPProbe())
	discoverySvc := service.NewDiscoveryService(profileSvc, scraperSvc, verSvc, shortlistRepo)
	runSvc := service.NewDiscoveryRunService(discoverySvc, runRepo)

	// Handlers
	profileH := handler.NewProfileHandler(profileSvc, authSvc)
	discoveryH := handler.NewDiscoveryHandler(runSvc)
	shortlistH := handler.NewShortlistHandler(shortlistRepo)

	router := setupRouter(profileH, discoveryH, shortlistH)

	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: router}
	go func() {
		log.Printf("listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	log.Println("shut down")
}

func setupRouter(p *handler.ProfileHandler, d *handler.DiscoveryHandler, s *handler.ShortlistHandler) *gin.Engine {
	r := gin.Default()
	r.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	r.GET("/api/profiles", p.GetProfiles)
	r.POST("/api/profiles/:id/login", p.Login)

	r.POST("/api/discovery/run", d.StartRun)
	r.GET("/api/discovery/:runId/status", d.GetStatus)

	r.GET("/api/shortlist", s.GetShortlist)
	r.PATCH("/api/shortlist/:id", s.UpdateStatus)
	r.GET("/api/shortlist/stats/:profileId", s.GetStats)
	// NOTE: there is deliberately no /api/apply route — applying is manual.
	return r
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
