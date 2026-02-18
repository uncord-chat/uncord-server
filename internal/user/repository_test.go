package user

import (
	"errors"
	"strings"
	"testing"
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
		name := new("  Bob  ")
		NormalizeDisplayName(name)
		if *name != "Bob" {
			t.Errorf("expected trimmed value %q, got %q", "Bob", *name)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		name := new("Alice")
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
		{"single char", new("A"), false},
		{"32 chars", new(strings.Repeat("a", 32)), false},
		{"33 chars", new(strings.Repeat("a", 33)), true},
		{"empty string", new(""), true},
		{"32 multibyte runes", new(strings.Repeat("🎮", 32)), false},
		{"33 multibyte runes", new(strings.Repeat("🎮", 33)), true},
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
		name := new("   ")
		NormalizeDisplayName(name)
		if err := ValidateDisplayName(name); !errors.Is(err, ErrDisplayNameLength) {
			t.Errorf("expected ErrDisplayNameLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		name := new("  Bob  ")
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
		p := new("  she/her  ")
		NormalizePronouns(p)
		if *p != "she/her" {
			t.Errorf("expected trimmed value %q, got %q", "she/her", *p)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		p := new("they/them")
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
		{"single char", new("X"), false},
		{"40 chars", new(strings.Repeat("a", 40)), false},
		{"41 chars", new(strings.Repeat("a", 41)), true},
		{"empty string", new(""), true},
		{"40 multibyte runes", new(strings.Repeat("ñ", 40)), false},
		{"41 multibyte runes", new(strings.Repeat("ñ", 41)), true},
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
		p := new("   ")
		NormalizePronouns(p)
		if err := ValidatePronouns(p); !errors.Is(err, ErrPronounsLength) {
			t.Errorf("expected ErrPronounsLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		p := new("  he/him  ")
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
		a := new("  hello world  ")
		NormalizeAbout(a)
		if *a != "hello world" {
			t.Errorf("expected trimmed value %q, got %q", "hello world", *a)
		}
	})

	t.Run("leaves clean value unchanged", func(t *testing.T) {
		t.Parallel()
		a := new("about me")
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
		{"single char", new("A"), false},
		{"190 chars", new(strings.Repeat("a", 190)), false},
		{"191 chars", new(strings.Repeat("a", 191)), true},
		{"empty string", new(""), true},
		{"190 multibyte runes", new(strings.Repeat("日", 190)), false},
		{"191 multibyte runes", new(strings.Repeat("日", 191)), true},
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
		a := new("   ")
		NormalizeAbout(a)
		if err := ValidateAbout(a); !errors.Is(err, ErrAboutLength) {
			t.Errorf("expected ErrAboutLength after trimming whitespace-only input, got %v", err)
		}
	})

	t.Run("padded value passes after trim", func(t *testing.T) {
		t.Parallel()
		a := new("  about me  ")
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
		{"zero", new(0), false},
		{"max RGB", new(0xFFFFFF), false},
		{"mid range", new(0x7F7F7F), false},
		{"one over max", new(0xFFFFFF + 1), true},
		{"negative", new(-1), true},
		{"large negative", new(-999999), true},
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
