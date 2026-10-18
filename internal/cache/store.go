// Package cache is a small bounded disk cache for regenerable data.
//
// One Store owns one directory. Keys are hashed before they touch the disk,
// so server addresses, usernames and media IDs do not appear in file names
// or in index.json. Writes are atomic, the index is rebuilt from the
// directory when it is missing or damaged, and least-recently-used entries
// are evicted once the directory exceeds its byte limit.
package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	indexFile  = "index.json"
	indexEvery = 30 * time.Second
	// Trim a little below the limit so a burst of writes does not evict
	// on every Put.
	trimTo = 9.0 / 10.0
)

var errClosed = errors.New("cache is closed")

type entry struct {
	Ext  string `json:"ext"`
	Size int64  `json:"size"`
	Used int64  `json:"used"` // Unix nanoseconds, for LRU eviction.
}

type diskIndex struct {
	Version int              `json:"version"`
	Entries map[string]entry `json:"entries"`
}

// Store is safe for concurrent use. Reads hold the lock for the duration of
// the file read, which keeps eviction from removing a file mid-read.
type Store struct {
	dir string

	mu      sync.Mutex
	limit   int64
	entries map[string]entry
	used    int64
	dirty   bool
	gen     uint64 // bumped by Clear so an in-flight Put cannot resurrect data
	closed  bool

	done chan struct{}
}

// Open prepares dir and loads its index. A missing or damaged index is
// rebuilt from the files that are already present.
func Open(dir string, limit int64) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	s := &Store{dir: dir, limit: limit, entries: map[string]entry{}, done: make(chan struct{})}
	s.load()
	s.mu.Lock()
	s.trimLocked()
	s.mu.Unlock()
	go s.flushLoop()
	return s, nil
}

// Get returns the data for key and marks the entry as recently used.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false
	}
	h := hashKey(key)
	e, ok := s.entries[h]
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(s.file(h, e.Ext))
	if err != nil {
		s.dropLocked(h, e)
		return nil, false
	}
	e.Used = time.Now().UnixNano()
	s.entries[h] = e
	s.dirty = true
	return data, true
}

// Path returns the on-disk file for key, for file:// consumers. It also
// counts as a use, so the cover of the track being played survives a trim.
func (s *Store) Path(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", false
	}
	h := hashKey(key)
	e, ok := s.entries[h]
	if !ok {
		return "", false
	}
	e.Used = time.Now().UnixNano()
	s.entries[h] = e
	s.dirty = true
	return s.file(h, e.Ext), true
}

// Put stores data under key. Errors are for callers that care; a cache
// failure never has to fail the request that produced the data.
func (s *Store) Put(key string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	h := hashKey(key)
	ext := extension(data)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errClosed
	}
	gen := s.gen
	s.mu.Unlock()

	tmp, err := os.CreateTemp(s.dir, "put-*.tmp")
	if err != nil {
		return err
	}
	temp := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(temp)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	path := s.file(h, ext)
	if err = os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		os.Remove(path)
		return errClosed
	}
	if s.gen != gen {
		// Clear ran while this entry was being written.
		os.Remove(path)
		return nil
	}
	if old, ok := s.entries[h]; ok {
		s.used -= old.Size
		if old.Ext != ext {
			os.Remove(s.file(h, old.Ext))
		}
	}
	s.entries[h] = entry{Ext: ext, Size: int64(len(data)), Used: time.Now().UnixNano()}
	s.used += int64(len(data))
	s.dirty = true
	s.trimLocked()
	return nil
}

// Clear removes every cached file and resets the index.
func (s *Store) Clear() error {
	s.mu.Lock()
	s.gen++
	old := s.entries
	s.entries = map[string]entry{}
	s.used = 0
	s.dirty = true
	s.mu.Unlock()

	var firstErr error
	for h, e := range old {
		if err := os.Remove(s.file(h, e.Ext)); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	// Also remove files the index did not know about.
	if files, err := os.ReadDir(s.dir); err == nil {
		for _, f := range files {
			if f.IsDir() || f.Name() == indexFile {
				continue
			}
			if err := os.Remove(filepath.Join(s.dir, f.Name())); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := s.flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Stats reports the bytes in use and the current limit.
func (s *Store) Stats() (used, limit int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used, s.limit
}

// SetLimit changes the byte budget and trims immediately.
func (s *Store) SetLimit(limit int64) {
	if limit < 1 {
		limit = 1
	}
	s.mu.Lock()
	s.limit = limit
	s.trimLocked()
	s.dirty = true
	s.mu.Unlock()
}

// Close stops the background index writer and flushes the index.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	return s.flush()
}

func (s *Store) load() {
	var idx diskIndex
	if data, err := os.ReadFile(filepath.Join(s.dir, indexFile)); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	entries := make(map[string]entry, len(idx.Entries))
	if idx.Version == 1 {
		for h, e := range idx.Entries {
			if !validHash(h) || !validExt(e.Ext) {
				continue
			}
			info, err := os.Stat(s.file(h, e.Ext))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			e.Size = info.Size()
			entries[h] = e
		}
	}
	files, err := os.ReadDir(s.dir)
	if err == nil {
		for _, f := range files {
			name := f.Name()
			if name == indexFile {
				continue
			}
			if f.IsDir() {
				continue
			}
			if strings.HasSuffix(name, ".tmp") {
				os.Remove(filepath.Join(s.dir, name))
				continue
			}
			h, ext, ok := splitName(name)
			if !ok {
				continue
			}
			info, err := f.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if e, ok := entries[h]; ok && e.Ext == ext {
				continue
			}
			// A file the index does not know, or one whose extension
			// changed. Adopt it with its modification time as the last use.
			entries[h] = entry{Ext: ext, Size: info.Size(), Used: info.ModTime().UnixNano()}
		}
	}
	s.entries = entries
	s.used = 0
	for _, e := range entries {
		s.used += e.Size
	}
	s.dirty = true
}

func (s *Store) flushLoop() {
	t := time.NewTicker(indexEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			_ = s.flush()
		case <-s.done:
			return
		}
	}
}

// flush writes the index atomically. The dirty flag is cleared under the
// same lock that snapshots the entries, so a Put after the snapshot sets it
// again and is picked up by the next flush.
func (s *Store) flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(diskIndex{Version: 1, Entries: s.entries})
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "index-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err = os.Rename(name, filepath.Join(s.dir, indexFile)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (s *Store) trimLocked() {
	if s.used <= s.limit || len(s.entries) == 0 {
		return
	}
	target := int64(float64(s.limit) * trimTo)
	type victim struct {
		hash string
		used int64
	}
	list := make([]victim, 0, len(s.entries))
	for h, e := range s.entries {
		list = append(list, victim{h, e.Used})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].used < list[j].used })
	for _, v := range list {
		if s.used <= target {
			break
		}
		if e, ok := s.entries[v.hash]; ok {
			s.dropLocked(v.hash, e)
		}
	}
}

func (s *Store) dropLocked(h string, e entry) {
	os.Remove(s.file(h, e.Ext))
	delete(s.entries, h)
	s.used -= e.Size
	s.dirty = true
}

func (s *Store) file(hash, ext string) string {
	return filepath.Join(s.dir, hash+"."+ext)
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

func validHash(h string) bool {
	if len(h) != 32 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validExt(ext string) bool {
	if ext == "" || len(ext) > 8 {
		return false
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func splitName(name string) (hash, ext string, ok bool) {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return "", "", false
	}
	hash, ext = name[:i], name[i+1:]
	if !validHash(hash) || !validExt(ext) {
		return "", "", false
	}
	return hash, ext, true
}

// extension picks a suffix from the bytes, so a later Path can be used as a
// file:// URL that image and media consumers understand.
func extension(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return "json"
	}
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "bin"
	}
}
