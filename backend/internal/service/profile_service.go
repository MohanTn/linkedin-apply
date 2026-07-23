package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// ProfileService loads profiles from env credentials + JSON search preferences.
type ProfileService struct {
	store   ProfileStore
	dataDir string                        // where data_profileN.json live
	getenv  func(string) string           // injectable for tests
	prefs   map[string]models.SearchPrefs // cache: profileID -> prefs
}

func NewProfileService(store ProfileStore, dataDir string) *ProfileService {
	return &ProfileService{
		store:   store,
		dataDir: dataDir,
		getenv:  os.Getenv,
		prefs:   map[string]models.SearchPrefs{},
	}
}

// LoadProfiles discovers profiles from PROFILE_<N>_* env vars (N = 1,2,3,...),
// loads each data_profileN.json for search preferences, syncs to the store, and
// returns the list. Stops at the first N with no LinkedIn email.
func (s *ProfileService) LoadProfiles(ctx context.Context) ([]models.Profile, error) {
	var out []models.Profile
	for n := 1; ; n++ {
		liEmail := s.getenv(fmt.Sprintf("PROFILE_%d_LINKEDIN_EMAIL", n))
		gdEmail := s.getenv(fmt.Sprintf("PROFILE_%d_GLASSDOOR_EMAIL", n))
		if liEmail == "" && gdEmail == "" {
			break
		}
		id := fmt.Sprintf("profile-%d", n)
		path := filepath.Join(s.dataDir, fmt.Sprintf("data_profile%d.json", n))

		name, prefs := s.loadPrefs(path)
		s.prefs[id] = prefs
		if name == "" {
			name = id
		}

		p := &models.Profile{ID: id, Name: name, LinkedinEmail: liEmail, GlassdoorEmail: gdEmail, ProfileDataPath: path}
		if err := s.store.Upsert(ctx, p); err != nil {
			return nil, fmt.Errorf("sync profile %s: %w", id, err)
		}
		got, err := s.store.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *got)
	}
	return out, nil
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

// GetCredentials returns the email/password for a profile+platform from env.
func (s *ProfileService) GetCredentials(profileID, platform string) (email, password string, err error) {
	n := strings.TrimPrefix(profileID, "profile-")
	plat := strings.ToUpper(platform)
	email = s.getenv(fmt.Sprintf("PROFILE_%s_%s_EMAIL", n, plat))
	password = s.getenv(fmt.Sprintf("PROFILE_%s_%s_PASSWORD", n, plat))
	if email == "" || password == "" {
		return "", "", fmt.Errorf("missing %s credentials for %s", platform, profileID)
	}
	return email, password, nil
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
