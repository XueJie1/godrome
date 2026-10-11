package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"godrome/internal/config"
	"godrome/internal/player"
	"godrome/internal/subsonic"
)

type fakePlayer struct {
	loads            []string
	generation       uint64
	paused           bool
	volume, position float64
	events           chan player.Event
}

func (p *fakePlayer) Load(s string, g uint64) error {
	p.loads = append(p.loads, s)
	p.generation = g
	return nil
}
func (p *fakePlayer) Pause(b bool) error          { p.paused = b; return nil }
func (p *fakePlayer) Seek(s float64) error        { p.position = s; return nil }
func (p *fakePlayer) SetVolume(s float64) error   { p.volume = s; return nil }
func (*fakePlayer) Stop() error                   { return nil }
func (*fakePlayer) SetVisible(bool) error         { return nil }
func (p *fakePlayer) Events() <-chan player.Event { return p.events }
func (*fakePlayer) Close()                        {}
func testApp(t *testing.T) (*app, *fakePlayer, chan func()) {
	t.Helper()
	p := &fakePlayer{}
	a := newApp(config.Defaults(), "", p)
	updates := make(chan func(), 64)
	a.post = func(fn func()) { updates <- fn }
	t.Cleanup(a.close)
	return a, p, updates
}
func waitUpdate(t *testing.T, updates chan func()) {
	t.Helper()
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("missing UI result")
	}
}
func TestPlaybackLabelMatchesAcceptanceCopy(t *testing.T) {
	labels := map[player.State]string{
		player.Idle:      "待播放",
		player.Loading:   "加载中",
		player.Playing:   "播放中",
		player.Paused:    "已暂停",
		player.Buffering: "缓冲中",
		player.Ended:     "播放结束",
		player.Failed:    "播放失败",
	}
	if len(labels) != 7 {
		t.Fatal("playback label missing a state")
	}
	for state, label := range labels {
		if playbackLabel(state) != label {
			t.Fatalf("state %d label %q", state, playbackLabel(state))
		}
	}
}

func TestLoginAndOnlineViews(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/ping.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case "/rest/getAlbumList2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"测试专辑"}]}}}`)
		case "/rest/getAlbum.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","album":{"name":"测试专辑","song":[{"id":"1","title":"第一首"},{"id":"2","title":"第二首"}]}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	a, p, updates := testApp(t)
	tt := ui.NewTester(a.view, 1080, 720)
	for _, field := range []struct{ label, value string }{{"服务器地址", s.URL}, {"用户名", "user"}, {"密码", "secret"}} {
		if err := tt.Click(field.label); err != nil {
			t.Fatal(err)
		}
		tt.Type(field.value)
	}
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	if !a.busy {
		t.Fatalf("login not started: server=%q username=%q password-set=%t error=%s", a.server, a.username, a.password != "", a.errorText)
	}
	waitUpdate(t, updates)
	waitUpdate(t, updates)
	tt.Frame()
	snapshot(t, tt, "albums.png")
	if a.password != "" || !tt.HasText("测试专辑") {
		t.Fatalf("login did not reach albums: %v", tt.Texts())
	}
	if err := tt.Click("测试专辑"); err != nil {
		t.Fatal(err)
	}
	if a.screen != screenAlbum {
		r, _ := tt.Find("测试专辑")
		t.Fatalf("open not activated: box=%+v selected=%d screen=%s texts=%v", r, a.selectedAlbum, a.screen, tt.Texts())
	}
	waitUpdate(t, updates)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(p.loads) != 1 || len(a.play.queue.Songs) != 2 || a.play.queue.Index != 0 {
		t.Fatal("play did not replace queue")
	}
	a.onPlayback(player.Event{Generation: a.generation, State: player.Playing, Duration: 20})
	tt.Frame()
	if err := tt.Click("暂停"); err != nil || !p.paused {
		t.Fatalf("pause: %v", err)
	}
	a.onPlayback(player.Event{Generation: a.generation, State: player.Ended})
	tt.Frame()
	if a.play.queue.Index != 1 || len(p.loads) != 2 {
		t.Fatal("EOF did not advance")
	}
	a.onPlayback(player.Event{Generation: a.generation, State: player.Failed, Err: fmt.Errorf("测试播放错误")})
	tt.Frame()
	if !tt.HasText("测试播放错误") || len(a.play.queue.Songs) != 2 {
		t.Fatal("error lost queue or wasn't shown")
	}
	if err := tt.Click("重试"); err != nil || len(p.loads) != 3 {
		t.Fatal("retry")
	}
	snapshot(t, tt, "queue.png")
	tt.SetDark(true)
	tt.SetScale(1.5)
	tt.Frame()
	snapshot(t, tt, "dark-150.png")
	if err := tt.Click("退出登录"); err != nil || a.client != nil || len(a.play.queue.Songs) != 0 {
		t.Fatal("logout")
	}
}

func snapshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("GODROME_UI_EVIDENCE")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
func TestSearchDebounceCancellationAndStaleResults(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	cancelled := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("query") == "old" {
			close(started)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[{"id":"new","title":"新结果"}]}}}`)
	}))
	defer s.Close()
	a, _, updates := testApp(t)
	a.client, _ = subsonic.New(s.URL, "u", "p")
	a.query = "not sent"
	a.search()
	a.query = "old"
	a.search()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("search not started")
	}
	a.query = "new"
	a.search()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("old request not canceled")
	}
	for len(a.songs) == 0 {
		waitUpdate(t, updates)
	}
	if a.songs[0].ID != "new" || calls.Load() != 2 {
		t.Fatal("debounce or stale guard")
	}
	old := a.generation
	a.generation++
	a.onPlayback(player.Event{Generation: old, State: player.Ended})
	if a.playback.State == player.Ended {
		t.Fatal("stale player result accepted")
	}
}
func TestVirtualSongList(t *testing.T) {
	a, _, _ := testApp(t)
	a.screen = screenLocal
	for i := range 10000 {
		a.songs = append(a.songs, subsonic.Song{ID: fmt.Sprint(i), Title: fmt.Sprintf("曲目 %d", i)})
	}
	tt := ui.NewTester(a.view, 1080, 720)
	if len(tt.Texts()) > 220 {
		t.Fatalf("not virtualized: %d texts", len(tt.Texts()))
	}
	a.songsList.ScrollTo(9999, ui.End)
	tt.Frame()
	if !tt.HasText("曲目 9999") {
		t.Fatal("last virtual row inaccessible")
	}
	tt.Key(0, ui.KeyTab)
	tt.SetDark(true)
	tt.SetScale(2)
	tt.Frame()
}

func TestKeyboardLoginBrowseQueueAndControls(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/ping.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case "/rest/getAlbumList2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"专辑 A"},{"id":"b","name":"专辑 B"}]}}}`)
		case "/rest/getAlbum.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","album":{"name":"专辑 B","song":[{"id":"1","title":"歌曲一"},{"id":"2","title":"歌曲二"},{"id":"3","title":"歌曲三"}]}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	a, p, updates := testApp(t)
	tt := ui.NewTester(a.view, 1080, 720)
	focus := func(label string) {
		t.Helper()
		for range 64 {
			if tt.Focused(label) {
				return
			}
			tt.Key(0, ui.KeyTab)
		}
		t.Fatalf("keyboard cannot reach %s", label)
	}
	var password [24]byte
	if _, err := rand.Read(password[:]); err != nil {
		t.Fatal("cannot generate fixture session")
	}
	for _, field := range []struct{ label, value string }{{"服务器地址", s.URL}, {"用户名", "fixture"}, {"密码", hex.EncodeToString(password[:])}} {
		focus(field.label)
		tt.Type(field.value)
	}
	tt.Key(0, ui.KeyEnter)
	waitUpdate(t, updates)
	waitUpdate(t, updates)
	tt.Frame()
	if a.client == nil || a.password != "" {
		t.Fatal("keyboard login failed")
	}
	selectList := func(selected func() bool) {
		t.Helper()
		for range 64 {
			tt.Key(0, ui.KeyTab)
			// The native sidebar is one tab stop and uses the arrows itself.
			if tt.Focused("曲库") || tt.Focused("专辑") || tt.Focused("喜爱") || tt.Focused("歌单") {
				continue
			}
			tt.Key(0, ui.KeyDown)
			if selected() {
				return
			}
		}
		t.Fatal("keyboard cannot reach list")
	}
	selectList(func() bool { return a.selectedAlbum >= 0 })
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	waitUpdate(t, updates)
	tt.Frame()
	if a.screen != screenAlbum || len(a.songs) != 3 {
		t.Fatal("keyboard did not open album")
	}
	selectList(func() bool { return a.selectedSong >= 0 })
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.play.queue.Index != 1 || len(p.loads) != 1 {
		t.Fatal("keyboard did not play selected song")
	}
	selectList(func() bool { return a.selectedQueue == 2 })
	tt.Key(0, ui.KeyEnter)
	if a.play.queue.Index != 2 || len(p.loads) != 2 {
		t.Fatal("keyboard did not choose queue song")
	}
	a.onPlayback(player.Event{Generation: a.generation, State: player.Playing, Duration: 20, Position: 5})
	tt.Frame()
	focus("顺序播放")
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("暂停") {
		t.Fatal("keyboard cannot reach 暂停")
	}
	tt.Key(0, ui.KeyEnter)
	if !p.paused {
		t.Fatal("keyboard pause failed")
	}
	focus("播放进度")
	tt.Key(0, ui.KeyRight)
	if p.position <= 5 {
		t.Fatal("keyboard seek failed")
	}
	focus("退出登录")
	tt.Key(0, ui.KeyEnter)
	if a.client != nil || len(a.play.queue.Songs) != 0 {
		t.Fatal("keyboard logout failed")
	}
}
