package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
	"github.com/mohan/linkedin-apply-backend/internal/models"
)

// minResumeChars: below this the extraction produced no usable text.
const minResumeChars = 40

var (
	ErrUnsupportedResume = errors.New("unsupported resume type (upload a PDF, .txt or .md)")
	ErrNoResumeText      = errors.New("no text could be extracted — export a text-based PDF")
)

// ResumeService handles resume upload (text extraction) and retrieval.
type ResumeService struct {
	store ResumeStore
}

func NewResumeService(store ResumeStore) *ResumeService {
	return &ResumeService{store: store}
}

// Upload extracts plain text from the file, then stores bytes + text as the
// profile's active resume (replacing any previous one).
func (s *ResumeService) Upload(ctx context.Context, profileID, filename string, content []byte) (*models.Resume, error) {
	text, err := extractText(filename, content)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(text)) < minResumeChars {
		return nil, ErrNoResumeText
	}
	res := &models.Resume{
		ID:        uuid.NewString(),
		ProfileID: profileID,
		Filename:  filename,
		Content:   content,
		Text:      text,
	}
	if err := s.store.Upsert(ctx, res); err != nil {
		return nil, err
	}
	return res, nil
}

// Get returns the profile's resume, or ErrNotFound via the store.
func (s *ResumeService) Get(ctx context.Context, profileID string) (*models.Resume, error) {
	return s.store.GetByProfile(ctx, profileID)
}

// extractText pulls plain text from a PDF (via ledongthuc/pdf) or treats the
// bytes as UTF-8 text for .txt/.md.
func extractText(filename string, content []byte) (string, error) {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		return extractPDF(content)
	case strings.HasSuffix(lower, ".txt"), strings.HasSuffix(lower, ".md"):
		return string(content), nil
	default:
		return "", ErrUnsupportedResume
	}
}

func extractPDF(content []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("read pdf: %w", err)
	}
	plain, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract pdf text: %w", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, plain); err != nil {
		return "", err
	}
	return buf.String(), nil
}
