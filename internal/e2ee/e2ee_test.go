package e2ee

import (
	"testing"
)

func TestValidatePublicKey(t *testing.T) {
	tests := []struct {
		name    string
		key     []byte
		wantErr bool
	}{
		{name: "valid 32 bytes", key: make([]byte, 32), wantErr: false},
		{name: "too short", key: make([]byte, 16), wantErr: true},
		{name: "too long", key: make([]byte, 64), wantErr: true},
		{name: "empty", key: nil, wantErr: true},
		{name: "one byte", key: make([]byte, 1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePublicKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePublicKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSignature(t *testing.T) {
	tests := []struct {
		name    string
		sig     []byte
		wantErr bool
	}{
		{name: "valid 64 bytes", sig: make([]byte, 64), wantErr: false},
		{name: "too short", sig: make([]byte, 32), wantErr: true},
		{name: "too long", sig: make([]byte, 128), wantErr: true},
		{name: "empty", sig: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSignature(tt.sig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSignature() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOPKBatch(t *testing.T) {
	validKey := make([]byte, 32)
	shortKey := make([]byte, 16)

	tests := []struct {
		name     string
		keys     []UploadOPKParams
		maxBatch int
		wantErr  bool
	}{
		{
			name:     "valid single key",
			keys:     []UploadOPKParams{{KeyID: 1, PublicKey: validKey}},
			maxBatch: 100,
			wantErr:  false,
		},
		{
			name: "valid multiple keys",
			keys: []UploadOPKParams{
				{KeyID: 1, PublicKey: validKey},
				{KeyID: 2, PublicKey: validKey},
				{KeyID: 3, PublicKey: validKey},
			},
			maxBatch: 100,
			wantErr:  false,
		},
		{
			name:     "empty batch",
			keys:     []UploadOPKParams{},
			maxBatch: 100,
			wantErr:  true,
		},
		{
			name:     "nil batch",
			keys:     nil,
			maxBatch: 100,
			wantErr:  true,
		},
		{
			name: "exceeds max batch",
			keys: func() []UploadOPKParams {
				keys := make([]UploadOPKParams, 5)
				for i := range keys {
					keys[i] = UploadOPKParams{KeyID: i, PublicKey: validKey}
				}
				return keys
			}(),
			maxBatch: 3,
			wantErr:  true,
		},
		{
			name: "invalid key in batch",
			keys: []UploadOPKParams{
				{KeyID: 1, PublicKey: validKey},
				{KeyID: 2, PublicKey: shortKey},
			},
			maxBatch: 100,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOPKBatch(tt.keys, tt.maxBatch)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOPKBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
