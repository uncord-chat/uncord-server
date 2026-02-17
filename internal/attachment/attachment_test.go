package attachment

import (
	"testing"
)

func TestCreateParamsZeroValue(t *testing.T) {
	t.Parallel()

	var p CreateParams
	if p.Filename != "" || p.ContentType != "" || p.StorageKey != "" {
		t.Error("CreateParams zero value should have empty strings")
	}
	if p.SizeBytes != 0 {
		t.Error("CreateParams zero value should have zero size")
	}
	if p.Width != nil || p.Height != nil {
		t.Error("CreateParams zero value should have nil dimensions")
	}
}

func TestAttachmentZeroValue(t *testing.T) {
	t.Parallel()

	var a Attachment
	if a.MessageID != nil {
		t.Error("Attachment zero value should have nil MessageID")
	}
	if a.ThumbnailKey != nil {
		t.Error("Attachment zero value should have nil ThumbnailKey")
	}
	if a.Width != nil || a.Height != nil {
		t.Error("Attachment zero value should have nil dimensions")
	}
	if !a.CreatedAt.IsZero() {
		t.Error("Attachment zero value should have zero CreatedAt")
	}
}
