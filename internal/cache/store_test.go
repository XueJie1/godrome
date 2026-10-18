package cache

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jsonBytes(size int) []byte {
	data := bytes.Repeat([]byte("a"), size-2)
	return append(append([]byte{'{'}, data...), '}')
}

func TestPutGetPathAndClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := pngBytes(t)
	if err := s.Put("cover|server|user|album-1|256", data); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("cover|server|user|album-1|256")
	if !ok || !bytes.Equal(got, data) {
		t.Fatal("cache miss after put")
	}
	path, ok := s.Path("cover|server|user|album-1|256")
	if !ok || !strings.HasSuffix(path, ".png") {
		t.Fatalf("path %q ok=%v", path, ok)
	}
	if !strings.HasPrefix(filepath.Base(path), strings.TrimSuffix(filepath.Base(path), ".png")) || len(filepath.Base(path)) != 36 {
		t.Fatalf("file name not hashed: %q", filepath.Base(path))
	}
	if strings.Contains(filepath.Base(path), "server") || strings.Contains(filepath.Base(path), "album") {
		t.Fatalf("key leaked into file name: %q", filepath.Base(path))
	}
	if onDisk, err := os.ReadFile(path); err != nil || !bytes.Equal(onDisk, data) {
		t.Fatal("file does not hold the cached bytes")
	}
	used, limit := s.Stats()
	if used != int64(len(data)) || limit != 1<<20 {
		t.Fatalf("stats used=%d limit=%d", used, limit)
	}
	// A stray file that was never indexed must not survive Clear.
	if err := os.WriteFile(filepath.Join(dir, "stray.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("cover|server|user|album-1|256"); ok {
		t.Fatal("clear left data")
	}
	if used, _ := s.Stats(); used != 0 {
		t.Fatalf("used after clear %d", used)
	}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.Name() != indexFile {
			t.Fatalf("clear left %q", f.Name())
		}
	}
}

func TestLRUEviction(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 250)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, b, c := jsonBytes(100), jsonBytes(101), jsonBytes(102)
	if err := s.Put("a", a); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("b", b); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("a"); !ok {
		t.Fatal("a missing")
	}
	if err := s.Put("c", c); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("a"); !ok {
		t.Fatal("recently used entry was evicted")
	}
	if _, ok := s.Get("c"); !ok {
		t.Fatal("new entry was evicted")
	}
	if _, ok := s.Get("b"); ok {
		t.Fatal("least recently used entry survived")
	}
	if used, _ := s.Stats(); used > 250 {
		t.Fatalf("still over limit: %d", used)
	}
}

func TestLimitChangeTrims(t *testing.T) {
	s, err := Open(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, key := range []string{"a", "b", "c", "d"} {
		if err := s.Put(key, jsonBytes(100)); err != nil {
			t.Fatal(err)
		}
	}
	s.SetLimit(150)
	if used, limit := s.Stats(); limit != 150 || used > 150 {
		t.Fatalf("used=%d limit=%d", used, limit)
	}
}

func TestIndexRecoveryAndOrphans(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	data := jsonBytes(40)
	if err := s.Put("known", data); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// A file placed in the directory without an index entry is adopted.
	orphan := filepath.Join(dir, hashKey("orphan")+".json")
	if err := os.WriteFile(orphan, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// A damaged index must not wipe the directory.
	if err := os.WriteFile(filepath.Join(dir, indexFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"known", "orphan"} {
		if _, ok := s.Get(key); !ok {
			t.Fatalf("%s was not recovered", key)
		}
	}
	s.Close()

	// An index entry whose file disappeared is dropped.
	s, err = Open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, hashKey("known")+".json")); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.Get("known"); ok {
		t.Fatal("missing file still reported as a hit")
	}
	if _, ok := s.Get("orphan"); !ok {
		t.Fatal("other entries were lost")
	}
}

func TestConcurrentUse(t *testing.T) {
	s, err := Open(t.TempDir(), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := string(rune('a' + w))
				_ = s.Put(key, jsonBytes(50+i%50))
				s.Get(key)
				s.Stats()
				s.Path(key)
			}
		}(w)
	}
	wg.Wait()
	if used, limit := s.Stats(); used > limit {
		t.Fatalf("over limit: %d > %d", used, limit)
	}
}
