package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mohan/linkedin-apply-backend/internal/models"
	"github.com/mohan/linkedin-apply-backend/internal/repository"
)

func newCRUDService(t *testing.T) (*ProfileService, *fakeProfileStore) {
	t.Helper()
	pstore := newFakeProfileStore()
	svc := NewProfileService(pstore, t.TempDir())
	svc.getenv = func(string) string { return "" } // no env-seeded profiles
	return svc, pstore
}

func TestProfileService_CreateProfile(t *testing.T) {
	svc, _ := newCRUDService(t)

	p, err := svc.CreateProfile(context.Background(), ProfileInput{Name: "Mohan"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != models.ProfileSourceDB || p.Name != "Mohan" {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileService_CreateRequiresName(t *testing.T) {
	svc, _ := newCRUDService(t)
	if _, err := svc.CreateProfile(context.Background(), ProfileInput{Name: "  "}); err == nil {
		t.Fatal("expected an error for a blank name")
	}
}

func TestProfileService_RenameProfile(t *testing.T) {
	svc, _ := newCRUDService(t)
	ctx := context.Background()

	p, _ := svc.CreateProfile(ctx, ProfileInput{Name: "Mohan"})
	got, err := svc.UpdateProfile(ctx, p.ID, ProfileInput{Name: "Mohan T"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Mohan T" {
		t.Fatalf("name=%q, want Mohan T", got.Name)
	}
}

func TestProfileService_DeleteRemovesProfile(t *testing.T) {
	svc, pstore := newCRUDService(t)
	ctx := context.Background()

	p, _ := svc.CreateProfile(ctx, ProfileInput{Name: "Temp"})
	if err := svc.DeleteProfile(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := pstore.m[p.ID]; ok {
		t.Fatal("profile was not deleted")
	}
}

func TestProfileService_DeleteUnknownProfile(t *testing.T) {
	svc, _ := newCRUDService(t)
	if err := svc.DeleteProfile(context.Background(), "nope"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

// envService wires a service whose PROFILE_1_* block seeds one env profile.
func envService(t *testing.T) (*ProfileService, *fakeProfileStore) {
	t.Helper()
	pstore := newFakeProfileStore()
	svc := NewProfileService(pstore, t.TempDir())
	env := map[string]string{
		"PROFILE_1_LINKEDIN_EMAIL":    "env@x.com",
		"PROFILE_1_LINKEDIN_PASSWORD": "envpw",
	}
	svc.getenv = func(k string) string { return env[k] }
	return svc, pstore
}

func TestProfileService_EnvProfileIsEditable(t *testing.T) {
	svc, _ := envService(t)
	ctx := context.Background()
	if _, err := svc.LoadProfiles(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateProfile(ctx, "profile-1", ProfileInput{Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	// A later load must not overwrite the edit with the env values again.
	all, err := svc.LoadProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Name != "Renamed" {
		t.Fatalf("reload clobbered the edit: %+v", all)
	}
}

func TestProfileService_DeletedEnvProfileStaysDeleted(t *testing.T) {
	svc, _ := envService(t)
	ctx := context.Background()
	if _, err := svc.LoadProfiles(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProfile(ctx, "profile-1"); err != nil {
		t.Fatalf("env profile must be deletable: %v", err)
	}

	// The env vars are still set; the profile must not come back.
	all, err := svc.LoadProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("deleted env profile reappeared: %+v", all)
	}
}

// --- sessions ---------------------------------------------------------------

func newAuthService(t *testing.T) (*AuthSessionService, *fakeSessionStore) {
	t.Helper()
	svc, _ := newCRUDService(t)
	sessions := newFakeSessionStore()
	return NewAuthSessionService(svc, sessions, &fakeBrowser{}), sessions
}

func TestAuthSessionService_SessionsListsEveryLoginPlatform(t *testing.T) {
	auth, _ := newAuthService(t)

	got := auth.Sessions(context.Background(), "profile-1")
	if len(got) != len(LoginPlatforms) {
		t.Fatalf("got %d platforms, want %d", len(got), len(LoginPlatforms))
	}
	for _, ps := range got {
		if ps.Status != "none" {
			t.Fatalf("%s status=%q, want none", ps.Platform, ps.Status)
		}
		// No session means no expiry at all, not a zero date.
		if ps.ExpiresAt != nil {
			t.Fatalf("%s has an expiry without a session: %v", ps.Platform, ps.ExpiresAt)
		}
	}
}

func TestAuthSessionService_SessionsReportsActiveAndExpiry(t *testing.T) {
	auth, sessions := newAuthService(t)
	expires := time.Now().Add(48 * time.Hour)
	_ = sessions.Upsert(context.Background(), &models.BrowserSession{
		ID: "s1", ProfileID: "profile-1", Platform: "linkedin",
		Status: models.SessionActive, Cookies: []byte(`[{"name":"li_at"}]`), ExpiresAt: expires,
	})

	for _, ps := range auth.Sessions(context.Background(), "profile-1") {
		if ps.Platform != "linkedin" {
			continue
		}
		if ps.Status != models.SessionActive {
			t.Fatalf("status=%q, want active", ps.Status)
		}
		if ps.ExpiresAt == nil || !ps.ExpiresAt.Equal(expires) {
			t.Fatalf("expiresAt=%v, want %v", ps.ExpiresAt, expires)
		}
		return
	}
	t.Fatal("linkedin session missing")
}

// A stored session past its expiry must not be advertised as usable.
func TestAuthSessionService_LapsedSessionReportsExpired(t *testing.T) {
	auth, sessions := newAuthService(t)
	_ = sessions.Upsert(context.Background(), &models.BrowserSession{
		ID: "s1", ProfileID: "profile-1", Platform: "linkedin",
		Status: models.SessionActive, Cookies: []byte(`[{"name":"li_at"}]`),
		ExpiresAt: time.Now().Add(-time.Hour),
	})

	for _, ps := range auth.Sessions(context.Background(), "profile-1") {
		if ps.Platform == "linkedin" && ps.Status != models.SessionExpired {
			t.Fatalf("status=%q, want expired", ps.Status)
		}
	}
}

// Signing in must persist the session with an expiry, with no credentials involved.
func TestAuthSessionService_LoginStoresSessionWithExpiry(t *testing.T) {
	auth, sessions := newAuthService(t)
	ctx := context.Background()

	sess, err := auth.Login(ctx, "profile-1", "linkedin")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != models.SessionActive || len(sess.Cookies) == 0 {
		t.Fatalf("unexpected session: %+v", sess)
	}
	stored, err := sessions.Get(ctx, "profile-1", "linkedin")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.ExpiresAt.After(time.Now()) {
		t.Fatalf("stored session has no future expiry: %v", stored.ExpiresAt)
	}
}
