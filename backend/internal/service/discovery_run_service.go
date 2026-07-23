package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// RunState is the observable status of a background discovery run.
type RunState struct {
	RunID     string `json:"runId"`
	ProfileID string `json:"profileId"`
	DiscoveryProgress
	Log       []string  `json:"log,omitempty"` // step-by-step activity feed
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const maxLogLines = 200

// appendLog adds msg to the activity log. Counter updates of the same step
// ("Shortlisting 4/10" -> "Shortlisting 5/10", "Researching Acme…" ->
// "Researching Beta…") share their first word and overwrite the previous line
// instead of flooding the feed; distinct steps append.
func appendLog(log []string, msg string) []string {
	if msg == "" || (len(log) > 0 && log[len(log)-1] == msg) {
		return log
	}
	if len(log) > 0 && firstWord(log[len(log)-1]) == firstWord(msg) {
		log[len(log)-1] = msg
		return log
	}
	log = append(log, msg)
	if len(log) > maxLogLines {
		log = log[len(log)-maxLogLines:]
	}
	return log
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

// ErrRunNotFound is returned by GetStatus for an unknown runID.
var ErrRunNotFound = errors.New("run not found")

// RunStore persists runs to the database.
type RunStore interface {
	Create(ctx context.Context, run *models.DiscoveryRun) error
	MarkComplete(ctx context.Context, runID string) error
}

// DiscoveryRunService runs discovery in the background and tracks progress.
type DiscoveryRunService struct {
	discovery *DiscoveryService
	store     RunStore
	mu        sync.RWMutex
	runs      map[string]*RunState
}

func NewDiscoveryRunService(d *DiscoveryService, s RunStore) *DiscoveryRunService {
	return &DiscoveryRunService{discovery: d, store: s, runs: map[string]*RunState{}}
}

// StartRun launches a discovery run in a goroutine and returns its id.
func (s *DiscoveryRunService) StartRun(profileID string, platforms []string, sinceHours int) string {
	runID := uuid.NewString()
	now := time.Now()
	st := &RunState{RunID: runID, ProfileID: profileID, StartedAt: now, UpdatedAt: now}
	st.Phase = PhaseLogin
	s.mu.Lock()
	s.runs[runID] = st
	s.mu.Unlock()

	go func() {
		// Detach from any request context so the run survives the HTTP call.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		run := &models.DiscoveryRun{ID: runID, ProfileID: profileID, StartedAt: now}
		if err := s.store.Create(ctx, run); err != nil {
			s.update(runID, func(rs *RunState) {
				rs.Phase = PhaseError
				rs.Error = err.Error()
			})
			return
		}

		_, err := s.discovery.DiscoverWithRunID(ctx, profileID, platforms, sinceHours, runID, func(p DiscoveryProgress) {
			s.update(runID, func(rs *RunState) {
				rs.DiscoveryProgress = p
				rs.Log = appendLog(rs.Log, p.Message)
			})
		})
		if err != nil {
			s.update(runID, func(rs *RunState) {
				rs.Phase = PhaseError
				if rs.Error == "" {
					rs.Error = err.Error()
				}
			})
		} else {
			if err := s.store.MarkComplete(ctx, runID); err != nil {
				s.update(runID, func(rs *RunState) { rs.Phase = PhaseError; rs.Error = err.Error() })
			}
		}
	}()
	return runID
}

// GetStatus returns a copy of the run state, or ErrRunNotFound.
func (s *DiscoveryRunService) GetStatus(runID string) (RunState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.runs[runID]
	if !ok {
		return RunState{}, ErrRunNotFound
	}
	cp := *st
	cp.Log = append([]string(nil), st.Log...) // appendLog edits lines in place
	return cp, nil
}

func (s *DiscoveryRunService) update(runID string, fn func(*RunState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.runs[runID]; ok {
		fn(st)
		st.UpdatedAt = time.Now()
	}
}
