package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const defaultRootFolderName = "ggnetwork"

// CASPathTransformFunc maps a storage key to a SHA-256 content-addressed path.
func CASPathTransformFunc(key string) PathKey {
	hash := sha256.Sum256([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	blocksize := 5
	sliceLen := len(hashStr) / blocksize
	paths := make([]string, sliceLen)
	for i := 0; i < sliceLen; i++ {
		from, to := i*blocksize, (i*blocksize)+blocksize
		paths[i] = hashStr[from:to]
	}
	return PathKey{PathName: strings.Join(paths, "/"), Filename: hashStr}
}

type PathTransformFunc func(string) PathKey

type PathKey struct {
	PathName string
	Filename string
}

func (p PathKey) FirstPathName() string {
	paths := strings.Split(p.PathName, "/")
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func (p PathKey) FullPath() string { return fmt.Sprintf("%s/%s", p.PathName, p.Filename) }

type StoreOpts struct {
	// Root is the folder name of the root, containing all the folders/files of the system.
	Root              string
	PathTransformFunc PathTransformFunc
}

var DefaultPathTransformFunc = func(key string) PathKey {
	return PathKey{PathName: key, Filename: key}
}

type Store struct{ StoreOpts }

func NewStore(opts StoreOpts) *Store {
	if opts.PathTransformFunc == nil {
		opts.PathTransformFunc = DefaultPathTransformFunc
	}
	if len(opts.Root) == 0 {
		opts.Root = defaultRootFolderName
	}

	// Ensure the root path is Windows-safe
	opts.Root = DefaultPathSanitizer.SanitizePath(opts.Root)

	return &Store{StoreOpts: opts}
}

func (s *Store) Has(key string) bool {
	fullPathWithRoot, err := s.fullPath(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(fullPathWithRoot)
	return !errors.Is(err, os.ErrNotExist)
}

func (s *Store) Clear() error { return os.RemoveAll(s.Root) }

func (s *Store) Delete(key string) error {
	fullPathWithRoot, err := s.fullPath(key)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	defer func() { slog.Info("deleted", slog.String("key", filepath.Base(fullPathWithRoot))) }()
	if err := os.Remove(fullPathWithRoot); err != nil {
		return err
	}
	dir := filepath.Dir(fullPathWithRoot)
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

func (s *Store) Write(key string, r io.Reader) (int64, error) { return s.writeStream(key, r) }

func (s *Store) WriteDecrypt(copyDecrypt func([]byte, io.Reader, io.Writer) (int, error), encKey []byte, key string, r io.Reader) (int64, error) {
	f, err := s.openFileForWriting(key)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("failed to close file", slog.String("error", closeErr.Error()))
		}
	}()
	n, err := copyDecrypt(encKey, r, f)
	return int64(n), err
}

// createFileAtomic creates a file atomically to avoid race conditions
// This is a workaround for GO-2025-3750 vulnerability
func (s *Store) createFileAtomic(fullPath string) (*os.File, error) {
	// Create the directory first
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Use O_CREATE|O_EXCL for atomic file creation
	// This prevents race conditions where multiple processes might create the same file
	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		// If file already exists, return an error
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("file %s already exists", fullPath)
		}
		return nil, fmt.Errorf("failed to create file %s: %w", fullPath, err)
	}

	return file, nil
}

func (s *Store) openFileForWriting(key string) (*os.File, error) {
	fullPathWithRoot, err := s.fullPath(key)
	if err != nil {
		return nil, err
	}
	return s.createFileAtomic(fullPathWithRoot)
}

func (s *Store) writeStream(key string, r io.Reader) (int64, error) {
	f, err := s.openFileForWriting(key)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("failed to close file", slog.String("error", closeErr.Error()))
		}
	}()
	return io.Copy(f, r)
}

func (s *Store) Read(key string) (int64, io.ReadCloser, error) { return s.readStream(key) }

func (s *Store) readStream(key string) (int64, io.ReadCloser, error) {
	fullPathWithRoot, err := s.fullPath(key)
	if err != nil {
		return 0, nil, err
	}
	file, err := os.Open(fullPathWithRoot)
	if err != nil {
		return 0, nil, err
	}
	fi, err := file.Stat()
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			slog.Error("failed to close file", slog.String("error", closeErr.Error()))
		}
		return 0, nil, err
	}
	return fi.Size(), file, nil
}

func (s *Store) fullPath(key string) (string, error) {
	pathKey := s.PathTransformFunc(key)
	rel := filepath.Clean(filepath.FromSlash(pathKey.FullPath()))
	if rel == "." || rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid storage key")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", err
	}
	full := filepath.Clean(filepath.Join(root, rel))
	relToRoot, err := filepath.Rel(root, full)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage key escapes root")
	}
	return full, nil
}
