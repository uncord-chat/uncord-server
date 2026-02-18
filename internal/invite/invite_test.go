package invite

import "testing"

func TestValidateMaxUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero is valid", new(0), false},
		{"positive is valid", new(10), false},
		{"negative is invalid", new(-1), true},
		{"large negative is invalid", new(-100), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMaxUses(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMaxUses() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMaxAge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   *int
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"zero is valid", new(0), false},
		{"positive is valid", new(3600), false},
		{"negative is invalid", new(-1), true},
		{"large negative is invalid", new(-86400), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMaxAge(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMaxAge() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"zero defaults", 0, DefaultLimit},
		{"negative defaults", -5, DefaultLimit},
		{"within range", 25, 25},
		{"at max", MaxLimit, MaxLimit},
		{"exceeds max", MaxLimit + 1, MaxLimit},
		{"one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClampLimit(tt.input)
			if got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
