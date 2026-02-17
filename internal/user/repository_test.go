package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	// Verify sentinel errors are distinct and usable with errors.Is.
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrAlreadyExists", ErrAlreadyExists},
		{"ErrInvalidToken", ErrInvalidToken},
		{"ErrDisplayNameLength", ErrDisplayNameLength},
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				if !errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%s, %s) = false, want true", a.name, b.name)
				}
			} else {
				if errors.Is(a.err, b.err) {
					t.Errorf("errors.Is(%s, %s) = true, want false", a.name, b.name)
				}
			}
		}
	}
}

func TestCreateParamsZeroValue(t *testing.T) {
	t.Parallel()

	var p CreateParams
	if p.Email != "" || p.Username != "" || p.PasswordHash != "" || p.VerifyToken != "" {
		t.Error("CreateParams zero value should have empty strings")
	}
	if !p.VerifyExpiry.IsZero() {
		t.Error("CreateParams zero value should have zero time")
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("nil is a no-op", func(t *testing.T) {
		t.Parallel()
		NormalizeDisplayName(nil) // must not panic
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		name := ptr("  Bob  ")
		NormalizeDisplayName(name)
		if *name != "Bob" {
			t.Errorf("expected trimmed value %q, got %q", "Bob", *name)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		name := ptr("Alice")
		NormalizeDisplayName(name)
		if *name != "Alice" {
			t.Errorf("expected %q, got %q", "Alice", *name)
		}
	})
}

func TestValidateDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"single char", ptr("A"), false},
		{"32 chars", ptr(strings.Repeat("a", 32)), false},
		{"33 chars", ptr(strings.Repeat("a", 33)), true},
		{"empty string", ptr(""), true},
		{"32 multibyte runes", ptr(strings.Repeat("🎮", 32)), false},
		{"33 multibyte runes", ptr(strings.Repeat("🎮", 33)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDisplayName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDisplayName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrDisplayNameLength) {
				t.Errorf("ValidateDisplayName() error = %v, want ErrDisplayNameLength", err)
			}
		})
	}
}

func TestNormalizeAndValidateDisplayName(t *testing.T) {
	t.Parallel()

	t.Run("whitespace only rejects after trim", func(t *testing.T) {
		t.Parallel()
		name := ptr("   ")
		NormalizeDisplayName(name)
		if err := ValidateDisplayName(name); !errors.Is(err, ErrDisplayNameLength) {
			t.Errorf("expected ErrDisplayNameLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		name := ptr("  Bob  ")
		NormalizeDisplayName(name)
		if err := ValidateDisplayName(name); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if *name != "Bob" {
			t.Errorf("expected %q, got %q", "Bob", *name)
		}
	})
}

func TestNormalizePronouns(t *testing.T) {
	t.Parallel()

	t.Run("nil is a no-op", func(t *testing.T) {
		t.Parallel()
		NormalizePronouns(nil) // must not panic
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		p := ptr("  she/her  ")
		NormalizePronouns(p)
		if *p != "she/her" {
			t.Errorf("expected trimmed value %q, got %q", "she/her", *p)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		p := ptr("they/them")
		NormalizePronouns(p)
		if *p != "they/them" {
			t.Errorf("expected %q, got %q", "they/them", *p)
		}
	})
}

func TestValidatePronouns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"single char", ptr("X"), false},
		{"40 chars", ptr(strings.Repeat("a", 40)), false},
		{"41 chars", ptr(strings.Repeat("a", 41)), true},
		{"empty string", ptr(""), true},
		{"40 multibyte runes", ptr(strings.Repeat("ñ", 40)), false},
		{"41 multibyte runes", ptr(strings.Repeat("ñ", 41)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePronouns(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePronouns() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrPronounsLength) {
				t.Errorf("ValidatePronouns() error = %v, want ErrPronounsLength", err)
			}
		})
	}
}

func TestNormalizeAndValidatePronouns(t *testing.T) {
	t.Parallel()

	t.Run("whitespace only rejects after trim", func(t *testing.T) {
		t.Parallel()
		p := ptr("   ")
		NormalizePronouns(p)
		if err := ValidatePronouns(p); !errors.Is(err, ErrPronounsLength) {
			t.Errorf("expected ErrPronounsLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		p := ptr("  he/him  ")
		NormalizePronouns(p)
		if err := ValidatePronouns(p); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if *p != "he/him" {
			t.Errorf("expected %q, got %q", "he/him", *p)
		}
	})
}

func TestNormalizeAbout(t *testing.T) {
	t.Parallel()

	t.Run("nil is a no-op", func(t *testing.T) {
		t.Parallel()
		NormalizeAbout(nil) // must not panic
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		a := ptr("  hello world  ")
		NormalizeAbout(a)
		if *a != "hello world" {
			t.Errorf("expected trimmed value %q, got %q", "hello world", *a)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		a := ptr("about me")
		NormalizeAbout(a)
		if *a != "about me" {
			t.Errorf("expected %q, got %q", "about me", *a)
		}
	})
}

func TestValidateAbout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *string
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"single char", ptr("A"), false},
		{"190 chars", ptr(strings.Repeat("a", 190)), false},
		{"191 chars", ptr(strings.Repeat("a", 191)), true},
		{"empty string", ptr(""), true},
		{"190 multibyte runes", ptr(strings.Repeat("日", 190)), false},
		{"191 multibyte runes", ptr(strings.Repeat("日", 191)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAbout(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAbout() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrAboutLength) {
				t.Errorf("ValidateAbout() error = %v, want ErrAboutLength", err)
			}
		})
	}
}

func TestNormalizeAndValidateAbout(t *testing.T) {
	t.Parallel()

	t.Run("whitespace only rejects after trim", func(t *testing.T) {
		t.Parallel()
		a := ptr("   ")
		NormalizeAbout(a)
		if err := ValidateAbout(a); !errors.Is(err, ErrAboutLength) {
			t.Errorf("expected ErrAboutLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		a := ptr("  about me  ")
		NormalizeAbout(a)
		if err := ValidateAbout(a); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if *a != "about me" {
			t.Errorf("expected %q, got %q", "about me", *a)
		}
	})
}

func TestValidateThemeColour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero", intPtr(0), false},
		{"max RGB", intPtr(0xFFFFFF), false},
		{"mid range", intPtr(0x7F7F7F), false},
		{"one over max", intPtr(0xFFFFFF + 1), true},
		{"negative", intPtr(-1), true},
		{"large negative", intPtr(-999999), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateThemeColour(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateThemeColour() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrThemeColourRange) {
				t.Errorf("ValidateThemeColour() error = %v, want ErrThemeColourRange", err)
			}
		})
	}
}

func TestBuildUpdateClauses(t *testing.T) {
	t.Parallel()
	id := uuid.New()

	tests := []struct {
		name           string
		params         UpdateParams
		wantClauses    int
		wantArgKeys    []string
		notWantArgKeys []string
	}{
		{
			name:        "empty params produces no SET clauses",
			params:      UpdateParams{},
			wantClauses: 0,
			wantArgKeys: []string{"id"},
		},
		{
			name:        "display name only",
			params:      UpdateParams{DisplayName: ptr("Alice")},
			wantClauses: 1,
			wantArgKeys: []string{"id", "display_name"},
		},
		{
			name:        "avatar key only",
			params:      UpdateParams{AvatarKey: ptr("avatars/abc.png")},
			wantClauses: 1,
			wantArgKeys: []string{"id", "avatar_key"},
		},
		{
			name:        "pronouns only",
			params:      UpdateParams{Pronouns: ptr("they/them")},
			wantClauses: 1,
			wantArgKeys: []string{"id", "pronouns"},
		},
		{
			name:        "banner key only",
			params:      UpdateParams{BannerKey: ptr("banners/xyz.jpg")},
			wantClauses: 1,
			wantArgKeys: []string{"id", "banner_key"},
		},
		{
			name:        "about only",
			params:      UpdateParams{About: ptr("hello world")},
			wantClauses: 1,
			wantArgKeys: []string{"id", "about"},
		},
		{
			name:        "theme colour primary only",
			params:      UpdateParams{ThemeColourPrimary: intPtr(0xFF0000)},
			wantClauses: 1,
			wantArgKeys: []string{"id", "theme_colour_primary"},
		},
		{
			name:        "theme colour secondary only",
			params:      UpdateParams{ThemeColourSecondary: intPtr(0x00FF00)},
			wantClauses: 1,
			wantArgKeys: []string{"id", "theme_colour_secondary"},
		},
		{
			name: "all fields set",
			params: UpdateParams{
				DisplayName:          ptr("Bob"),
				AvatarKey:            ptr("a.png"),
				Pronouns:             ptr("he/him"),
				BannerKey:            ptr("b.jpg"),
				About:                ptr("bio"),
				ThemeColourPrimary:   intPtr(100),
				ThemeColourSecondary: intPtr(200),
			},
			wantClauses: 7,
			wantArgKeys: []string{
				"id", "display_name", "avatar_key", "pronouns",
				"banner_key", "about", "theme_colour_primary", "theme_colour_secondary",
			},
		},
		{
			name:           "partial mix: display name and about",
			params:         UpdateParams{DisplayName: ptr("Eve"), About: ptr("about me")},
			wantClauses:    2,
			wantArgKeys:    []string{"id", "display_name", "about"},
			notWantArgKeys: []string{"avatar_key", "pronouns", "banner_key", "theme_colour_primary", "theme_colour_secondary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clauses, args := buildUpdateClauses(id, tt.params)

			if len(clauses) != tt.wantClauses {
				t.Errorf("len(clauses) = %d, want %d; clauses: %v", len(clauses), tt.wantClauses, clauses)
			}

			for _, key := range tt.wantArgKeys {
				if _, ok := args[key]; !ok {
					t.Errorf("named args missing key %q", key)
				}
			}

			for _, key := range tt.notWantArgKeys {
				if _, ok := args[key]; ok {
					t.Errorf("named args should not contain key %q", key)
				}
			}

			// Every SET clause must reference a named arg that exists in the map.
			for _, clause := range clauses {
				// Extract the @name from the clause (e.g., "display_name = @display_name" → "display_name").
				parts := strings.SplitN(clause, "@", 2)
				if len(parts) != 2 {
					t.Errorf("clause %q does not contain a named parameter", clause)
					continue
				}
				if _, ok := args[parts[1]]; !ok {
					t.Errorf("clause references @%s but named args does not contain it", parts[1])
				}
			}
		})
	}
}

func ptr(s string) *string { return &s }

func intPtr(i int) *int { return &i }
