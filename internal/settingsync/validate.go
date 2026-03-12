package settingsync

// ValidateUpsertParams checks that all fields in the upsert request meet their constraints.
func ValidateUpsertParams(params UpsertParams) error {
	if len(params.EncryptedBlob) == 0 {
		return ErrBlobEmpty
	}
	if len(params.EncryptedBlob) > MaxBlobSize {
		return ErrBlobTooLarge
	}
	if len(params.Salt) != 16 {
		return ErrSaltLength
	}
	if len(params.Nonce) != 12 {
		return ErrNonceLength
	}
	if params.BlobVersion < 1 {
		return ErrVersionInvalid
	}
	return nil
}
