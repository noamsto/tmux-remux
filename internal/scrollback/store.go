// Package scrollback provides a content-addressed compressed file store
// for tmux pane scrollback contents.
package scrollback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/klauspost/compress/zstd"
)

// shaPattern bounds a content hash to a lowercase-hex sha256. Get/Stream/Delete
// reject anything else, so a tampered manifest referencing a malformed sha can
// neither drive path() outside dir (via "/" or "..") nor panic it on sha[:2].
var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Store is a content-addressed file store for compressed scrollback contents.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. Subdirectories are created lazily.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Put writes content to the CAS and returns its sha256 (hex) and the number
// of bytes written on disk (compressed). Idempotent: same content → same sha.
func (s *Store) Put(_ context.Context, content []byte) (string, int64, error) {
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	dest := s.path(sha)

	if info, err := os.Stat(dest); err == nil {
		return sha, info.Size(), nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", 0, fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".tmp-*")
	if err != nil {
		return "", 0, fmt.Errorf("tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	enc, err := zstd.NewWriter(tmp, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("zstd writer: %w", err)
	}
	if _, err := enc.Write(content); err != nil {
		_ = enc.Close()
		_ = tmp.Close()
		return "", 0, fmt.Errorf("zstd write: %w", err)
	}
	if err := enc.Close(); err != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("zstd close: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("close tmp: %w", err)
	}

	if err := os.Rename(tmpName, dest); err != nil {
		return "", 0, fmt.Errorf("rename: %w", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		return "", 0, err
	}
	return sha, info.Size(), nil
}

// Get reads and decompresses the scrollback identified by sha.
func (s *Store) Get(_ context.Context, sha string) ([]byte, error) {
	if !shaPattern.MatchString(sha) {
		return nil, fmt.Errorf("invalid scrollback sha %q", sha)
	}
	f, err := os.Open(s.path(sha))
	if err != nil {
		return nil, fmt.Errorf("open scrollback: %w", err)
	}
	defer func() { _ = f.Close() }()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	defer dec.Close()

	return io.ReadAll(dec)
}

// Stream returns a streaming reader of the decompressed scrollback identified
// by sha. The caller MUST Close the returned ReadCloser. If the file does not
// exist, the returned error wraps fs.ErrNotExist.
//
// ctx is accepted for signature symmetry with the rest of the package and
// is not honored during reads. To interrupt an in-flight stream, the caller
// closes the returned ReadCloser.
func (s *Store) Stream(_ context.Context, sha string) (io.ReadCloser, error) {
	if !shaPattern.MatchString(sha) {
		return nil, fmt.Errorf("invalid scrollback sha %q", sha)
	}
	f, err := os.Open(s.path(sha))
	if err != nil {
		return nil, fmt.Errorf("open scrollback: %w", err)
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("zstd reader: %w", err)
	}
	return &streamReader{file: f, dec: dec}, nil
}

// streamReader couples a zstd.Decoder with its backing file so Close releases both.
type streamReader struct {
	file *os.File
	dec  *zstd.Decoder
}

func (r *streamReader) Read(p []byte) (int, error) { return r.dec.Read(p) }

func (r *streamReader) Close() error {
	r.dec.Close()
	return r.file.Close()
}

// Delete removes the scrollback file identified by sha. Missing files are not
// an error.
func (s *Store) Delete(_ context.Context, sha string) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("invalid scrollback sha %q", sha)
	}
	if err := os.Remove(s.path(sha)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove scrollback: %w", err)
	}
	return nil
}

func (s *Store) path(sha string) string {
	return filepath.Join(s.dir, sha[:2], sha+".zst")
}
