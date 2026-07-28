package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ProfileService owns profiles: the env-seeded ones plus everything created in
// the cockpit. It never handles portal passwords — signing in is done by the
// user in a real browser window, and only the session is persisted.
type ProfileService struct {
	store   ProfileStore
	dataDir string                        // where data_profileN.json live
	getenv  func(string) string           // injectable for tests
	prefs   map[string]models.SearchPrefs // cache: profileID -> prefs
	newID   func() string                 // injectable for tests
}

func NewProfileService(store ProfileStore, dataDir string) *ProfileService {
	return &ProfileService{
		store:   store,
		dataDir: dataDir,
		getenv:  os.Getenv,
		prefs:   map[string]models.SearchPrefs{},
		newID:   func() string { return "profile-" + uuid.NewString()[:8] },
	}
}

// ProfileInput is the cockpit's create/update payload. A profile is just a name:
// which portals it is signed in to follows from the sessions it holds.
type ProfileInput struct {
	Name string `json:"name"`
}

// LoadProfiles syncs the PROFILE_<N>_* env-seeded profiles into the store, then
// returns every profile (env-seeded and cockpit-created).
func (s *ProfileService) LoadProfiles(ctx context.Context) ([]models.Profile, error) {
	if err := s.syncEnvProfiles(ctx); err != nil {
		return nil, err
	}
	return s.store.GetAll(ctx)
}

// syncEnvProfiles seeds profile-N for each PROFILE_<N>_* block, stopping at the
// first N with no LinkedIn and no Glassdoor email. Seeding happens once per id:
// afterwards the profile is owned by the database, so renames and deletes made
// in the cockpit are not undone the next time the list is loaded.
func (s *ProfileService) syncEnvProfiles(ctx context.Context) error {
	for n := 1; ; n++ {
		liEmail := s.getenv(fmt.Sprintf("PROFILE_%d_LINKEDIN_EMAIL", n))
		gdEmail := s.getenv(fmt.Sprintf("PROFILE_%d_GLASSDOOR_EMAIL", n))
		if liEmail == "" && gdEmail == "" {
			return nil
		}
		id := fmt.Sprintf("profile-%d", n)
		path := filepath.Join(s.dataDir, fmt.Sprintf("data_profile%d.json", n))

		name, prefs := s.loadPrefs(path)
		s.prefs[id] = prefs
		if name == "" {
			name = id
		}

		p := &models.Profile{
			ID: id, Name: name, LinkedinEmail: liEmail, GlassdoorEmail: gdEmail,
			ProfileDataPath: path, Source: models.ProfileSourceEnv,
		}
		if err := s.store.SeedIfAbsent(ctx, p); err != nil {
			return fmt.Errorf("seed profile %s: %w", id, err)
		}
	}
}

// loadPrefs reads data_profileN.json. A missing/malformed file yields default
// (broad) preferences and an empty name, rather than an error.
func (s *ProfileService) loadPrefs(path string) (string, models.SearchPrefs) {
	def := models.SearchPrefs{MinCompanyScore: 0}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", def
	}
	var pf models.ProfileFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return "", def
	}
	return pf.Name, pf.Search
}

// GetCredentials returns optional PROFILE_<N>_* env credentials for a profile,
// used only to prefill the login form in the browser window. Empty values are
// normal and not an error: the user types their own credentials in that window,
// and nothing is ever persisted.
func (s *ProfileService) GetCredentials(profileID, platform string) (email, password string) {
	n := strings.TrimPrefix(profileID, "profile-")
	plat := strings.ToUpper(platform)
	return s.getenv(fmt.Sprintf("PROFILE_%s_%s_EMAIL", n, plat)),
		s.getenv(fmt.Sprintf("PROFILE_%s_%s_PASSWORD", n, plat))
}

// GetSearchPrefs returns preferences, preferring (in order) the in-memory cache,
// the user-edited prefs stored in the DB, then the data_profileN.json seed.
func (s *ProfileService) GetSearchPrefs(ctx context.Context, profileID string) (models.SearchPrefs, error) {
	if p, ok := s.prefs[profileID]; ok {
		return p, nil
	}
	if raw, err := s.store.GetPrefs(ctx, profileID); err == nil && len(raw) > 0 {
		var prefs models.SearchPrefs
		if json.Unmarshal(raw, &prefs) == nil {
			s.prefs[profileID] = prefs
			return prefs, nil
		}
	}
	prof, err := s.store.GetByID(ctx, profileID)
	if err != nil {
		return models.SearchPrefs{}, err
	}
	_, prefs := s.loadPrefs(prof.ProfileDataPath)
	s.prefs[profileID] = prefs
	return prefs, nil
}

// SavePrefs persists user-edited search preferences and refreshes the cache, so
// the next discovery run uses them immediately.
func (s *ProfileService) SavePrefs(ctx context.Context, profileID string, prefs models.SearchPrefs) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err := s.store.SetPrefs(ctx, profileID, raw); err != nil {
		return err
	}
	s.prefs[profileID] = prefs
	return nil
}

// CreateProfile stores a new cockpit-managed profile. Portals are connected
// afterwards by signing in, which is what creates the session.
func (s *ProfileService) CreateProfile(ctx context.Context, in ProfileInput) (*models.Profile, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("profile name is required")
	}
	p := &models.Profile{ID: s.newID(), Name: name, Source: models.ProfileSourceDB}
	if err := s.store.Upsert(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateProfile renames a profile. Every profile is editable, including ones
// originally seeded from the environment.
func (s *ProfileService) UpdateProfile(ctx context.Context, id string, in ProfileInput) (*models.Profile, error) {
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		existing.Name = name
	}
	if err := s.store.Upsert(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteProfile removes any profile and its dependent rows. Env-seeded profiles
// can be deleted too: the store keeps a tombstone so they are not re-seeded.
func (s *ProfileService) DeleteProfile(ctx context.Context, id string) error {
	if _, err := s.store.GetByID(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	delete(s.prefs, id)
	return nil
}

// scaffold:inject
