package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// atsConcurrency bounds parallel JD fetches. Each fetch launches a headless
// browser, so this trades wall-time against the cost of N concurrent Chromes.
const atsConcurrency = 4

// ErrNoResume is returned when an ATS run is requested for a profile that has no
// resume uploaded.
var ErrNoResume = errors.New("no resume uploaded for this profile")

// AtsMatch is the per-entry outcome of an on-demand ATS run.
type AtsMatch struct {
	EntryID string `json:"entryId"`
	Score   int    `json:"score"`
	Scored  bool   `json:"scored"` // false when the JD could not be fetched/scored
}

// AtsRunService scores the resume against the job descriptions of a user-chosen
// set of shortlist entries, on demand (discovery no longer does this).
type AtsRunService struct {
	shortlist ShortlistEntryStore
	jobs      JobReader
	resumes   ResumeStore
	jd        JDFetcher
}

func NewAtsRunService(shortlist ShortlistEntryStore, jobs JobReader, resumes ResumeStore, jd JDFetcher) *AtsRunService {
	return &AtsRunService{shortlist: shortlist, jobs: jobs, resumes: resumes, jd: jd}
}

// Run scores the given shortlist entries concurrently and writes each result
// back. It returns one AtsMatch per entry. A missing resume is a hard error; a
// single unfetchable JD just yields Scored=false for that entry.
func (s *AtsRunService) Run(ctx context.Context, profileID string, entryIDs []string) ([]AtsMatch, error) {
	resume, err := s.resumes.GetByProfile(ctx, profileID)
	if err != nil || resume == nil {
		return nil, ErrNoResume
	}

	results := make([]AtsMatch, len(entryIDs))
	sem := make(chan struct{}, atsConcurrency)
	var wg sync.WaitGroup
	for i, id := range entryIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = s.scoreOne(ctx, profileID, resume.Text, id)
		}(i, id)
	}
	wg.Wait()
	return results, nil
}

func (s *AtsRunService) scoreOne(ctx context.Context, profileID, resumeText, entryID string) AtsMatch {
	out := AtsMatch{EntryID: entryID}
	entry, err := s.shortlist.Get(ctx, entryID)
	if err != nil || entry.ProfileID != profileID {
		return out // unknown or foreign entry -> unscored
	}
	job, err := s.jobs.GetByID(ctx, entry.JobID)
	if err != nil {
		return out
	}
	jd, _ := s.jd.FetchJD(ctx, profileID, *job)
	res := ScoreResume(resumeText, jd)
	if len(res.Matched)+len(res.Missing) == 0 {
		return out // JD unfetchable / too short
	}
	details, _ := json.Marshal(res)
	if err := s.shortlist.UpdateAts(ctx, entryID, res.Score, details); err != nil {
		return out
	}
	out.Score = res.Score
	out.Scored = true
	return out
}
