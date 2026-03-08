package onboarding

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDocumentsValidManifest(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [
		{"slug": "rules", "title": "Server Rules", "file": "rules.md", "position": 0, "required": true},
		{"slug": "privacy", "title": "Privacy Policy", "file": "privacy.md", "position": 1, "required": false}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "rules.md"), "# Rules\n\nBe nice.")
	writeFile(t, filepath.Join(docsDir, "privacy.md"), "# Privacy\n\nWe respect your privacy.")

	store, err := LoadDocuments(dir)
	if err != nil {
		t.Fatalf("LoadDocuments() returned error: %v", err)
	}

	docs := store.Documents()
	if len(docs) != 2 {
		t.Fatalf("len(Documents()) = %d, want 2", len(docs))
	}

	if docs[0].Slug != "rules" {
		t.Errorf("docs[0].Slug = %q, want %q", docs[0].Slug, "rules")
	}
	if docs[0].Position != 0 {
		t.Errorf("docs[0].Position = %d, want 0", docs[0].Position)
	}
	if !docs[0].Required {
		t.Error("docs[0].Required = false, want true")
	}
	if docs[1].Slug != "privacy" {
		t.Errorf("docs[1].Slug = %q, want %q", docs[1].Slug, "privacy")
	}
	if docs[1].Required {
		t.Error("docs[1].Required = true, want false")
	}

	required := store.RequiredSlugs()
	if len(required) != 1 {
		t.Fatalf("len(RequiredSlugs()) = %d, want 1", len(required))
	}
	if _, ok := required["rules"]; !ok {
		t.Error("RequiredSlugs() does not contain \"rules\"")
	}
}

func TestLoadDocumentsMissingManifestReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	store, err := LoadDocuments(dir)
	if err != nil {
		t.Fatalf("LoadDocuments() returned error: %v", err)
	}
	if len(store.Documents()) != 0 {
		t.Errorf("len(Documents()) = %d, want 0", len(store.Documents()))
	}
	if len(store.RequiredSlugs()) != 0 {
		t.Errorf("len(RequiredSlugs()) = %d, want 0", len(store.RequiredSlugs()))
	}
}

func TestLoadDocumentsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "manifest.json"), "{bad json")

	_, err := LoadDocuments(dir)
	if err == nil {
		t.Fatal("LoadDocuments() returned nil error, want manifest parse error")
	}
	if !errors.Is(err, ErrManifestInvalid) {
		t.Errorf("err = %v, want ErrManifestInvalid", err)
	}
}

func TestLoadDocumentsDuplicateSlug(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [
		{"slug": "rules", "title": "Rules", "file": "rules.md", "position": 0, "required": true},
		{"slug": "rules", "title": "Rules Again", "file": "rules2.md", "position": 1, "required": false}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "rules.md"), "rules")
	writeFile(t, filepath.Join(docsDir, "rules2.md"), "rules2")

	_, err := LoadDocuments(dir)
	if err == nil {
		t.Fatal("LoadDocuments() returned nil error, want duplicate slug error")
	}
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("err = %v, want ErrDuplicateSlug", err)
	}
}

func TestLoadDocumentsMissingFile(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [
		{"slug": "rules", "title": "Rules", "file": "missing.md", "position": 0, "required": true}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)

	_, err := LoadDocuments(dir)
	if err == nil {
		t.Fatal("LoadDocuments() returned nil error, want file not found error")
	}
	if !errors.Is(err, ErrDocumentNotFound) {
		t.Errorf("err = %v, want ErrDocumentNotFound", err)
	}
}

func TestLoadDocumentsPathTraversalRejection(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{"dot dot", "../etc/passwd"},
		{"forward slash", "sub/file.md"},
		{"backslash", `sub\file.md`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := `{"documents": [{"slug": "bad", "title": "Bad", "file": "` + tt.file + `", "position": 0, "required": false}]}`
			writeFile(t, filepath.Join(dir, "manifest.json"), manifest)

			_, err := LoadDocuments(dir)
			if err == nil {
				t.Fatal("LoadDocuments() returned nil error, want path traversal error")
			}
		})
	}
}

func TestLoadDocumentsPreservesRawMarkdown(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	source := "# Rules\n\nBe **nice**.\n\n- Item one\n- Item two\n"
	manifest := `{"documents": [
		{"slug": "rules", "title": "Rules", "file": "rules.md", "position": 0, "required": true}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "rules.md"), source)

	store, err := LoadDocuments(dir)
	if err != nil {
		t.Fatalf("LoadDocuments() returned error: %v", err)
	}

	docs := store.Documents()
	if len(docs) != 1 {
		t.Fatalf("len(Documents()) = %d, want 1", len(docs))
	}
	if docs[0].Content != source {
		t.Errorf("Content = %q, want %q", docs[0].Content, source)
	}
}

func TestLoadDocumentsInvalidSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
	}{
		{"starts with dash", "-rules"},
		{"contains uppercase", "Rules"},
		{"contains space", "my rules"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			docsDir := filepath.Join(dir, "documents")
			if err := os.MkdirAll(docsDir, 0o755); err != nil {
				t.Fatal(err)
			}

			manifest := `{"documents": [{"slug": "` + tt.slug + `", "title": "Test", "file": "test.md", "position": 0, "required": false}]}`
			writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
			writeFile(t, filepath.Join(docsDir, "test.md"), "test")

			_, err := LoadDocuments(dir)
			if err == nil {
				t.Fatalf("LoadDocuments() returned nil error for slug %q, want ErrInvalidSlug", tt.slug)
			}
			if !errors.Is(err, ErrInvalidSlug) {
				t.Errorf("err = %v, want ErrInvalidSlug", err)
			}
		})
	}
}

func TestLoadDocumentsEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [{"slug": "rules", "title": "   ", "file": "rules.md", "position": 0, "required": false}]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "rules.md"), "rules")

	_, err := LoadDocuments(dir)
	if err == nil {
		t.Fatal("LoadDocuments() returned nil error, want empty title error")
	}
}

func TestEmptyDocumentStore(t *testing.T) {
	store := EmptyDocumentStore()
	if len(store.Documents()) != 0 {
		t.Errorf("len(Documents()) = %d, want 0", len(store.Documents()))
	}
	if len(store.RequiredSlugs()) != 0 {
		t.Errorf("len(RequiredSlugs()) = %d, want 0", len(store.RequiredSlugs()))
	}
	if len(store.ToModels()) != 0 {
		t.Errorf("len(ToModels()) = %d, want 0", len(store.ToModels()))
	}
}

func TestToModels(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [
		{"slug": "rules", "title": "Server Rules", "file": "rules.md", "position": 0, "required": true}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "rules.md"), "Be nice.")

	store, err := LoadDocuments(dir)
	if err != nil {
		t.Fatalf("LoadDocuments() returned error: %v", err)
	}

	result := store.ToModels()
	if len(result) != 1 {
		t.Fatalf("len(ToModels()) = %d, want 1", len(result))
	}
	if result[0].Slug != "rules" {
		t.Errorf("Slug = %q, want %q", result[0].Slug, "rules")
	}
	if result[0].Title != "Server Rules" {
		t.Errorf("Title = %q, want %q", result[0].Title, "Server Rules")
	}
	if !result[0].Required {
		t.Error("Required = false, want true")
	}
}

func TestDocumentsSortedByPosition(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{"documents": [
		{"slug": "second", "title": "Second", "file": "b.md", "position": 5, "required": false},
		{"slug": "first", "title": "First", "file": "a.md", "position": 1, "required": false}
	]}`
	writeFile(t, filepath.Join(dir, "manifest.json"), manifest)
	writeFile(t, filepath.Join(docsDir, "a.md"), "a")
	writeFile(t, filepath.Join(docsDir, "b.md"), "b")

	store, err := LoadDocuments(dir)
	if err != nil {
		t.Fatalf("LoadDocuments() returned error: %v", err)
	}

	docs := store.Documents()
	if docs[0].Slug != "first" {
		t.Errorf("docs[0].Slug = %q, want %q", docs[0].Slug, "first")
	}
	if docs[1].Slug != "second" {
		t.Errorf("docs[1].Slug = %q, want %q", docs[1].Slug, "second")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
