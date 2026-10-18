package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/zalando/go-keyring"
	"godrome/internal/cache"
	"godrome/internal/config"
	"godrome/internal/cover"
	"godrome/internal/player"
	"godrome/internal/subsonic"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

type fakePlayer struct {
	loads            []string
	generation       uint64
	paused           bool
	volume, position float64
	equalizer        string
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
func (p *fakePlayer) SetEqualizer(s string) error { p.equalizer = s; return nil }
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
func TestSettingsLookAndEqualizer(t *testing.T) {
	a, p, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	var applied mygo.ThemeSource
	a.setAppearance = func(s mygo.ThemeSource) { applied = s }
	tt := ui.NewTester(a.view, 1080, 720)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "settings-look.png")
	if err := tt.Click("自定义颜色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Hue") && !tt.HasText("Saturation and brightness") {
		t.Fatalf("color picker did not open: %v open=%v", tt.Texts(), a.accentOpen)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !tt.HasText("主题色") || !tt.HasText("声音") || !tt.HasText("账号") {
		t.Fatalf("settings copy: %v", tt.Texts())
	}
	if err := tt.Click("蓝"); err != nil {
		t.Fatal(err)
	}
	if a.accent != ui.Hex("#0a84ff") {
		t.Fatalf("accent %v", a.accent)
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "settings-dark.png")
	if a.appearance != "dark" || applied != mygo.ThemeDark {
		t.Fatalf("appearance %q applied %q", a.appearance, applied)
	}
	if err := tt.Click("声音"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("均衡器预设"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("摇滚"); err != nil {
		t.Fatal(err)
	}
	if a.eqPreset != eqRock || a.eq != presetCurve(eqRock, eqCurve{}) || p.equalizer == "" {
		t.Fatalf("preset %q filter %q", a.eqPreset, p.equalizer)
	}
	if err := tt.Click("32 Hz"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if a.eqPreset != eqCustom || a.eq[0] == presetCurve(eqRock, eqCurve{})[0] {
		t.Fatalf("band drag stayed on preset %q gain %v", a.eqPreset, a.eq[0])
	}
	if p.equalizer != a.eq.filter() {
		t.Fatalf("player filter %q", p.equalizer)
	}
	snapshot(t, tt, "settings.png")
	if err := tt.Click("账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("退出登录"); err != nil || a.client != nil {
		t.Fatalf("logout: %v screen=%s", err, a.screen)
	}
	if a.eqPreset != eqCustom || a.accent != ui.Hex("#0a84ff") {
		t.Fatal("logout cleared look or equalizer")
	}
}

func TestMuteRemembersVolume(t *testing.T) {
	a, p, _ := testApp(t)
	a.screen = screenLibrary
	tt := ui.NewTester(a.view, 1080, 720)
	if err := tt.Click("静音"); err != nil || a.volume != 0 || p.volume != 0 {
		t.Fatalf("mute: %v volume=%v player=%v", err, a.volume, p.volume)
	}
	if err := tt.Click("静音"); err != nil || a.volume != 70 || p.volume != 70 {
		t.Fatalf("unmute: %v volume=%v", err, a.volume)
	}
	for range 8 {
		if tt.Focused("音量") {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	tt.Key(0, ui.KeyHome)
	if a.volume != 0 {
		t.Fatalf("drag to zero: %v", a.volume)
	}
	if err := tt.Click("静音"); err != nil || a.volume != 70 {
		t.Fatalf("unmute after drag: %v volume=%v", err, a.volume)
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
	tt := ui.NewTester(a.view, 1080, 900)
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
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.screen != screenSettings || !tt.HasText("外观") {
		t.Fatal("settings page missing")
	}
	if err := tt.Click("账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("退出登录"); err != nil || a.client != nil || len(a.play.queue.Songs) != 0 {
		t.Fatal("logout")
	}
}

func TestSavedPasswordLogsInAgain(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/ping.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case "/rest/getAlbumList2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"测试专辑"}]}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := config.Defaults()
	settings.Server = s.URL
	settings.Username = "user"
	if err := config.Save(path, settings); err != nil {
		t.Fatal(err)
	}
	if err := config.SavePassword(s.URL, "user", "secret"); err != nil {
		t.Fatal(err)
	}
	p := &fakePlayer{}
	a := newApp(settings, path, p)
	updates := make(chan func(), 64)
	a.post = func(fn func()) { updates <- fn }
	t.Cleanup(a.close)
	if !a.autoLogin || a.password == "" {
		t.Fatal("saved password was not loaded")
	}
	tt := ui.NewTester(a.view, 1080, 900)
	waitUpdate(t, updates)
	waitUpdate(t, updates)
	tt.Frame()
	if a.client == nil || a.password != "" || !tt.HasText("测试专辑") {
		t.Fatal("saved password did not log in")
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), "secret") {
		t.Fatal("password written to settings")
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("退出登录"); err != nil || a.client != nil {
		t.Fatal(err)
	}
	if _, ok, err := config.LoadPassword(s.URL, "user"); err != nil || ok {
		t.Fatal("logout kept the password")
	}
}

func TestRejectedSavedPasswordIsForgotten(t *testing.T) {
	var pings atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pings.Add(1)
		fmt.Fprint(w, `{"subsonic-response":{"status":"failed","error":{"code":40}}}`)
	}))
	defer s.Close()
	settings := config.Defaults()
	settings.Server = s.URL
	settings.Username = "user"
	if err := config.SavePassword(s.URL, "user", "secret"); err != nil {
		t.Fatal(err)
	}
	a := newApp(settings, filepath.Join(t.TempDir(), "settings.json"), &fakePlayer{})
	updates := make(chan func(), 64)
	a.post = func(fn func()) { updates <- fn }
	t.Cleanup(a.close)
	tt := ui.NewTester(a.view, 1080, 720)
	waitUpdate(t, updates)
	tt.Frame()
	if a.client != nil || a.autoLogin || a.busy || pings.Load() != 1 {
		t.Fatalf("auto login retried: client=%v busy=%t pings=%d", a.client != nil, a.busy, pings.Load())
	}
	if _, ok, err := config.LoadPassword(s.URL, "user"); err != nil || ok {
		t.Fatal("rejected password kept")
	}
	if !tt.HasText("认证失败") {
		t.Fatal(tt.Texts())
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
func TestSongSelectionClearsTheScrollbar(t *testing.T) {
	a, _, _ := testApp(t)
	a.screen = screenAlbum
	a.albumName = "专辑"
	for i := range 40 {
		a.songs = append(a.songs, subsonic.Song{ID: fmt.Sprint(i), Title: fmt.Sprintf("曲目 %d", i), Artist: "艺人", Duration: 60})
	}
	a.selectedSong = 2
	tt := ui.NewTester(a.view, 1080, 720)
	row, ok := tt.Find("曲目 2")
	if !ok {
		t.Fatal("selected song not shown")
	}
	// The thumb shows only while the pointer is over the list, and only
	// along the top of the track when the list is at its start.
	first, ok := tt.Find("曲目 0")
	if !ok {
		t.Fatal("first song not shown")
	}
	tt.Move(row.X+8, first.Y+4)
	img := tt.Image()
	y0, y1 := int(row.Y), int(row.Y+row.H)
	if y0 < 0 || y1 > img.Bounds().Dy() {
		t.Fatalf("row outside the frame: %v", row)
	}
	accent := -1
	for y := y0; y < y1; y++ {
		for x := int(row.X); x < int(row.X+row.W)+2 && x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 > 200 && g>>8 < 90 && b>>8 < 110 && x > accent {
				accent = x
			}
		}
	}
	// The thumb is black at 32% over the page, about rgb(173, 173, 173).
	// Sample it beside the first row, inside the list, so the header
	// button is not mistaken for the thumb.
	bar := -1
	x0, x1 := int(row.X+row.W), int(row.X+row.W)+28
	for y := int(first.Y); y < int(first.Y+first.H); y++ {
		if y < 0 {
			continue
		}
		for x := x0; x < x1 && x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			if r > 160 && r < 190 && absInt(int(r)-int(g)) < 6 && absInt(int(g)-int(b)) < 6 {
				if bar < 0 || x < bar {
					bar = x
				}
			}
		}
	}
	if accent < 0 {
		t.Fatal("selection highlight not painted")
	}
	if bar < 0 {
		t.Fatalf("scrollbar not painted while the pointer is over the list; highlight ends at x=%d row=%v", accent, row)
	}
	if accent+6 >= bar {
		t.Fatalf("selection reaches the scrollbar: highlight ends at %d, thumb starts at %d, row %v", accent, bar, row)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestHeadersDoNotRepeatTheSidebar(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenAlbums
	a.albums = []subsonic.Album{{ID: "a", Name: "测试专辑"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if tt.HasText("资料库") {
		t.Fatalf("album header repeats the sidebar: %v", tt.Texts())
	}
	a.screen = screenLibrary
	a.songs = []subsonic.Song{{ID: "1", Title: "第一首", Artist: "艺人"}}
	tt.Frame()
	if tt.HasText("资料库") {
		t.Fatalf("library header repeats the sidebar: %v", tt.Texts())
	}
}

func TestAlbumPageHasNoReturnButton(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenAlbum
	a.albumName = "测试专辑"
	a.songs = []subsonic.Song{{ID: "1", Title: "第一首", Artist: "艺人"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if tt.HasText("返回专辑") {
		t.Fatal("album page still shows a return button")
	}
}

func TestPlaceholderNamesThePageOnce(t *testing.T) {
	a, _, _ := testApp(t)
	a.showPlaceholder(screenPlaylists, "歌单")
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("还没有这一页。从左侧回到曲库。") {
		t.Fatalf("placeholder copy missing: %v", tt.Texts())
	}
	n := 0
	for _, text := range tt.Texts() {
		if text == "歌单" || text == "即将推出" {
			n++
		}
	}
	// The sidebar item is the other "歌单". The page itself says it once.
	if n != 2 || tt.HasText("即将推出") {
		t.Fatalf("page name shown %d times: %v", n, tt.Texts())
	}
}

func TestStarWritesAndStarredPageReads(t *testing.T) {
	var starred, unstarred atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/rest/getStarred2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","starred2":{"artist":[{"id":"ar","name":"艺人","starred":"2023-03-22T01:51:06Z"}],"album":[{"id":"al","name":"喜爱专辑","artist":"艺人","starred":"2023-03-22T01:51:06Z"}],"song":[{"id":"s","title":"喜爱歌曲","artist":"艺人","starred":"2023-03-27T09:45:27Z"}]}}}`)
		case "/rest/getAlbumList2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"al","name":"喜爱专辑","artist":"艺人"}]}}}`)
		case "/rest/star.view":
			if q.Get("id") != "local-song" || q.Get("albumId") != "" {
				t.Errorf("star query: %s", r.URL.RawQuery)
			}
			starred.Add(1)
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case "/rest/unstar.view":
			if q.Get("albumId") != "al" && q.Get("artistId") != "ar" && q.Get("id") != "s" {
				t.Errorf("unstar query: %s", r.URL.RawQuery)
			}
			unstarred.Add(1)
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	client, err := subsonic.New(s.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	a, _, updates := testApp(t)
	a.client = client
	a.screen = screenLibrary
	a.songs = []subsonic.Song{{ID: "local-song", Title: "第一首", Artist: "艺人"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if err := tt.Click("喜欢"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	if starred.Load() != 1 || !a.likedSongs["local-song"] {
		t.Fatalf("star did not stick: calls=%d liked=%v", starred.Load(), a.likedSongs)
	}
	if err := tt.Click("喜爱"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	tt.Frame()
	if a.screen != screenStarred || !tt.HasText("喜爱歌曲") || !tt.HasText("歌曲") || !tt.HasText("专辑") || !tt.HasText("艺人") {
		t.Fatalf("starred page: screen=%s texts=%v", a.screen, tt.Texts())
	}
	if err := tt.Click("喜爱艺人"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("取消喜欢 艺人"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	if unstarred.Load() != 1 || len(a.artists) != 0 {
		t.Fatalf("artist unstar: calls=%d artists=%v", unstarred.Load(), a.artists)
	}
	if err := tt.Click("喜爱专辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("取消喜欢 喜爱专辑"); err != nil {
		t.Fatalf("album unlike: %v texts=%v", err, tt.Texts())
	}
	waitUpdate(t, updates)
	tt.Frame()
	if unstarred.Load() != 2 || tt.HasText("取消喜欢 喜爱专辑") {
		t.Fatalf("album stayed after unstar: calls=%d texts=%v", unstarred.Load(), tt.Texts())
	}
	if err := tt.Click("歌曲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("取消喜欢"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	tt.Frame()
	if unstarred.Load() != 3 || tt.HasText("喜爱歌曲") {
		t.Fatalf("song stayed after unstar: calls=%d texts=%v", unstarred.Load(), tt.Texts())
	}
}

func TestStarFailureRestoresTheMark(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer s.Close()
	client, err := subsonic.New(s.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	a, _, updates := testApp(t)
	a.client = client
	a.screen = screenLibrary
	a.songs = []subsonic.Song{{ID: "s", Title: "第一首", Artist: "艺人", Starred: "2023-03-27T09:45:27Z"}}
	a.likedSongs = map[string]bool{"s": true}
	tt := ui.NewTester(a.view, 1080, 720)
	if err := tt.Click("取消喜欢"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	tt.Frame()
	if !a.likedSongs["s"] || !tt.HasText("HTTP 503") || !tt.HasText("取消喜欢") {
		t.Fatalf("failed unstar did not restore: liked=%v error=%q texts=%v", a.likedSongs, a.errorText, tt.Texts())
	}
}

func TestAlbumCoverCentersInTheSelection(t *testing.T) {
	a, _, _ := testApp(t)
	a.screen = screenAlbums
	a.albums = []subsonic.Album{{ID: "al", Name: "测试专辑", Artist: "艺人"}}
	a.selectedAlbum = 0
	tt := ui.NewTester(a.view, 1080, 720)
	// Find returns the grid cell: its label is the album name, and it is
	// as tall as the cell. A click in the cell's side margin focuses the
	// grid without opening the album.
	cell, ok := tt.Find("测试专辑")
	if !ok || cell.H < 200 {
		t.Fatalf("album cell missing: %+v ok=%v", cell, ok)
	}
	tt.ClickAt(cell.X+6, cell.Y+cell.H/2)
	if a.screen != screenAlbums {
		t.Fatal("side click opened the album")
	}
	img := tt.Image()
	// Through the placeholder, above its icon, so the square is one color.
	y := int(cell.Y) + 28
	if y < 0 || y >= img.Bounds().Dy() {
		t.Fatalf("cover sample outside the frame: cell=%v", cell)
	}
	selL, selR := -1, -1
	coverL, coverR := -1, -1
	for x := int(cell.X) - 4; x < int(cell.X+cell.W)+4 && x < img.Bounds().Dx(); x++ {
		if x < 0 {
			continue
		}
		r, g, b, _ := img.At(x, y).RGBA()
		r, g, b = r>>8, g>>8, b>>8
		// Accent at 20% over white, or the accent border.
		if r > 220 && int(r) > int(g)+20 && int(r) > int(b)+15 {
			if selL < 0 {
				selL = x
			}
			selR = x
			continue
		}
		// The missing-cover tile is SurfaceHover, grayer than the page.
		if r > 210 && r < 245 && absInt(int(r)-int(g)) < 10 && absInt(int(g)-int(b)) < 10 {
			if coverL < 0 {
				coverL = x
			}
			coverR = x
		}
	}
	if selL < 0 || coverL < 0 || coverL < selL || coverR > selR {
		t.Fatalf("cover is not inside the selection: sel=%d..%d cover=%d..%d cell=%v", selL, selR, coverL, coverR, cell)
	}
	left, right := coverL-selL, selR-coverR
	if absInt(left-right) > 2 {
		t.Fatalf("cover is not centered in the selection: left %d, right %d", left, right)
	}
}

func TestAlbumStarSitsUnderTheTitle(t *testing.T) {
	var albumID atomic.Value
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/star.view":
			albumID.Store(r.URL.Query().Get("albumId"))
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	client, err := subsonic.New(s.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	a, _, updates := testApp(t)
	a.client = client
	a.screen = screenAlbums
	a.albums = []subsonic.Album{{ID: "al", Name: "测试专辑", Artist: "艺人"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if tt.HasText("喜欢 测试专辑") {
		t.Fatal("album grid still shows a like button")
	}
	a.screen = screenAlbum
	a.albumID = "al"
	a.albumName = "测试专辑"
	a.songs = []subsonic.Song{{ID: "1", Title: "第一首", Artist: "艺人"}}
	tt.Frame()
	title, ok := tt.Find("测试专辑")
	if !ok {
		t.Fatal("album title missing")
	}
	heart, ok := tt.Find("喜欢 测试专辑")
	if !ok || heart.Y <= title.Y || absInt(int(heart.X-title.X)) > 1 {
		t.Fatalf("like button is not under the title's start: title=%+v heart=%+v ok=%v", title, heart, ok)
	}
	if err := tt.Click("喜欢 测试专辑"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	if a.screen != screenAlbum || albumID.Load() != "al" || !a.likedAlbums["al"] {
		t.Fatalf("album star missed: screen=%s id=%v liked=%v", a.screen, albumID.Load(), a.likedAlbums)
	}
}

func TestHistoryButtonsReturnToThePreviousPage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getStarred2.view" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"subsonic-response":{"status":"ok","starred2":{}}}`)
	}))
	defer s.Close()
	client, err := subsonic.New(s.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	a, _, updates := testApp(t)
	a.client = client
	a.screen = screenPlaylists
	a.albumName = "歌单"
	a.nav.push(place{screen: screenStarred})
	a.nav.push(place{screen: screenPlaylists})
	tt := ui.NewTester(a.view, 1080, 720)
	if err := tt.Click("后退"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	if a.screen != screenStarred {
		t.Fatalf("back left screen=%s", a.screen)
	}
	if err := tt.Click("前进"); err != nil {
		t.Fatal(err)
	}
	if a.screen != screenPlaylists {
		t.Fatalf("forward left screen=%s", a.screen)
	}
	if err := tt.Click("喜爱"); err != nil {
		t.Fatal(err)
	}
	waitUpdate(t, updates)
	forward, ok := tt.Find("前进")
	if !ok {
		t.Fatal("forward button missing")
	}
	tt.ClickAt(forward.X+forward.W/2, forward.Y+forward.H/2)
	if a.nav.canForward() || a.screen != screenStarred {
		t.Fatalf("new visit kept forward: screen=%s forward=%v", a.screen, a.nav.canForward())
	}
}

func TestSidebarDividerKeepsTheMinimum(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.sidebarWidth = 220
	tt := ui.NewTester(a.view, 1080, 720)
	div, ok := tt.Find("Divider")
	if !ok {
		t.Fatal("sidebar divider missing")
	}
	x, y := div.X+div.W/2, div.Y+40
	tt.Press(x, y)
	tt.Move(20, y)
	tt.Release(20, y)
	if a.sidebarWidth != sidebarMinWidth {
		t.Fatalf("sidebar width = %v, want %v", a.sidebarWidth, sidebarMinWidth)
	}
	div, _ = tt.Find("Divider")
	x, y = div.X+div.W/2, div.Y+40
	tt.Press(x, y)
	tt.Move(900, y)
	tt.Release(900, y)
	if a.sidebarWidth != sidebarMaxWidth {
		t.Fatalf("sidebar width = %v, want %v", a.sidebarWidth, sidebarMaxWidth)
	}
}

func TestSidebarRailDropsLabelsWhenNarrow(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.sidebarWidth = 100
	tt := ui.NewTester(a.view, 1080, 720)
	snapshot(t, tt, "sidebar-rail.png")
	if tt.HasText("Godrome") {
		t.Fatalf("narrow sidebar still shows the title: %v", tt.Texts())
	}
	// The name stays for assistive technology, over the whole entry, and
	// no label text sits beside the icon.
	item, ok := tt.Find("曲库")
	if !ok || item.X > 20 || item.W < 60 {
		t.Fatalf("narrow sidebar has no icon-only entry: %+v ok=%v", item, ok)
	}
	settings, _ := tt.Find("设置")
	tt.ClickAt(settings.X+settings.W/2, settings.Y+settings.H/2)
	tt.Frame()
	if a.screen != screenSettings {
		t.Fatalf("icon-only entry did not choose its page: screen=%s", a.screen)
	}
	a.sidebarWidth = 220
	tt.Frame()
	snapshot(t, tt, "sidebar-wide.png")
	if !tt.HasText("Godrome") {
		t.Fatalf("wide sidebar lost the title: %v", tt.Texts())
	}
	label, ok := tt.Find("曲库")
	if !ok || label.X < 30 {
		t.Fatalf("wide sidebar did not show the label beside the icon: %+v ok=%v", label, ok)
	}
}

func TestQueueDividerKeepsTheMinimum(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.queueVisible = true
	a.queueWidth = 280
	tt := ui.NewTester(a.view, 1080, 720)
	// The queue line is one DIP. Split's handle covers a few DIPs to either
	// side, so the press is just to the right of that line.
	x := 1080 - a.queueWidth - 1 + 2
	y := float32(200)
	tt.Press(x, y)
	tt.Move(x+400, y)
	tt.Release(x+400, y)
	if a.queueWidth != queueMinWidth {
		t.Fatalf("queue width = %v, want %v", a.queueWidth, queueMinWidth)
	}
	x = 1080 - a.queueWidth - 1 + 2
	tt.Press(x, y)
	tt.Move(x-400, y)
	tt.Release(x-400, y)
	if a.queueWidth != queueMaxWidth {
		t.Fatalf("queue width = %v, want %v", a.queueWidth, queueMaxWidth)
	}
}

func TestQueueColumnFollowsVisibility(t *testing.T) {
	a, _, _ := testApp(t)
	a.screen = screenLibrary
	a.queueVisible = true
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("播放队列") {
		t.Fatalf("open queue missing: %v", tt.Texts())
	}
	if err := tt.Click("播放列表"); err != nil {
		t.Fatal(err)
	}
	if a.queueVisible {
		t.Fatal("queue toggle did not hide the queue")
	}
	a.queueVisible = false
	closed := ui.NewTester(a.view, 1080, 720)
	if closed.HasText("播放队列") {
		t.Fatal("closed queue was built")
	}
}

func TestQueueSelectionClearsTheScrollbar(t *testing.T) {
	a, _, _ := testApp(t)
	a.screen = screenLibrary
	a.queueVisible = true
	for i := range 40 {
		a.play.queue.Add(subsonic.Song{ID: fmt.Sprint(i), Title: fmt.Sprintf("队列曲目 %d", i)})
	}
	a.selectedQueue = 2
	tt := ui.NewTester(a.view, 1080, 720)
	row, ok := tt.Find("队列曲目 2")
	if !ok {
		t.Fatal("selected queue song not shown")
	}
	first, ok := tt.Find("队列曲目 0")
	if !ok {
		t.Fatal("first queue song not shown")
	}
	tt.Move(row.X+8, first.Y+4)
	img := tt.Image()
	y0, y1 := int(row.Y), int(row.Y+row.H)
	accent := -1
	for y := y0; y < y1 && y < img.Bounds().Dy(); y++ {
		if y < 0 {
			continue
		}
		for x := int(row.X); x < int(row.X+row.W)+2 && x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 > 200 && g>>8 < 90 && b>>8 < 110 && x > accent {
				accent = x
			}
		}
	}
	bar := -1
	x0, x1 := int(row.X+row.W), int(row.X+row.W)+28
	for y := int(first.Y); y < int(first.Y+first.H); y++ {
		if y < 0 {
			continue
		}
		for x := x0; x < x1 && x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			r, g, b = r>>8, g>>8, b>>8
			if r > 160 && r < 190 && absInt(int(r)-int(g)) < 6 && absInt(int(g)-int(b)) < 6 {
				if bar < 0 || x < bar {
					bar = x
				}
			}
		}
	}
	if accent < 0 {
		t.Fatal("queue selection highlight not painted")
	}
	if bar < 0 {
		t.Fatalf("queue scrollbar not painted; highlight ends at x=%d", accent)
	}
	if accent+6 >= bar {
		t.Fatalf("queue selection reaches the scrollbar: highlight ends at %d, thumb starts at %d, row %v", accent, bar, row)
	}
}

func TestProbeLibraryScroll(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	for i := range 200 {
		a.songs = append(a.songs, subsonic.Song{ID: fmt.Sprint(i), Title: fmt.Sprintf("曲目 %d", i), Artist: "艺人"})
	}
	a.more = true
	tt := ui.NewTester(a.view, 1080, 720)
	r, ok := tt.Find("曲目 0")
	t.Logf("first row %v ok=%v window texts=%d", r, ok, len(tt.Texts()))
	last, lok := tt.Find("曲目 199")
	t.Logf("last row %v ok=%v", last, lok)
	if ok {
		tt.Scroll(r.X+10, r.Y+4, 0, -400)
		tt.Frame()
		r2, _ := tt.Find("曲目 0")
		t.Logf("after wheel first row %v", r2)
		mid, mok := tt.Find("曲目 40")
		t.Logf("row 40 %v ok=%v", mid, mok)
	}
	tt2 := ui.NewTester(a.view, 700, 720)
	r3, ok3 := tt2.Find("曲目 0")
	last3, lok3 := tt2.Find("曲目 199")
	t.Logf("narrow first %v ok=%v last %v ok=%v texts=%d", r3, ok3, last3, lok3, len(tt2.Texts()))
	if ok3 {
		tt2.Scroll(r3.X+10, r3.Y+4, 0, -400)
		r4, _ := tt2.Find("曲目 0")
		t.Logf("narrow after wheel %v", r4)
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
	tt := ui.NewTester(a.view, 1080, 900)
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
			if tt.Focused("曲库") || tt.Focused("专辑") || tt.Focused("喜爱") || tt.Focused("歌单") || tt.Focused("设置") {
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
	focus("曲库")
	for range 8 {
		if a.screen == screenSettings {
			break
		}
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	if a.screen != screenSettings {
		t.Fatalf("keyboard did not open settings, screen=%s", a.screen)
	}
	focus("外观")
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if a.settingsTab != 2 {
		t.Fatalf("keyboard did not open 账号, tab=%d", a.settingsTab)
	}
	focus("退出登录")
	tt.Key(0, ui.KeyEnter)
	if a.client != nil || len(a.play.queue.Songs) != 0 {
		t.Fatal("keyboard logout failed")
	}
}

func TestSearchHeaderDoesNotBorrowTheFirstSong(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenSearch
	a.query = "夜"
	a.songs = []subsonic.Song{
		{ID: "1", Title: "第一首", Artist: "甲", Album: "专辑甲", CoverArt: "cover-1"},
		{ID: "2", Title: "第二首", Artist: "乙", Album: "专辑乙", CoverArt: "cover-2"},
	}
	tt := ui.NewTester(a.view, 1080, 720)
	title, ok := tt.Find("搜索结果")
	if !ok {
		t.Fatal("search title missing")
	}
	for _, name := range []string{"甲", "乙", "专辑甲"} {
		box, found := tt.Find(name)
		if found && box.Y < title.Y+70 {
			t.Fatalf("search header borrowed %s: title=%v box=%v", name, title, box)
		}
	}
	if !tt.HasText("搜索结果") || !tt.HasText("夜") || !tt.HasText("本次返回 2 首") {
		t.Fatalf("search header: %v", tt.Texts())
	}
}

func TestLibraryHeaderCountsThePage(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.songOffset = subsonic.PageSize
	a.more = true
	a.songs = []subsonic.Song{{ID: "1", Title: "第二页第一首", Artist: "不该出现在页眉"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("曲库") || !tt.HasText("本页 1 首") || !tt.HasText("第 2 页") {
		t.Fatalf("library header: %v", tt.Texts())
	}
	title, ok := tt.Find("曲库")
	if !ok {
		t.Fatal("library title missing")
	}
	if artist, found := tt.Find("不该出现在页眉"); found && artist.Y < title.Y+80 {
		t.Fatal("library header borrowed the first song's artist")
	}
}

func TestAlbumHeaderUsesTheAlbum(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenAlbum
	a.albumID = "al"
	a.albumName = "测试专辑"
	a.albumHead = albumHeaderData{ID: "al", Name: "测试专辑", Artist: "专辑艺人", CoverArt: "album-cover"}
	a.songs = []subsonic.Song{{ID: "1", Title: "第一首", Artist: "歌曲艺人", CoverArt: "song-cover"}}
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("专辑艺人") || !tt.HasText("播放") {
		t.Fatalf("album header: %v", tt.Texts())
	}
	if err := tt.Click("播放"); err != nil {
		t.Fatal(err)
	}
	if a.play.queue.Index != 0 || len(a.play.queue.Songs) != 1 {
		t.Fatalf("album play did not replace the queue: %+v", a.play.queue)
	}
}

func TestEmptySearchNamesTheQuery(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenSearch
	a.query = "不存在"
	a.busy = false
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("没有找到与「不存在」匹配的歌曲") {
		t.Fatalf("empty search: %v", tt.Texts())
	}
}

func TestSearchLimitSaysOneHundred(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenSearch
	a.query = "歌"
	for i := range subsonic.PageSize {
		a.songs = append(a.songs, subsonic.Song{ID: fmt.Sprint(i), Title: fmt.Sprintf("曲目 %d", i)})
	}
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("最多显示 100 首") {
		t.Fatalf("search cap: %v", tt.Texts())
	}
}

func TestNarrowWindowFoldsTheQueueWithoutForgettingIt(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.queueVisible = true
	a.queueWidth = 280
	a.sidebarWidth = 220
	wide := ui.NewTester(a.view, 1080, 720)
	if !wide.HasText("播放队列") {
		t.Fatal("wide window hid the queue")
	}
	// One tester keeps one frame count. A second tester would share the
	// app's list state and be taken for a second list in the same frame.
	narrow := wide
	narrow.SetSize(800, 560)
	narrow.Frame()
	if narrow.HasText("队列为空") {
		t.Fatalf("800-wide window still docked the queue: sidebar=%v queue=%v texts=%v", a.sidebarWidth, a.queueWidth, narrow.Texts())
	}
	if !narrow.HasText("窗口较窄，队列已折叠。加宽窗口后恢复。") {
		t.Fatalf("folded queue has no notice: %v", narrow.Texts())
	}
	if !a.queueVisible || a.queueWidth != 280 {
		t.Fatalf("fold overwrote the queue preference: visible=%v width=%v", a.queueVisible, a.queueWidth)
	}
	narrow.SetSize(1440, 900)
	narrow.Frame()
	if !narrow.HasText("播放队列") || a.queueWidth != 280 {
		t.Fatalf("wide window did not restore the queue: visible=%v width=%v texts=%v", a.queueVisible, a.queueWidth, narrow.Texts())
	}
}

func TestCompactVolumeKeepsMute(t *testing.T) {
	a, p, _ := testApp(t)
	a.screen = screenLibrary
	tt := ui.NewTester(a.view, 800, 560)
	if tt.HasText("音量") {
		t.Fatal("narrow bar still shows the volume slider")
	}
	if err := tt.Click("静音"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	slider, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("volume slider stayed hidden: %v", tt.Texts())
	}
	tt.ClickAt(slider.X+2, slider.Y+slider.H/2)
	if a.volume != 0 || p.volume != 0 {
		t.Fatalf("compact slider did not mute: volume=%v", a.volume)
	}
}

func TestEqualizerBandsStayReachableWhenNarrow(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenSettings
	a.settingsTab = 1
	tt := ui.NewTester(a.view, 800, 560)
	if err := tt.Click("16K Hz"); err != nil {
		t.Fatalf("last equalizer band: %v texts=%v", err, tt.Texts())
	}
}

func TestSearchTypingDoesNotGrowHistory(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query().Get("query")
		fmt.Fprintf(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[{"id":%q,"title":"结果"}]}}}`, q)
	}))
	defer s.Close()
	a, _, updates := testApp(t)
	a.client, _ = subsonic.New(s.URL, "u", "p")
	a.screen = screenAlbums
	a.visit()
	a.query = "夜"
	a.search()
	a.query = "夜色"
	a.search()
	waitUpdate(t, updates)
	if len(a.nav.entries) != 2 {
		t.Fatalf("typing grew history: %+v", a.nav.entries)
	}
	if len(a.songs) != 1 || a.songs[0].ID != "夜色" || calls.Load() != 1 {
		t.Fatalf("stale or extra search: songs=%v calls=%d", a.songs, calls.Load())
	}
	a.query = ""
	a.search()
	if a.screen != screenAlbums {
		t.Fatalf("clearing search left screen=%s", a.screen)
	}
}

func TestBackRestoresTheSong(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/search3.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","searchResult3":{"song":[{"id":"keep","title":"还在"},{"id":"other","title":"另一首"}]}}}`)
		case "/rest/getStarred2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","starred2":{}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	a, _, updates := testApp(t)
	a.client, _ = subsonic.New(s.URL, "u", "p")
	a.screen = screenLibrary
	a.songs = []subsonic.Song{{ID: "keep", Title: "还在"}, {ID: "other", Title: "另一首"}}
	a.selectedSong = 1
	a.visit()
	a.showStarred()
	waitUpdate(t, updates)
	a.goBack()
	waitUpdate(t, updates)
	if a.screen != screenLibrary || a.selectedSong != 1 {
		t.Fatalf("back did not restore the song: screen=%s selected=%d", a.screen, a.selectedSong)
	}
}

func TestStarredAlbumsBuildOnlyVisibleCards(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenStarred
	a.starredTab = 1
	for i := range 10000 {
		a.albums = append(a.albums, subsonic.Album{ID: fmt.Sprint(i), Name: fmt.Sprintf("专辑 %d", i), CoverArt: fmt.Sprint(i)})
	}
	var fetches atomic.Int32
	a.covers = coverCacheCounter(&fetches)
	tt := ui.NewTester(a.view, 1080, 720)
	if !tt.HasText("专辑 0") || tt.HasText("专辑 9999") {
		t.Fatalf("starred grid built offscreen cards, texts=%d", len(tt.Texts()))
	}
	if len(tt.Texts()) > 160 {
		t.Fatalf("starred albums not virtualized: %d texts", len(tt.Texts()))
	}
	before := int(fetches.Load())
	tt.Frame()
	if int(fetches.Load())-before > 8 {
		t.Fatalf("covers kept loading while the grid stood still: %d then %d", before, fetches.Load())
	}
	a.starredGrid.ScrollTo(9999, ui.End)
	tt.Frame()
	if !tt.HasText("专辑 9999") {
		t.Fatal("last starred album inaccessible")
	}
}

func coverCacheCounter(n *atomic.Int32) *cover.Cache {
	return cover.New(context.Background(), func(context.Context, string) ([]byte, error) {
		n.Add(1)
		return nil, fmt.Errorf("fixture")
	}, func() {})
}

func testCaches(t *testing.T) *appCaches {
	t.Helper()
	covers, err := cache.Open(filepath.Join(t.TempDir(), "covers"), 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := cache.Open(filepath.Join(t.TempDir(), "meta"), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	return &appCaches{covers: covers, meta: meta}
}

func TestStorageSettings(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.caches = testCaches(t)
	a.screen = screenSettings
	a.settingsTab = 3
	tt := ui.NewTester(a.view, 1080, 720)
	tt.Frame()
	if !tt.HasText("离线时显示缓存") || !tt.HasText("清除缓存") || !tt.HasText("已用") {
		t.Fatalf("storage settings missing: %v", tt.Texts())
	}
	if err := tt.Click("离线时显示缓存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.OfflineCache {
		t.Fatal("offline cache switch did not turn off")
	}
	if err := tt.Click("缓存上限设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("512 MiB"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.CacheLimitMB != 512 {
		t.Fatalf("cache limit %d", a.settings.CacheLimitMB)
	}
	if _, limit := a.caches.Stats(); limit != 512<<20 {
		t.Fatalf("store limit %d", limit)
	}
	if err := tt.Click("清除缓存"); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineBadgeOnlyOnContentPages(t *testing.T) {
	a, _, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.stale = true
	tt := ui.NewTester(a.view, 1080, 720)
	tt.Frame()
	if _, ok := tt.Find("离线缓存"); !ok {
		t.Fatalf("offline badge missing: %v", tt.Texts())
	}
	a.screen = screenSettings
	tt.Frame()
	if _, ok := tt.Find("离线缓存"); ok {
		t.Fatal("offline badge shown on settings")
	}
}

func TestOfflineStartupUsesCachedLibrary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ping.view"):
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		case strings.HasSuffix(r.URL.Path, "/getAlbumList2.view"):
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","albumList2":{"album":[{"id":"a","name":"缓存专辑"}]}}}`)
		default:
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok"}}`)
		}
	}))

	caches := testCaches(t)
	seed, err := subsonic.New(server.URL, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	seed.UseCache(caches, true)
	if _, err := seed.Albums(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	server.Close()

	p := &fakePlayer{}
	settings := config.Defaults()
	settings.Server = server.URL
	settings.Username = "user"
	a := newApp(settings, filepath.Join(t.TempDir(), "settings.json"), p)
	a.caches = caches
	updates := make(chan func(), 8)
	a.post = func(fn func()) { updates <- fn }
	t.Cleanup(a.close)
	a.server = server.URL
	a.username = "user"
	a.password = "secret"
	a.login()
	waitUpdate(t, updates) // login result, then browse starts
	waitUpdate(t, updates) // cached album page
	if a.screen != screenAlbums || len(a.albums) != 1 || a.albums[0].Name != "缓存专辑" {
		t.Fatalf("offline startup screen=%s albums=%v", a.screen, a.albums)
	}
	if !a.stale {
		t.Fatal("offline page is not marked stale")
	}
}
