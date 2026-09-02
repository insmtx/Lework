package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// nonSeekableReader 模拟 multipart part 等只能顺序读取、无法 Seek 的流。
type nonSeekableReader struct {
	data []byte
	off  int
}

func (r *nonSeekableReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func setupUploadStreamTest(t *testing.T) *gorm.DB {
	t.Helper()
	initTestStorage(t)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&types.FileUpload{}); err != nil {
		t.Fatalf("migrate file upload: %v", err)
	}
	return database
}

func TestUploadStream_Success(t *testing.T) {
	database := setupUploadStreamTest(t)
	ctx := context.Background()

	data := []byte("hello world, this is a streaming upload payload")
	reader := &nonSeekableReader{data: data}

	file, err := UploadStream(ctx, database, UploadStreamParams{
		Filename:     "stream-upload.txt",
		OriginalName: "original.txt",
		MimeType:     "text/plain",
		OrgID:        7,
		OwnerID:      8,
		ObjectKey:    "test/7/uploads/stream-upload.txt",
		Purpose:      PurposeAttachment,
	}, reader, 1024)
	if err != nil {
		t.Fatalf("UploadStream: %v", err)
	}

	if file.FileSize != int64(len(data)) {
		t.Errorf("expected file size %d, got %d", len(data), file.FileSize)
	}
	sum := sha256.Sum256(data)
	if file.Sha256 != hex.EncodeToString(sum[:]) {
		t.Errorf("expected sha256 %x, got %s", sum, file.Sha256)
	}

	var record types.FileUpload
	if err := database.First(&record, "public_id = ?", file.PublicID).Error; err != nil {
		t.Fatalf("file upload record not found: %v", err)
	}

	reader2, err := OpenFileUpload(ctx, file)
	if err != nil {
		t.Fatalf("open uploaded file: %v", err)
	}
	defer reader2.Close()
	got, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("uploaded content mismatch: got %q, want %q", got, data)
	}
}

func TestUploadStream_TooLarge(t *testing.T) {
	database := setupUploadStreamTest(t)
	ctx := context.Background()

	data := []byte("1234567890")
	reader := &nonSeekableReader{data: data}

	_, err := UploadStream(ctx, database, UploadStreamParams{
		Filename:  "too-large.txt",
		MimeType:  "text/plain",
		OrgID:     7,
		OwnerID:   8,
		ObjectKey: "test/7/uploads/too-large.txt",
	}, reader, 5)
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("expected ErrUploadTooLarge, got %v", err)
	}

	// 超限对象应被清理，不残留孤儿对象。
	if _, err := GetStorage().GetObject(ctx, DefaultBucket(), "test/7/uploads/too-large.txt"); err == nil {
		t.Error("oversized object should have been cleaned up")
	}
}

func TestUploadStream_Empty(t *testing.T) {
	database := setupUploadStreamTest(t)
	ctx := context.Background()

	reader := &nonSeekableReader{data: nil}

	_, err := UploadStream(ctx, database, UploadStreamParams{
		Filename:  "empty.txt",
		MimeType:  "text/plain",
		OrgID:     7,
		OwnerID:   8,
		ObjectKey: "test/7/uploads/empty.txt",
	}, reader, 1024)
	if !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("expected ErrEmptyFile, got %v", err)
	}
}

func TestUploadStream_CleanupOnDBFailure(t *testing.T) {
	database := setupUploadStreamTest(t)
	ctx := context.Background()

	// 删除表使 FileUpload 记录写入必然失败，验证已上传对象会被清理而不残留孤儿。
	if err := database.Migrator().DropTable(&types.FileUpload{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	_, err := UploadStream(ctx, database, UploadStreamParams{
		Filename:  "db-fail.txt",
		MimeType:  "text/plain",
		OrgID:     7,
		OwnerID:   8,
		ObjectKey: "test/7/uploads/db-fail.txt",
	}, &nonSeekableReader{data: []byte("content")}, 1024)
	if err == nil || !strings.Contains(err.Error(), "create file upload record") {
		t.Fatalf("expected record create failure, got %v", err)
	}

	if _, err := GetStorage().GetObject(ctx, DefaultBucket(), "test/7/uploads/db-fail.txt"); err == nil {
		t.Error("object should have been cleaned up when record creation failed")
	}
}

// tinyChunkReader 每次 Read 最多返回 chunk 字节，模拟网络流式分块读取。
type tinyChunkReader struct {
	r     io.Reader
	chunk int
}

func (r *tinyChunkReader) Read(p []byte) (int, error) {
	if len(p) > r.chunk {
		p = p[:r.chunk]
	}
	return r.r.Read(p)
}

func TestUploadStream_MultiChunk(t *testing.T) {
	database := setupUploadStreamTest(t)
	ctx := context.Background()

	// 64KB 数据按每次最多 7 字节分块读取，验证跨多次 Read 的计数与 sha256 累计正确。
	data := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	reader := &tinyChunkReader{r: &nonSeekableReader{data: data}, chunk: 7}

	file, err := UploadStream(ctx, database, UploadStreamParams{
		Filename:     "stream-chunked.bin",
		OriginalName: "original.bin",
		MimeType:     "application/octet-stream",
		OrgID:        7,
		OwnerID:      8,
		ObjectKey:    "test/7/uploads/stream-chunked.bin",
		Purpose:      PurposeAttachment,
	}, reader, 1<<20)
	if err != nil {
		t.Fatalf("UploadStream: %v", err)
	}

	if file.FileSize != int64(len(data)) {
		t.Errorf("expected file size %d, got %d", len(data), file.FileSize)
	}
	sum := sha256.Sum256(data)
	if file.Sha256 != hex.EncodeToString(sum[:]) {
		t.Errorf("expected sha256 %x, got %s", sum, file.Sha256)
	}

	reader2, err := OpenFileUpload(ctx, file)
	if err != nil {
		t.Fatalf("open uploaded file: %v", err)
	}
	defer reader2.Close()
	got, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("uploaded content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}
