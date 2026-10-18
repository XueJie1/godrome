package subsonic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeCache struct {
	mu   sync.Mutex
	data map[string][]byte
	keys []string
}

func newFakeCache() *fakeCache { return &fakeCache{data: map[string][]byte{}} }

func (f *fakeCache) Get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.data[key]
	return data, ok
}

func (f *fakeCache) Put(key string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = append([]byte(nil), data...)
	f.keys = append(f.keys, key)
	return nil
}

func (f *fakeCache) Path(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; !ok {
		return "", false
	}
	return "/cache/cover.png", true
}

func (f *fakeCache) allKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

const albumJSON = `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"专辑"}]}}}`

func TestMetadataCacheAndOfflineFallback(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, albumJSON)
	}))
	defer server.Close()

	c, err := New(server.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeCache()
	c.UseCache(fc, true)
	ctx := context.Background()

	for range 2 {
		albums, err := c.Albums(ctx, 0)
		if err != nil || len(albums) != 1 {
			t.Fatalf("albums: %v", err)
		}
	}
	if requests != 2 {
		t.Fatalf("metadata should stay network-first: %d requests", requests)
	}
	if !c.OfflineAvailable() {
		t.Fatal("first album page was not cached")
	}
	for _, key := range fc.allKeys() {
		for _, secret := range []string{"secret", "&t=", "&s=", "&u=", "http://", server.URL} {
			if strings.Contains(key, secret) {
				t.Fatalf("cache key leaked %q: %s", secret, key)
			}
		}
	}

	server.Close()
	albums, err := c.Albums(ctx, 0)
	if err != nil || len(albums) != 1 {
		t.Fatalf("offline fallback: %v albums=%d", err, len(albums))
	}
	if !c.Stale() {
		t.Fatal("fallback did not mark the result stale")
	}
	if requests != 2 {
		t.Fatalf("offline fallback made a request: %d", requests)
	}

	c.SetOfflineCache(false)
	if _, err := c.Albums(ctx, 0); err == nil {
		t.Fatal("offline cache still served after being disabled")
	}
}

func TestAuthFailureDoesNotUseCache(t *testing.T) {
	var fail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			fmt.Fprint(w, `{"subsonic-response":{"status":"failed","error":{"code":40}}}`)
			return
		}
		fmt.Fprint(w, albumJSON)
	}))
	defer server.Close()
	c, _ := New(server.URL, "user", "secret")
	c.UseCache(newFakeCache(), true)
	if _, err := c.Albums(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := c.Albums(context.Background(), 0); !errors.Is(err, ErrAuth) {
		t.Fatalf("auth failure was hidden by cache: %v", err)
	}
}

func TestHTTP4xxDoesNotUseCache(t *testing.T) {
	var fail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, albumJSON)
	}))
	defer server.Close()
	c, _ := New(server.URL, "user", "secret")
	c.UseCache(newFakeCache(), true)
	if _, err := c.Albums(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := c.Albums(context.Background(), 0); err == nil {
		t.Fatal("HTTP 404 was hidden by cache")
	}
}

func TestCoverIsCached(t *testing.T) {
	var requests int
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	imageBytes := buf.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(imageBytes)
	}))
	c, _ := New(server.URL, "user", "secret")
	fc := newFakeCache()
	c.UseCache(fc, true)
	ctx := context.Background()

	first, err := c.Cover(ctx, "album-1")
	if err != nil || !bytes.Equal(first, imageBytes) {
		t.Fatalf("cover: %v", err)
	}
	second, err := c.Cover(ctx, "album-1")
	if err != nil || !bytes.Equal(second, imageBytes) {
		t.Fatalf("cached cover: %v", err)
	}
	if requests != 1 {
		t.Fatalf("cover fetched %d times", requests)
	}
	if path, err := c.CoverPath(ctx, "album-1"); err != nil || path != "/cache/cover.png" {
		t.Fatalf("cover path %q err=%v", path, err)
	}
	server.Close()
	if _, err := c.Cover(ctx, "album-1"); err != nil {
		t.Fatalf("cover not served offline: %v", err)
	}
}

func TestCoverRejectsNonImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>not an image</html>")
	}))
	defer server.Close()
	c, _ := New(server.URL, "user", "secret")
	fc := newFakeCache()
	c.UseCache(fc, true)
	if _, err := c.Cover(context.Background(), "album-1"); err == nil {
		t.Fatal("non-image cover accepted")
	}
	if len(fc.allKeys()) != 0 {
		t.Fatal("bad cover was written to the cache")
	}
}

func TestIsNetworkFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{ErrNetwork, true},
		{ErrTimeout, true},
		{&HTTPError{Code: 503}, true},
		{&HTTPError{Code: 404}, false},
		{ErrAuth, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{errors.New("other"), false},
	} {
		if got := IsNetworkFailure(tc.err); got != tc.want {
			t.Errorf("IsNetworkFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
