package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type codedSQLiteError struct {
	code int
	text string
}

func (e *codedSQLiteError) Error() string { return e.text }
func (e *codedSQLiteError) Code() int    { return e.code }

func TestDescribeSQLiteOpenError_DistinguishesCantOpenFromNoMemory(t *testing.T) {
	// This exact driver message is misleading: "out of memory" is text, but 14 is
	// SQLITE_CANTOPEN. Classification must use the numeric result code.
	cantOpen := &codedSQLiteError{code: 14, text: "out of memory (14)"}
	got := DescribeSQLiteOpenError(cantOpen)
	if !strings.Contains(got, "SQLITE_CANTOPEN") {
		t.Fatalf("code 14 must be reported as CANTOPEN, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "out of memory") {
		t.Fatalf("CANTOPEN must not be reported as memory exhaustion, got %q", got)
	}

	nomem := &codedSQLiteError{code: 7, text: "allocation failed"}
	got = DescribeSQLiteOpenError(nomem)
	if !strings.Contains(got, "SQLITE_NOMEM") || !strings.Contains(got, "内存") {
		t.Fatalf("code 7 should identify real SQLite NOMEM, got %q", got)
	}
}

func TestDescribeSQLiteOpenError_HandlesExtendedCodes(t *testing.T) {
	// SQLite extended result codes keep the primary result in the low byte.
	got := DescribeSQLiteOpenError(&codedSQLiteError{code: 14 | (3 << 8), text: "extended cantopen"})
	if !strings.Contains(got, "SQLITE_CANTOPEN") {
		t.Fatalf("extended CANTOPEN should retain primary classification, got %q", got)
	}
	// SQLITE_IOERR_NOMEM is an I/O error extended code (primary 10), not primary NOMEM 7.
	got = DescribeSQLiteOpenError(&codedSQLiteError{code: 10 | (12 << 8), text: "ioerr nomem"})
	if strings.Contains(got, "SQLITE_NOMEM") {
		t.Fatalf("IOERR_NOMEM must not be mislabeled as primary SQLITE_NOMEM, got %q", got)
	}
}

func TestDescribeSQLiteOpenError_PreservesUnknownCause(t *testing.T) {
	cause := errors.New("migration checksum mismatch")
	if got := DescribeSQLiteOpenError(cause); got != cause.Error() {
		t.Fatalf("unknown non-driver error should remain visible, got %q", got)
	}
}

func TestStoreOpen_DirectoryAsDatabaseReportsCantOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-is-a-directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil {
		t.Fatal("opening a directory as a SQLite database should fail")
	}
	got := DescribeSQLiteOpenError(err)
	if !strings.Contains(got, "SQLITE_CANTOPEN") {
		t.Fatalf("expected accurate CANTOPEN diagnosis, got %q (raw %v)", got, err)
	}
}
