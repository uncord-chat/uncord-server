package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Sentinel errors for document loading.
var (
	ErrManifestInvalid  = errors.New("manifest.json is malformed")
	ErrDocumentNotFound = errors.New("document HTML file not found")
	ErrDuplicateSlug    = errors.New("duplicate document slug")
	ErrInvalidSlug      = errors.New("invalid document slug")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Document holds a single onboarding document loaded from the filesystem. The Content field contains sanitised HTML.
type Document struct {
	Slug     string
	Title    string
	Content  string
	Position int
	Required bool
}

// manifestFile is the JSON structure read from manifest.json.
type manifestFile struct {
	Documents []manifestEntry `json:"documents"`
}

type manifestEntry struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	File     string `json:"file"`
	Position int    `json:"position"`
	Required bool   `json:"required"`
}

// DocumentStore holds onboarding documents loaded from the filesystem. It is immutable after construction and safe for
// concurrent use.
type DocumentStore struct {
	docs          []Document
	requiredSlugs map[string]struct{}
}

// EmptyDocumentStore returns a store with no documents. Used when DATA_DIR is unset.
func EmptyDocumentStore() *DocumentStore {
	return &DocumentStore{
		docs:          nil,
		requiredSlugs: make(map[string]struct{}),
	}
}

// LoadDocuments reads manifest.json from dir, loads each referenced HTML file, sanitises the content, and returns an
// immutable DocumentStore. If manifest.json does not exist, an empty store is returned (valid state for servers without
// onboarding documents). A malformed manifest is a hard error that prevents startup.
func LoadDocuments(dir string) (*DocumentStore, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyDocumentStore(), nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var mf manifestFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}

	policy := bluemonday.UGCPolicy()
	docsDir := filepath.Join(dir, "documents")
	seen := make(map[string]struct{}, len(mf.Documents))
	docs := make([]Document, 0, len(mf.Documents))
	requiredSlugs := make(map[string]struct{})

	for _, entry := range mf.Documents {
		if err := validateManifestEntry(entry, seen); err != nil {
			return nil, err
		}
		seen[entry.Slug] = struct{}{}

		htmlPath := filepath.Join(docsDir, entry.File)
		raw, err := os.ReadFile(htmlPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: %s", ErrDocumentNotFound, entry.File)
			}
			return nil, fmt.Errorf("read document %s: %w", entry.File, err)
		}

		sanitised := policy.Sanitize(string(raw)) //nolint:misspell // bluemonday API uses American English spelling.

		doc := Document{
			Slug:     entry.Slug,
			Title:    entry.Title,
			Content:  sanitised,
			Position: entry.Position,
			Required: entry.Required,
		}
		docs = append(docs, doc)

		if entry.Required {
			requiredSlugs[entry.Slug] = struct{}{}
		}
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Position < docs[j].Position
	})

	return &DocumentStore{
		docs:          docs,
		requiredSlugs: requiredSlugs,
	}, nil
}

// Documents returns all loaded documents sorted by position.
func (s *DocumentStore) Documents() []Document {
	return s.docs
}

// RequiredSlugs returns the set of slugs that must be accepted during onboarding.
func (s *DocumentStore) RequiredSlugs() map[string]struct{} {
	return s.requiredSlugs
}

// ToModels converts all documents to protocol response types.
func (s *DocumentStore) ToModels() []models.OnboardingDocument {
	result := make([]models.OnboardingDocument, len(s.docs))
	for i, doc := range s.docs {
		result[i] = models.OnboardingDocument{
			Slug:     doc.Slug,
			Title:    doc.Title,
			Content:  doc.Content,
			Position: doc.Position,
			Required: doc.Required,
		}
	}
	return result
}

// validateManifestEntry checks a single manifest entry for validity.
func validateManifestEntry(entry manifestEntry, seen map[string]struct{}) error {
	if !slugPattern.MatchString(entry.Slug) {
		return fmt.Errorf("%w: %q", ErrInvalidSlug, entry.Slug)
	}
	if _, exists := seen[entry.Slug]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateSlug, entry.Slug)
	}
	if strings.TrimSpace(entry.Title) == "" {
		return fmt.Errorf("document %q has an empty title", entry.Slug)
	}
	if strings.ContainsAny(entry.File, `/\`) || strings.Contains(entry.File, "..") {
		return fmt.Errorf("document %q file path %q contains directory traversal", entry.Slug, entry.File)
	}
	return nil
}
