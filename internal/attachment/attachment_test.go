package attachment

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	if ErrNotFound.Error() == "" {
		t.Error("ErrNotFound should have a non-empty message")
	}
}

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

func TestScanAttachment(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	msgID := uuid.New()
	chanID := uuid.New()
	uploaderID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)
	width := 1920
	height := 1080
	thumbnail := "thumb/abc.webp"

	row := &fakeRow{
		a: Attachment{
			ID:           id,
			MessageID:    &msgID,
			ChannelID:    chanID,
			UploaderID:   uploaderID,
			Filename:     "photo.png",
			ContentType:  "image/png",
			SizeBytes:    204800,
			StorageKey:   "uploads/photo.png",
			Width:        &width,
			Height:       &height,
			ThumbnailKey: &thumbnail,
			CreatedAt:    now,
		},
	}

	a, err := scanAttachment(row)
	if err != nil {
		t.Fatalf("scanAttachment() error = %v", err)
	}
	if a.ID != id {
		t.Errorf("ID = %v, want %v", a.ID, id)
	}
	if a.MessageID == nil || *a.MessageID != msgID {
		t.Errorf("MessageID = %v, want %v", a.MessageID, msgID)
	}
	if a.ChannelID != chanID {
		t.Errorf("ChannelID = %v, want %v", a.ChannelID, chanID)
	}
	if a.Filename != "photo.png" {
		t.Errorf("Filename = %q, want %q", a.Filename, "photo.png")
	}
	if a.SizeBytes != 204800 {
		t.Errorf("SizeBytes = %d, want %d", a.SizeBytes, 204800)
	}
	if a.Width == nil || *a.Width != 1920 {
		t.Errorf("Width = %v, want %d", a.Width, 1920)
	}
	if a.Height == nil || *a.Height != 1080 {
		t.Errorf("Height = %v, want %d", a.Height, 1080)
	}
	if a.ThumbnailKey == nil || *a.ThumbnailKey != thumbnail {
		t.Errorf("ThumbnailKey = %v, want %q", a.ThumbnailKey, thumbnail)
	}
	if !a.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", a.CreatedAt, now)
	}
}

func TestScanAttachment_NilOptionalFields(t *testing.T) {
	t.Parallel()

	row := &fakeRow{
		a: Attachment{
			ID:          uuid.New(),
			ChannelID:   uuid.New(),
			UploaderID:  uuid.New(),
			Filename:    "doc.pdf",
			ContentType: "application/pdf",
			SizeBytes:   1024,
			StorageKey:  "uploads/doc.pdf",
			CreatedAt:   time.Now().Truncate(time.Microsecond),
		},
	}

	a, err := scanAttachment(row)
	if err != nil {
		t.Fatalf("scanAttachment() error = %v", err)
	}
	if a.MessageID != nil {
		t.Errorf("MessageID = %v, want nil", a.MessageID)
	}
	if a.Width != nil {
		t.Errorf("Width = %v, want nil", a.Width)
	}
	if a.Height != nil {
		t.Errorf("Height = %v, want nil", a.Height)
	}
	if a.ThumbnailKey != nil {
		t.Errorf("ThumbnailKey = %v, want nil", a.ThumbnailKey)
	}
}

func TestScanAttachment_Error(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("column type mismatch")
	row := &fakeRow{err: scanErr}

	_, err := scanAttachment(row)
	if err == nil {
		t.Fatal("scanAttachment with error returned nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want wrapped %v", err, scanErr)
	}
}

func TestCollectAttachments_Empty(t *testing.T) {
	t.Parallel()

	rows := &fakeRows{attachments: nil}
	result, err := collectAttachments(rows)
	if err != nil {
		t.Fatalf("collectAttachments(empty) error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("collectAttachments(empty) returned %d, want 0", len(result))
	}
}

func TestCollectAttachments_MultipleRows(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Microsecond)
	chanID := uuid.New()
	uploaderID := uuid.New()

	rows := &fakeRows{
		attachments: []Attachment{
			{ID: uuid.New(), ChannelID: chanID, UploaderID: uploaderID, Filename: "a.png", ContentType: "image/png", SizeBytes: 100, StorageKey: "k/a.png", CreatedAt: now},
			{ID: uuid.New(), ChannelID: chanID, UploaderID: uploaderID, Filename: "b.jpg", ContentType: "image/jpeg", SizeBytes: 200, StorageKey: "k/b.jpg", CreatedAt: now},
			{ID: uuid.New(), ChannelID: chanID, UploaderID: uploaderID, Filename: "c.pdf", ContentType: "application/pdf", SizeBytes: 300, StorageKey: "k/c.pdf", CreatedAt: now},
		},
	}

	result, err := collectAttachments(rows)
	if err != nil {
		t.Fatalf("collectAttachments() error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("collectAttachments() returned %d, want 3", len(result))
	}
	if result[0].Filename != "a.png" {
		t.Errorf("result[0].Filename = %q, want %q", result[0].Filename, "a.png")
	}
	if result[1].Filename != "b.jpg" {
		t.Errorf("result[1].Filename = %q, want %q", result[1].Filename, "b.jpg")
	}
	if result[2].Filename != "c.pdf" {
		t.Errorf("result[2].Filename = %q, want %q", result[2].Filename, "c.pdf")
	}
}

func TestCollectAttachments_ScanError(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("column type mismatch")
	rows := &fakeRows{scanErr: scanErr}

	_, err := collectAttachments(rows)
	if err == nil {
		t.Fatal("collectAttachments with scan error returned nil error")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want wrapped %v", err, scanErr)
	}
}

func TestCollectAttachments_IterationError(t *testing.T) {
	t.Parallel()

	iterErr := errors.New("connection lost")
	rows := &fakeRows{iterErr: iterErr}

	_, err := collectAttachments(rows)
	if err == nil {
		t.Fatal("collectAttachments with iteration error returned nil error")
	}
	if !errors.Is(err, iterErr) {
		t.Errorf("error = %v, want wrapped %v", err, iterErr)
	}
}

// fakeRow implements pgx.Row for testing scanAttachment. It returns the pre-loaded Attachment fields, or the configured
// error on Scan.
type fakeRow struct {
	a   Attachment
	err error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}

	// scanAttachment scans 12 columns: id, message_id, channel_id, uploader_id, filename, content_type, size_bytes,
	// storage_key, width, height, thumbnail_key, created_at.
	if len(dest) != 12 {
		return errors.New("unexpected number of scan destinations")
	}
	*dest[0].(*uuid.UUID) = r.a.ID
	*dest[1].(**uuid.UUID) = r.a.MessageID
	*dest[2].(*uuid.UUID) = r.a.ChannelID
	*dest[3].(*uuid.UUID) = r.a.UploaderID
	*dest[4].(*string) = r.a.Filename
	*dest[5].(*string) = r.a.ContentType
	*dest[6].(*int64) = r.a.SizeBytes
	*dest[7].(*string) = r.a.StorageKey
	*dest[8].(**int) = r.a.Width
	*dest[9].(**int) = r.a.Height
	*dest[10].(**string) = r.a.ThumbnailKey
	*dest[11].(*time.Time) = r.a.CreatedAt
	return nil
}

// fakeRows implements the subset of pgx.Rows used by collectAttachments. Each call to Scan returns the next pre-loaded
// Attachment, or the configured scanErr. After all rows are consumed, Err returns iterErr.
type fakeRows struct {
	attachments []Attachment
	pos         int
	scanErr     error
	iterErr     error
}

func (r *fakeRows) Next() bool {
	if r.scanErr != nil && r.pos == 0 {
		r.pos = -1
		return true
	}
	if r.pos < 0 {
		return false
	}
	return r.pos < len(r.attachments)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	a := r.attachments[r.pos]
	r.pos++

	if len(dest) != 12 {
		return errors.New("unexpected number of scan destinations")
	}
	*dest[0].(*uuid.UUID) = a.ID
	*dest[1].(**uuid.UUID) = a.MessageID
	*dest[2].(*uuid.UUID) = a.ChannelID
	*dest[3].(*uuid.UUID) = a.UploaderID
	*dest[4].(*string) = a.Filename
	*dest[5].(*string) = a.ContentType
	*dest[6].(*int64) = a.SizeBytes
	*dest[7].(*string) = a.StorageKey
	*dest[8].(**int) = a.Width
	*dest[9].(**int) = a.Height
	*dest[10].(**string) = a.ThumbnailKey
	*dest[11].(*time.Time) = a.CreatedAt
	return nil
}

func (r *fakeRows) Err() error {
	return r.iterErr
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
