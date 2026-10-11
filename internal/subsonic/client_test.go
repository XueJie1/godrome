package subsonic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProtocol(t *testing.T) {
	salts := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		salt := q.Get("s")
		token := md5.Sum([]byte("secret" + salt))
		if len(salt) != 32 || salts[salt] || q.Get("t") != hex.EncodeToString(token[:]) || q.Get("u") != "用户" || q.Get("p") != "" {
			t.Errorf("invalid or reused token auth")
		}
		salts[salt] = true
		if !strings.HasPrefix(r.URL.Path, "/music/prefix/rest/") {
			t.Errorf("prefix lost: %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/music/prefix/rest/ping.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case "/music/prefix/rest/getAlbumList2.view":
			if q.Get("size") != "100" || q.Get("offset") != "200" {
				t.Error("wrong page")
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"专辑"}]}}}`)
		case "/music/prefix/rest/getAlbum.view":
			if q.Get("id") != "a" {
				t.Error("wrong album")
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","album":{"id":"a","song":[{"id":"s","title":"歌曲","duration":30}]}}}`)
		case "/music/prefix/rest/search3.view":
			if q.Get("query") != "空 格&" || q.Get("songCount") != "100" {
				t.Error("query not encoded")
			}
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[{"id":"s"}]}}}`)
		default:
			t.Error("unexpected path")
		}
	}))
	defer server.Close()
	c, err := New(server.URL+"/music/prefix/", "用户", "secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	albums, err := c.Albums(ctx, 200)
	if err != nil || len(albums) != 1 {
		t.Fatalf("albums: %v", err)
	}
	album, err := c.Album(ctx, "a")
	if err != nil || len(album.Songs) != 1 {
		t.Fatalf("album: %v", err)
	}
	songs, err := c.Search(ctx, "空 格&")
	if err != nil || len(songs) != 1 {
		t.Fatalf("search: %v", err)
	}
	stream, err := c.StreamURL("s")
	if err != nil || !strings.Contains(stream, "format=raw") || !strings.Contains(stream, "/music/prefix/rest/stream.view") {
		t.Fatal("bad stream URL")
	}
}
func TestErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"auth", `{"subsonic-response":{"status":"failed","error":{"code":40,"message":"secret https://leak?t=token"}}}`, 200, "认证失败"},
		{"http", "secret", 503, "HTTP 503"},
		{"invalid", "secret", 200, "有效的 Subsonic JSON"},
		{"missing status", `{}`, 200, "Subsonic 请求失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			c, _ := New(s.URL, "user", "secret")
			err := c.Ping(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") {
				t.Fatalf("unsafe/wrong error: %v", err)
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	c, _ := New(s.URL, "user", "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	c.http.Timeout = 20 * time.Millisecond
	if err := c.Ping(context.Background()); err == nil || err.Error() != "服务器请求超时" {
		t.Fatalf("timeout: %v", err)
	}
}
func TestRejectUnsafeURLAndRedirect(t *testing.T) {
	for _, s := range []string{"file:///tmp/test", "https://user:secret@server", "https://server/?token=a", "invalid", "https://server/#x"} {
		if _, err := New(s, "user", "pass"); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	}))
	defer s.Close()
	c, _ := New(s.URL, "u", "password")
	if err := c.Ping(context.Background()); err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "t=") {
		t.Fatalf("redirect: %v", err)
	}
}
