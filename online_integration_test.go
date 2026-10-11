//go:build cgo

package main

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"godrome/internal/config"
	"godrome/internal/player"
)

// Credentials are injected into this test's environment, never stored in source/config.
// This is read-only: no server playlists, favorites, users or library metadata are modified.
func TestRealNavidrome(t *testing.T) {
	server := os.Getenv("GODROME_TEST_SERVER")
	if server == "" {
		t.Skip("set GODROME_TEST_SERVER, GODROME_TEST_USER and GODROME_TEST_PASSWORD for live read-only validation")
	}
	p := player.New(player.Options{AudioOutput: "null"})
	a := newApp(config.Defaults(), "", p)
	updates := make(chan func(), 128)
	a.post = func(fn func()) { updates <- fn }
	a.listen()
	defer a.close()
	a.server = server
	a.username = os.Getenv("GODROME_TEST_USER")
	a.password = os.Getenv("GODROME_TEST_PASSWORD")
	tt := ui.NewTester(a.view, 1080, 720)
	pump := func(done func() bool) {
		t.Helper()
		deadline := time.NewTimer(35 * time.Second)
		defer deadline.Stop()
		for !done() {
			select {
			case fn := <-updates:
				fn()
				tt.Frame()
				if a.errorText != "" {
					t.Fatal(a.errorText)
				}
			case <-deadline.C:
				t.Fatal("timed out waiting for the real server/player")
			}
		}
	}
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	pump(func() bool { return a.screen == screenAlbums && !a.busy })
	if a.client == nil || a.password != "" {
		t.Fatal("live login did not establish session")
	}
	if len(a.albums) == 0 {
		t.Fatal("test server has no albums; stream validation requires a sample library")
	}
	if err := tt.Click(a.albums[0].Name); err != nil {
		t.Fatal(err)
	}
	pump(func() bool { return a.screen == screenAlbum && !a.busy })
	if len(a.songs) == 0 {
		t.Fatal("test album has no songs")
	}
	title := a.songs[0].Title
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	pump(func() bool { return a.playback.State == player.Playing && a.playback.Duration > 0 })
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	pump(func() bool { return a.playback.State == player.Paused })
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	pump(func() bool { return a.playback.State == player.Playing })
	// A real pointer interaction with the progress slider, not a direct state mutation.
	r, ok := tt.Find("播放进度")
	if !ok || r.W < 1 {
		t.Fatal("progress slider missing")
	}
	tt.ClickAt(r.X+r.W*.35, r.Y+r.H/2)
	target := a.position
	pump(func() bool { return a.playback.Position >= target-.5 })
	if err := tt.Click("搜索歌曲"); err != nil {
		t.Fatal(err)
	}
	tt.Type(title)
	pump(func() bool { return a.screen == screenSearch && !a.busy })
	if len(a.songs) == 0 {
		t.Fatal("live song search returned no results")
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	if a.playback.State != player.Idle {
		t.Fatal("stop not reflected")
	}
	t.Log("real Navidrome: login, album list/detail, search, raw stream, pause/resume, seek and stop passed; audio output=null")
}
