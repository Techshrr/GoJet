package files

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var storageKeyPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type NativeStorage struct {
	root       string
	quarantine string
	published  string
}

func NewNativeStorage(root string) (*NativeStorage, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInvalidInput
	}
	root = filepath.Clean(root)
	storage := &NativeStorage{
		root:       root,
		quarantine: filepath.Join(root, "quarantine"),
		published:  filepath.Join(root, "published"),
	}
	for _, dir := range []string{storage.root, storage.quarantine, storage.published} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create file storage: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secure file storage: %w", err)
		}
	}
	return storage, nil
}

func NewStorageKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func validStorageKey(key string) bool {
	return storageKeyPattern.MatchString(key)
}

func objectPath(root, key string) (string, error) {
	if !validStorageKey(key) {
		return "", ErrInvalidInput
	}
	candidate := filepath.Join(root, key[:2], key[2:4], key)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(os.PathSeparator) {
		return "", ErrInvalidInput
	}
	return candidate, nil
}

func (s *NativeStorage) ensure() error {
	if s == nil || s.root == "" || s.quarantine == "" || s.published == "" {
		return ErrStorageUnavailable
	}
	return nil
}

func (s *NativeStorage) WriteQuarantine(key string, src io.Reader, maxBytes int64) (uint64, string, error) {
	if err := s.ensure(); err != nil {
		return 0, "", err
	}
	if src == nil || maxBytes <= 0 {
		return 0, "", ErrInvalidInput
	}
	path, err := objectPath(s.quarantine, key)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, "", err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	written, err := io.Copy(io.MultiWriter(file, hash), limited)
	if err != nil {
		return 0, "", err
	}
	if written > maxBytes {
		return 0, "", ErrQuota
	}
	if err := file.Sync(); err != nil {
		return 0, "", err
	}
	if err := file.Close(); err != nil {
		return 0, "", err
	}
	remove = false
	return uint64(written), hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *NativeStorage) OpenQuarantine(key string) (*os.File, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	path, err := objectPath(s.quarantine, key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *NativeStorage) OpenPublished(key string) (*os.File, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	path, err := objectPath(s.published, key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *NativeStorage) Publish(key string) error {
	if err := s.ensure(); err != nil {
		return err
	}
	source, err := objectPath(s.quarantine, key)
	if err != nil {
		return err
	}
	target, err := objectPath(s.published, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}

func (s *NativeStorage) ReturnToQuarantine(key string) error {
	if err := s.ensure(); err != nil {
		return err
	}
	source, err := objectPath(s.published, key)
	if err != nil {
		return err
	}
	target, err := objectPath(s.quarantine, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, quarantineErr := os.Stat(target); quarantineErr == nil {
				return nil
			}
		}
		return err
	}
	return os.Chmod(target, 0o600)
}

func (s *NativeStorage) Remove(key string) error {
	if err := s.ensure(); err != nil {
		return err
	}
	var joined error
	for _, root := range []string{s.quarantine, s.published} {
		path, err := objectPath(root, key)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}
