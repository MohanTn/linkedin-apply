package service

import (
	"context"
	"errors"
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
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
			s.update(runID, func(rs *RunState) { rs.DiscoveryProgress = p })
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
	return *st, nil
}

func (s *DiscoveryRunService) update(runID string, fn func(*RunState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.runs[runID]; ok {
		fn(st)
		st.UpdatedAt = time.Now()
	}
}
