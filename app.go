package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"godrome/internal/config"
	"godrome/internal/cover"
	"godrome/internal/player"
	"godrome/internal/subsonic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type playback interface {
	Load(string, uint64) error
	Pause(bool) error
	Seek(float64) error
	SetVolume(float64) error
	SetEqualizer(string) error
	Stop() error
	SetVisible(bool) error
	Events() <-chan player.Event
	Close()
}

// screen is the only navigation value. Sidebar item ids stay at the widget edge.
type screen int

const (
	screenLogin screen = iota
	screenLibrary
	screenAlbums
	screenAlbum
	screenSearch
	screenStarred
	screenPlaylists
	screenLocal
	screenSettings
)

func (s screen) String() string {
	switch s {
	case screenLogin:
		return "login"
	case screenLibrary:
		return "library"
	case screenAlbums:
		return "albums"
	case screenAlbum:
		return "album"
	case screenSearch:
		return "search"
	case screenStarred:
		return "starred"
	case screenPlaylists:
		return "playlists"
	case screenLocal:
		return "local"
	case screenSettings:
		return "settings"
	default:
		return "login"
	}
}

// nav is the Sidebar item id. Search and local files keep the library item selected.
func (s screen) nav() string {
	switch s {
	case screenAlbums, screenAlbum:
		return "albums"
	case screenStarred:
		return "starred"
	case screenPlaylists:
		return "playlists"
	case screenSettings:
		return "settings"
	default:
		return "library"
	}
}

// legacyPage preserves acceptance timeline labels from before screen existed.
func (s screen) legacyPage() string {
	switch s {
	case screenAlbums:
		return "albums"
	case screenSearch:
		return "search"
	case screenStarred:
		return "starred"
	case screenPlaylists:
		return "playlists"
	case screenLocal:
		return "local"
	case screenSettings:
		return "settings"
	case screenLibrary, screenAlbum:
		return "songs"
	default:
		return "login"
	}
}

type albumPage struct {
	offset int
	albums []subsonic.Album
}
type app struct {
	ctx    context.Context
	cancel context.CancelFunc
	// post always schedules on Window.Update in production; the view only mutates main-thread state.
	post                              func(func())
	engine                            playback
	client                            *subsonic.Client
	settings                          config.Settings
	configPath                        string
	server, username, password, query string
	status, errorText                 string
	busy                              bool
	screen                            screen
	pageOffset, songOffset            int
	pages                             []albumPage
	albums                            []subsonic.Album
	songs                             []subsonic.Song
	artists                           []subsonic.Artist
	albumName                         string
	albumHead                         albumHeaderData
	// likedSongs and likedAlbums are the session copy of server stars.
	// They are not written to settings.
	likedSongs                                  map[string]bool
	likedAlbums                                 map[string]bool
	more                                        bool
	pageCancel                                  context.CancelFunc
	pageCtx                                     context.Context
	requestID                                   uint64
	covers                                      *cover.Cache
	play                                        playbackState
	playback                                    player.Event
	generation                                  uint64
	position, volume                            float64
	volumeBefore                                float64
	albumsGrid                                  ui.GridState
	songsList, queueList                        ui.ListState
	starredSongs                                ui.ListState
	starredGrid                                 ui.GridState
	artistList                                  ui.ListState
	selectedAlbum, selectedSong, selectedQueue  int
	selectedStarredAlbum, selectedStarredArtist int
	starredTab                                  int
	queueVisible                                bool
	sidebarWidth                                float32
	queueWidth                                  float32
	albumID                                     string
	nav                                         history
	restoring                                   bool
	closeOnce                                   sync.Once
	remember                                    bool
	autoLogin                                   bool
	accent                                      ui.Color
	appearance                                  string
	eq                                          eqCurve
	eqPreset                                    eqPreset
	eqGain                                      [eqBands]float64
	settingsTab                                 int
	settingsScroll                              ui.ScrollState
	accentOpen                                  bool
	volumeShown                                 bool
	searchFrom                                  place
	searching                                   bool
	pendingRestore                              restore
	wantRestore                                 bool
	// setAppearance applies light, dark, or system. The window sets it.
	// The UI tester leaves it empty, because that call waits for the app loop.
	setAppearance func(mygo.ThemeSource)
}

func newApp(settings config.Settings, path string, engine playback) *app {
	ctx, cancel := context.WithCancel(context.Background())
	sidebar := settings.SidebarWidth
	if sidebar == 0 {
		sidebar = sidebarWidthDefault
	}
	queue := settings.QueueWidth
	if queue == 0 {
		queue = queueWidthDefault
	}
	a := &app{ctx: ctx, cancel: cancel, engine: engine, settings: settings, configPath: path, server: settings.Server, username: settings.Username, volume: settings.Volume, play: newPlayback(), playback: player.Event{State: player.Idle}, selectedAlbum: -1, selectedSong: -1, selectedQueue: -1, selectedStarredAlbum: -1, selectedStarredArtist: -1, queueVisible: true, sidebarWidth: sidebar, queueWidth: queue, screen: screenLogin, remember: true, likedSongs: map[string]bool{}, likedAlbums: map[string]bool{}, accent: accentFromSettings(settings.Accent), appearance: settings.Appearance, eqPreset: eqPreset(settings.EQPreset)}
	a.volumeBefore = settings.Volume
	if a.volumeBefore <= 0 {
		a.volumeBefore = 70
	}
	a.eq = gainsFromSettings(settings.EQGains)
	if a.eqPreset != eqCustom {
		a.eq = presetCurve(a.eqPreset, a.eq)
	}
	for i := range a.eqGain {
		a.eqGain[i] = a.eq[i]
	}
	if path != "" && settings.Server != "" && settings.Username != "" {
		if password, ok, err := config.LoadPassword(settings.Server, settings.Username); err != nil {
			a.errorText = err.Error()
		} else if ok {
			a.password = password
			a.autoLogin = true
		}
	}
	return a
}
func (a *app) dispatch(fn func()) {
	if a.ctx.Err() == nil && a.post != nil {
		a.post(func() {
			if a.ctx.Err() == nil {
				fn()
			}
		})
	}
}
func (a *app) listen() {
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case e, ok := <-a.engine.Events():
				if !ok {
					return
				}
				a.dispatch(func() { a.onPlayback(e) })
			}
		}
	}()
}
func (a *app) onPlayback(e player.Event) {
	if e.Generation != a.generation {
		return
	}
	a.playback = e
	a.position = e.Position
	if e.Err != nil {
		a.errorText = e.Err.Error()
	}
	if e.State == player.Ended && a.play.advance() {
		a.playCurrent()
	}
}

func (a *app) beginPage() (context.Context, uint64) {
	if a.pageCancel != nil {
		a.pageCancel()
	}
	a.pageCtx, a.pageCancel = context.WithCancel(a.ctx)
	a.requestID++
	a.busy = true
	a.errorText = ""
	a.status = "加载中…"
	a.covers = nil
	a.songs = nil
	a.albums = nil
	a.selectedSong = -1
	a.selectedAlbum = -1
	a.selectedStarredAlbum = -1
	a.selectedStarredArtist = -1
	a.songsList = ui.ListState{}
	a.albumsGrid = ui.GridState{}
	a.starredSongs = ui.ListState{}
	a.starredGrid = ui.GridState{}
	a.artistList = ui.ListState{}
	a.searching = false
	if !a.wantRestore {
		a.pendingRestore = restore{}
	}
	return a.pageCtx, a.requestID
}
func (a *app) finish(id uint64, err error) bool {
	if id != a.requestID {
		return false
	}
	a.busy = false
	a.status = ""
	a.searching = false
	if err != nil && !errors.Is(err, context.Canceled) {
		a.errorText = err.Error()
	}
	return err == nil
}

// keepRestore applies a bookmark once, after the page that owns it has
// its rows. A missing id falls back to the index, then to the first row.
func (a *app) keepRestore(ids []string, selected *int) {
	if !a.wantRestore {
		return
	}
	mark := a.pendingRestore
	a.wantRestore = false
	a.pendingRestore = restore{}
	if len(ids) == 0 {
		*selected = -1
		return
	}
	at := indexOf(ids, mark.selectedID)
	if at < 0 {
		at = mark.index
	}
	if at < 0 || at >= len(ids) {
		at = 0
	}
	*selected = at
	a.songsList.ScrollTo(at, ui.Center)
}

func indexOf(ids []string, id string) int {
	if id == "" {
		return -1
	}
	for i, item := range ids {
		if item == id {
			return i
		}
	}
	return -1
}

func songIDs(songs []subsonic.Song) []string {
	ids := make([]string, len(songs))
	for i, song := range songs {
		ids[i] = song.ID
	}
	return ids
}
func (a *app) setupCovers(ctx context.Context) {
	client := a.client
	a.covers = cover.New(ctx, client.Cover, func() { a.dispatch(func() {}) })
}
func (a *app) login() {
	dropSaved := a.autoLogin
	a.autoLogin = false
	client, err := subsonic.New(a.server, a.username, a.password)
	if err != nil {
		a.errorText = err.Error()
		return
	}
	ctx, id := a.beginPage()
	go func() {
		err := client.Ping(ctx)
		a.dispatch(func() {
			if id == a.requestID && dropSaved && err != nil && !errors.Is(err, context.Canceled) && strings.Contains(err.Error(), "认证失败") {
				_ = config.DeletePassword(a.server, a.username)
			}
			if !a.finish(id, err) {
				return
			}
			a.client = client
			a.pages = nil
			a.settings.Server = a.server
			a.settings.Username = a.username
			saveErr := a.persistPassword()
			a.password = ""
			a.browse(0)
			if saveErr != nil {
				a.errorText = saveErr.Error()
			}
		})
	}()
}
func (a *app) here() place {
	return place{
		screen:     a.screen,
		pageOffset: a.pageOffset,
		songOffset: a.songOffset,
		albumID:    a.albumID,
		albumName:  a.albumName,
		query:      a.query,
	}
}

// rememberPlace stores the selection of the page being left. History does
// not keep the songs themselves.
func (a *app) rememberPlace() {
	if a.restoring || a.screen == screenLogin {
		return
	}
	mark := restore{index: a.selectedSong}
	if a.screen == screenAlbums {
		mark.index = a.selectedAlbum
		if a.selectedAlbum >= 0 && a.selectedAlbum < len(a.albums) {
			mark.albumID = a.albums[a.selectedAlbum].ID
		}
	}
	if a.selectedSong >= 0 && a.selectedSong < len(a.songs) {
		mark.selectedID = a.songs[a.selectedSong].ID
	}
	first, _ := a.songsList.Visible()
	if first >= 0 && first < len(a.songs) {
		mark.visibleID = a.songs[first].ID
		mark.index = first
		if mark.selectedID == "" {
			mark.selectedID = a.songs[first].ID
		}
	}
	a.nav.mark(mark)
}

// visit records the screen the user just opened. Restoring a history
// entry must not record itself.
func (a *app) visit() {
	if a.restoring || a.screen == screenLogin {
		return
	}
	a.nav.push(a.here())
}

func (a *app) goTo(p place) {
	a.restoring = true
	a.pendingRestore = p.restore
	a.wantRestore = true
	defer func() { a.restoring = false }()
	switch p.screen {
	case screenLibrary:
		a.songOffset = p.songOffset
		a.showLibrary()
	case screenAlbums:
		a.browse(p.pageOffset)
	case screenAlbum:
		a.albumName = p.albumName
		a.openAlbum(p.albumID)
	case screenSearch:
		a.query = p.query
		a.searchNow(p.query)
	case screenStarred:
		a.showStarred()
	case screenPlaylists:
		a.showPlaceholder(screenPlaylists, "歌单")
	case screenLocal:
		a.screen = screenLocal
	case screenSettings:
		a.showSettings()
	}
}

func (a *app) goBack() {
	p, ok := a.nav.back()
	if ok {
		a.goTo(p)
	}
}

func (a *app) goForward() {
	p, ok := a.nav.forward()
	if ok {
		a.goTo(p)
	}
}

func (a *app) showLibrary() {
	if a.client == nil {
		a.screen = screenLocal
		a.visit()
		return
	}
	a.loadSongs(a.songOffset)
}

func (a *app) loadSongs(offset int) {
	a.rememberPlace()
	a.screen = screenLibrary
	ctx, id := a.beginPage()
	a.songOffset = max(0, offset)
	offset = a.songOffset
	a.albumName = ""
	a.albumID = ""
	a.albumHead = albumHeaderData{}
	a.query = ""
	a.visit()
	a.setupCovers(ctx)
	client := a.client
	go func() {
		songs, err := client.Songs(ctx, offset)
		a.dispatch(func() {
			if !a.finish(id, err) {
				return
			}
			a.songs = songs
			a.more = len(songs) == subsonic.PageSize
			a.keepRestore(songIDs(songs), &a.selectedSong)
		})
	}()
}

func (a *app) showStarred() {
	a.rememberPlace()
	if a.client == nil {
		a.showPlaceholder(screenStarred, "喜爱")
		return
	}
	a.screen = screenStarred
	a.albumName = "喜爱"
	a.albumID = ""
	a.albumHead = albumHeaderData{}
	a.query = ""
	a.artists = nil
	a.visit()
	ctx, id := a.beginPage()
	a.setupCovers(ctx)
	client := a.client
	go func() {
		starred, err := client.Starred(ctx)
		a.dispatch(func() {
			if !a.finish(id, err) {
				return
			}
			a.songs = starred.Songs
			a.albums = starred.Albums
			a.artists = starred.Artists
			a.noteStars(starred)
		})
	}()
}

// noteStars records the ids getStarred2 just returned. It does not drop
// stars learned from another page in this session.
func (a *app) noteStars(starred subsonic.Starred) {
	if a.likedSongs == nil {
		a.likedSongs = map[string]bool{}
	}
	if a.likedAlbums == nil {
		a.likedAlbums = map[string]bool{}
	}
	for _, song := range starred.Songs {
		if song.ID != "" {
			a.likedSongs[song.ID] = true
		}
	}
	for _, album := range starred.Albums {
		if album.ID != "" {
			a.likedAlbums[album.ID] = true
		}
	}
}

func (a *app) songLiked(song subsonic.Song) bool {
	if song.Path != "" || song.ID == "" {
		return false
	}
	if song.Liked() {
		return true
	}
	return a.likedSongs[song.ID]
}

func (a *app) albumLiked(album subsonic.Album) bool {
	if album.ID == "" {
		return false
	}
	if album.Liked() {
		return true
	}
	return a.likedAlbums[album.ID]
}

// setSongStar writes a song star and keeps the session copy in step.
// A failed write restores the previous mark.
func (a *app) setSongStar(song subsonic.Song, on bool) {
	if a.client == nil || song.Path != "" || song.ID == "" || a.songLiked(song) == on {
		return
	}
	if a.likedSongs == nil {
		a.likedSongs = map[string]bool{}
	}
	a.likedSongs[song.ID] = on
	a.applySongStar(song.ID, on)
	client := a.client
	id := song.ID
	go func() {
		var err error
		if on {
			err = client.Star(a.ctx, id, "", "")
		} else {
			err = client.Unstar(a.ctx, id, "", "")
		}
		a.dispatch(func() {
			if a.client != client {
				return
			}
			if err == nil || errors.Is(err, context.Canceled) {
				if err == nil && !on && a.screen == screenStarred {
					a.dropStarredSong(id)
				}
				return
			}
			a.likedSongs[id] = !on
			a.applySongStar(id, !on)
			a.errorText = err.Error()
		})
	}()
}

func (a *app) applySongStar(id string, on bool) {
	mark := ""
	if on {
		mark = "1"
	}
	for i := range a.songs {
		if a.songs[i].ID == id {
			a.songs[i].Starred = mark
		}
	}
}

func (a *app) dropStarredSong(id string) {
	kept := a.songs[:0]
	for _, song := range a.songs {
		if song.ID != id {
			kept = append(kept, song)
		}
	}
	a.songs = kept
}

func (a *app) setAlbumStar(album subsonic.Album, on bool) {
	if a.client == nil || album.ID == "" || a.albumLiked(album) == on {
		return
	}
	if a.likedAlbums == nil {
		a.likedAlbums = map[string]bool{}
	}
	a.likedAlbums[album.ID] = on
	for i := range a.albums {
		if a.albums[i].ID == album.ID {
			if on {
				a.albums[i].Starred = "1"
			} else {
				a.albums[i].Starred = ""
			}
		}
	}
	client := a.client
	id := album.ID
	go func() {
		var err error
		if on {
			err = client.Star(a.ctx, "", id, "")
		} else {
			err = client.Unstar(a.ctx, "", id, "")
		}
		a.dispatch(func() {
			if a.client != client {
				return
			}
			if err == nil || errors.Is(err, context.Canceled) {
				if err == nil && !on && a.screen == screenStarred {
					a.dropStarredAlbum(id)
				}
				return
			}
			a.likedAlbums[id] = !on
			for i := range a.albums {
				if a.albums[i].ID == id {
					if on {
						a.albums[i].Starred = ""
					} else {
						a.albums[i].Starred = "1"
					}
				}
			}
			a.errorText = err.Error()
		})
	}()
}

func (a *app) dropStarredAlbum(id string) {
	kept := a.albums[:0]
	for _, album := range a.albums {
		if album.ID != id {
			kept = append(kept, album)
		}
	}
	a.albums = kept
}

func (a *app) setArtistStar(artist subsonic.Artist, on bool) {
	if a.client == nil || artist.ID == "" || artist.Liked() == on {
		return
	}
	client := a.client
	id := artist.ID
	if !on {
		kept := a.artists[:0]
		for _, item := range a.artists {
			if item.ID != id {
				kept = append(kept, item)
			}
		}
		a.artists = kept
	}
	go func() {
		var err error
		if on {
			err = client.Star(a.ctx, "", "", id)
		} else {
			err = client.Unstar(a.ctx, "", "", id)
		}
		a.dispatch(func() {
			if a.client != client {
				return
			}
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			a.errorText = err.Error()
			if a.screen == screenStarred {
				a.showStarred()
			}
		})
	}()
}

func (a *app) showPlaceholder(s screen, title string) {
	a.rememberPlace()
	if a.pageCancel != nil {
		a.pageCancel()
	}
	a.screen = s
	a.albumName = title
	a.albumID = ""
	a.albumHead = albumHeaderData{}
	a.visit()
	a.busy = false
	a.status = ""
	a.errorText = ""
	a.songs = nil
	a.albums = nil
	a.selectedSong = -1
	a.selectedAlbum = -1
}

func (a *app) browse(offset int) {
	a.rememberPlace()
	a.screen = screenAlbums
	ctx, id := a.beginPage()
	a.pageOffset = max(0, offset)
	offset = a.pageOffset
	a.albumName = ""
	a.albumID = ""
	a.albumHead = albumHeaderData{}
	a.query = ""
	a.visit()
	a.setupCovers(ctx)
	for _, p := range a.pages {
		if p.offset == offset {
			a.albums = p.albums
			a.more = len(a.albums) == subsonic.PageSize
			a.finish(id, nil)
			return
		}
	}
	client := a.client
	go func() {
		albums, err := client.Albums(ctx, offset)
		a.dispatch(func() {
			if !a.finish(id, err) {
				return
			}
			a.albums = albums
			a.more = len(albums) == subsonic.PageSize
			a.pages = append(a.pages, albumPage{offset: offset, albums: albums})
			if len(a.pages) > 5 {
				a.pages = append([]albumPage(nil), a.pages[len(a.pages)-5:]...)
			}
		})
	}()
}
func (a *app) openAlbum(id string) {
	a.rememberPlace()
	a.screen = screenAlbum
	a.albumID = id
	a.albumHead = albumHeaderData{}
	a.visit()
	ctx, seq := a.beginPage()
	a.setupCovers(ctx)
	client := a.client
	go func() {
		album, err := client.Album(ctx, id)
		a.dispatch(func() {
			if !a.finish(seq, err) {
				return
			}
			a.songs = album.Songs
			a.albumName = album.Name
			a.albumHead = albumHeaderData{ID: album.ID, Name: album.Name, Artist: album.Artist, CoverArt: album.CoverArt}
			if a.likedAlbums == nil {
				a.likedAlbums = map[string]bool{}
			}
			if album.ID != "" {
				a.likedAlbums[album.ID] = album.Liked()
			}
			a.nav.note(a.here())
			a.keepRestore(songIDs(album.Songs), &a.selectedSong)
		})
	}()
}
func (a *app) search() {
	query := a.query
	if query == "" {
		a.clearSearch()
		return
	}
	if a.screen != screenSearch {
		a.rememberPlace()
		from := a.here()
		from.opened = true
		a.searchFrom = from
	}
	a.searchNow(query)
}

// clearSearch returns to the page search was opened from. It does not
// record another search visit.
func (a *app) clearSearch() {
	back := a.searchFrom
	a.searchFrom = place{}
	if !back.opened {
		a.browse(a.pageOffset)
		return
	}
	a.goTo(back)
}

// searchNow runs a search whose query is already chosen. The debounce
// still applies, so a history return does not hit the server at once.
func (a *app) searchNow(query string) {
	if query == "" {
		a.clearSearch()
		return
	}
	a.query = query
	fresh := a.screen != screenSearch
	a.screen = screenSearch
	a.albumID = ""
	a.albumName = ""
	a.albumHead = albumHeaderData{}
	if a.restoring || fresh {
		a.visit()
	} else {
		a.nav.replaceSearch(query)
	}
	if a.pageCancel != nil {
		a.pageCancel()
	}
	a.pageCtx, a.pageCancel = context.WithCancel(a.ctx)
	a.requestID++
	id := a.requestID
	ctx := a.pageCtx
	a.searching = true
	a.status = "正在搜索「" + query + "」"
	client := a.client
	if a.covers == nil {
		a.setupCovers(ctx)
	}
	go func() {
		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		songs, err := client.Search(ctx, query)
		a.dispatch(func() {
			if !a.finish(id, err) {
				return
			}
			if a.query != query {
				return
			}
			a.songs = songs
			a.keepRestore(songIDs(songs), &a.selectedSong)
		})
	}()
}
func (a *app) playCurrent() {
	song, ok := a.play.queue.Current()
	if !ok {
		return
	}
	path := song.Path
	if path == "" {
		if a.client == nil {
			a.errorText = "请先登录"
			return
		}
		var err error
		path, err = a.client.StreamURL(song.ID)
		if err != nil {
			a.errorText = err.Error()
			return
		}
	}
	a.generation++
	a.errorText = ""
	a.position = 0
	a.selectedQueue = a.play.queue.Index
	a.playback = player.Event{Generation: a.generation, State: player.Loading, Duration: song.Duration}
	a.control(a.engine.Load(path, a.generation))
}
func (a *app) playSongs(index int) {
	if a.play.queue.Replace(a.songs, index) {
		a.playCurrent()
	}
}
func (a *app) control(err error) {
	if err != nil {
		a.errorText = err.Error()
		a.playback.State = player.Failed
	}
}
func (a *app) persistPassword() error {
	if a.configPath == "" {
		return nil
	}
	if !a.remember {
		return config.DeletePassword(a.server, a.username)
	}
	return config.SavePassword(a.server, a.username, a.password)
}

func (a *app) logout() {
	if err := config.DeletePassword(a.server, a.username); err != nil {
		a.errorText = err.Error()
		return
	}
	a.generation++
	a.control(a.engine.Stop())
	a.screen = screenLogin
	a.nav = history{}
	a.searchFrom = place{}
	a.pendingRestore = restore{}
	a.wantRestore = false
	a.albumHead = albumHeaderData{}
	a.albumID = ""
	a.beginPage()
	a.busy = false
	a.status = ""
	a.client = nil
	a.pages = nil
	a.likedSongs = map[string]bool{}
	a.likedAlbums = map[string]bool{}
	a.artists = nil
	a.play = newPlayback()
	a.query = ""
	a.password = ""
	a.remember = true
	a.autoLogin = false
	a.position = 0
	a.playback = player.Event{Generation: a.generation, State: player.Idle}
}
func (a *app) local(paths []string) {
	a.rememberPlace()
	a.screen = screenLocal
	a.visit()
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		a.songs = append(a.songs, subsonic.Song{ID: abs, Path: abs, Title: filepath.Base(abs), Artist: "本地音频"})
	}
}
func (a *app) close() {
	a.closeOnce.Do(func() {
		a.cancel()
		if a.pageCancel != nil {
			a.pageCancel()
		}
		a.engine.Close()
	})
}
func playbackLabel(state player.State) string {
	switch state {
	case player.Idle:
		return "待播放"
	case player.Loading:
		return "加载中"
	case player.Playing:
		return "播放中"
	case player.Paused:
		return "已暂停"
	case player.Buffering:
		return "缓冲中"
	case player.Ended:
		return "播放结束"
	case player.Failed:
		return "播放失败"
	default:
		return "待播放"
	}
}

func clockText(seconds float64) string {
	s := max(0, int(seconds))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// appleMusicPink is the default accent. The system theme still supplies
// surfaces, text, fonts and scaling.
func appleMusicPink(dark bool) ui.Color {
	if dark {
		return ui.Hex("#fc3c44")
	}
	return ui.Hex("#fa233b")
}

func accentFromSettings(hex string) ui.Color {
	if hex == "" {
		return appleMusicPink(false)
	}
	return ui.Hex(hex)
}

func gainsFromSettings(gains []float64) eqCurve {
	var curve eqCurve
	if len(gains) != eqBands {
		return curve
	}
	for i := range curve {
		curve[i] = clampGain(gains[i])
	}
	return curve
}

func accentHex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

type accentSwatch struct {
	Name  string
	Color ui.Color
}

func accentSwatches() []accentSwatch {
	return []accentSwatch{
		{"粉", ui.Hex("#fa233b")},
		{"蓝", ui.Hex("#0a84ff")},
		{"紫", ui.Hex("#bf5af2")},
		{"绿", ui.Hex("#30d158")},
		{"橙", ui.Hex("#ff9f0a")},
	}
}

func appearanceIndex(v string) int {
	switch v {
	case "light":
		return 0
	case "dark":
		return 1
	default:
		return 2
	}
}

func appearanceValue(i int) string {
	switch i {
	case 0:
		return "light"
	case 1:
		return "dark"
	default:
		return "system"
	}
}

func appearanceHint(v string) string {
	switch v {
	case "light":
		return "始终使用浅色。"
	case "dark":
		return "始终使用深色。"
	default:
		return "跟随系统的浅色或深色。"
	}
}

// appearanceSource maps the saved choice onto MyGO. A headless test has no
// system appearance, so 跟随系统 keeps the appearance the test already set.
func appearanceSource(v string, systemDark bool) mygo.ThemeSource {
	switch v {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	default:
		if systemDark {
			return mygo.ThemeDark
		}
		return mygo.ThemeLight
	}
}

func eqPresetFromLabel(label string) eqPreset {
	if label == "自定义" || label == "" {
		return eqCustom
	}
	return eqPreset(label)
}

func eqHzLabel(hz int) string {
	if hz >= 1000 {
		return fmt.Sprintf("%dK", hz/1000)
	}
	return fmt.Sprintf("%d", hz)
}

func eqBandLabel(hz int) string {
	return eqHzLabel(hz) + " Hz"
}

// applyTheme paints the chosen accent onto the system theme. Surfaces, text,
// fonts and scaling still come from that theme.
func (a *app) applyTheme(c *ui.Context) *ui.Theme {
	t := *c.Theme()
	base := a.accent
	if base.A == 0 {
		base = appleMusicPink(t.Dark)
		a.accent = base
	}
	t.Accent = base
	t.AccentHover = base.Mix(ui.Hex("#ffffff"), 0.16)
	t.AccentPressed = base.Mix(ui.Hex("#000000"), 0.18)
	t.Focus = ui.RGBA(base.R, base.G, base.B, 0.55)
	t.Selection = ui.RGBA(base.R, base.G, base.B, 0.28)
	t.AccentText = ui.Hex("#ffffff")
	t.Radius = 10
	c.SetTheme(&t)
	return c.Theme()
}

const (
	sidebarWidthDefault = 220
	queueWidthDefault   = 280
	sidebarMinWidth     = 160
	queueMinWidth       = 200
	sidebarMaxWidth     = 360
	queueMaxWidth       = 480
	// contentMinWidth is the main page before its own padding. A docked
	// queue yields to this before it yields to the saved queue width.
	contentMinWidth = 320
	// foldWindow is the window width that cannot keep a docked queue. Wider
	// windows keep the queue the user asked for, and the divider limits apply.
	foldWindow = 860
	contentPad = 44
	// compactControls is the window width where the volume slider leaves the
	// bottom bar. The speaker button stays.
	compactControls = 900
	// motionDuration is one layout change. It is not a loop.
	motionDuration = 180 * time.Millisecond
)

func clampWidth(v, lo, hi float32) float32 {
	return max(lo, min(hi, v))
}

// coverEnter fades a cover in once, when its image first appears in a box
// that was already there. MyGO skips this when the desktop asks for less motion.
var coverEnter = ui.ElementTransition{
	Duration: motionDuration,
	Enter:    &ui.Motion{Opacity: 0},
}

// noticeMotion shows an error or status as it arrives, and lets it leave.
var noticeMotion = ui.ElementTransition{
	Duration: motionDuration,
	Enter:    &ui.Motion{Y: -8},
	Exit:     &ui.Motion{Opacity: 0},
}

// listScrollGutter is the room a vertical MyGO scroll thumb needs on the
// right of a list. The thumb is painted over the content: it sits 3 DIPs
// in from a track of width+6, and grows 2 DIPs to the left while dragged.
// Row padding stops the highlight short of that track.
func listScrollGutter(t *ui.Theme) float32 {
	w := t.ScrollbarWidth
	if w <= 0 {
		w = 6
	}
	return w + 16
}

func (a *app) view(c *ui.Context) {
	t := a.applyTheme(c)
	mode := a.layoutMode(c)
	// Copy the decision out of the struct. The view closure runs again as
	// the frame is built, and a later write to the struct must not change
	// the branch this frame already chose.
	sidebar := mode.sidebar
	docked := mode.queueDocked
	content := mode.content
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		if a.screen == screenLogin {
			a.loginView(c)
			return
		}
		ui.Split(c, &sidebar, func() { a.sidebar(c) }, func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.topBar(c)
				a.message(c)
				if docked {
					// The value Split stores is the content width. The queue
					// width is what is left, so the drag matches the pane on
					// the right.
					ui.Split(c, &content, func() { a.mainColumn(c) }, func() { a.queuePane(c) }).Grow(1).MinHeight(0)
					a.queueWidth = a.widthFromContent(c, content)
				} else {
					a.mainColumn(c)
				}
			})
		}).Grow(1).MinHeight(0)
		a.sidebarWidth = sidebar
		a.clampColumns(c)
		a.controlsView(c)
		if !docked && a.queueVisible {
			a.queueOverlay(c)
		}
	})
}

// layout is one decision about the three columns. Views read this instead
// of guessing from the window a second time.
type layout struct {
	sidebar     float32
	content     float32
	queueDocked bool
	compact     bool
}

// layoutMode keeps the saved sidebar and queue widths, and only changes
// the width this frame uses. A window that is too narrow folds the queue
// without clearing the user's request to show it.
func (a *app) layoutMode(c *ui.Context) layout {
	window, _ := c.Size()
	if window <= 1 {
		window = 1080
	}
	side := clampWidth(a.sidebarWidth, sidebarMinWidth, sidebarMaxWidth)
	room := window - side - 1
	need := float32(contentMinWidth + contentPad)
	docked := a.queueVisible && window >= foldWindow && room >= need+1+queueMinWidth
	content := room
	if docked {
		content = room - a.queueWidth - 1
		if content < need {
			content = need
		}
	}
	return layout{sidebar: side, content: content, queueDocked: docked, compact: window < compactControls}
}

// queueOpen reports whether the queue pane is taking part in the row.
func (a *app) queueOpen() bool { return a.queueVisible }

// contentWidth is the width Split should give the page. The inner split
// fills the column beside the sidebar, and its divider sits inside that
// width, so the queue is what remains.
func (a *app) contentWidth(c *ui.Context) float32 {
	_, after := a.columnRoom(c)
	if after <= 1 {
		return 400
	}
	return after - a.queueWidth - 1
}

// widthFromContent turns the content width Split reports back into a queue
// width. Before the row has a size, the stored width stays.
func (a *app) widthFromContent(c *ui.Context, content float32) float32 {
	_, after := a.columnRoom(c)
	if after <= 1 {
		return a.queueWidth
	}
	return after - content - 1
}

// pageWidth is the album or song page inside the main column padding.
// The view context reports the window, so this uses the same split widths
// the layout stores.
func (a *app) pageWidth(c *ui.Context) float32 {
	mode := a.layoutMode(c)
	page := mode.content - contentPad
	if page < 48 {
		return 48
	}
	return page
}

// columnRoom is the window width and the width beside the sidebar divider.
func (a *app) columnRoom(c *ui.Context) (window, afterSidebar float32) {
	window, _ = c.Size()
	return window, window - a.sidebarWidth - 1
}

// clampColumns keeps both panes inside their limits after a drag or a
// resize. Split itself only keeps 40 DIPs from either edge.
func (a *app) clampColumns(c *ui.Context) {
	a.sidebarWidth = clampWidth(a.sidebarWidth, sidebarMinWidth, sidebarMaxWidth)
	window, _ := c.Size()
	if window > 1 && window < foldWindow {
		a.sidebarWidth = min(a.sidebarWidth, max(sidebarMinWidth, window-contentMinWidth))
	}
	// A folded queue keeps the width the user saved. The next wide window
	// uses it again.
	if !a.layoutMode(c).queueDocked {
		a.queueWidth = clampWidth(a.queueWidth, queueMinWidth, queueMaxWidth)
		return
	}
	_, after := a.columnRoom(c)
	hi := min(queueMaxWidth, after-contentMinWidth-contentPad-1)
	if hi < queueMinWidth {
		hi = queueMinWidth
	}
	a.queueWidth = clampWidth(a.queueWidth, queueMinWidth, hi)
}

func (a *app) mainColumn(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Padding(4, 22, 8, 22).Children(func() {
		switch a.screen {
		case screenAlbums:
			a.albumView(c)
		case screenStarred:
			a.starredView(c)
		case screenPlaylists:
			a.placeholderView(c)
		case screenSettings:
			a.settingsView(c)
		default:
			a.songView(c)
		}
	})
}

func (a *app) queuePane(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Padding(12, 12, 8, 4).Children(func() { a.queueView(c) })
}

func (a *app) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Background(t.Surface).Children(func() {
		ui.Row(c).Padding(14, 10, 8, 16).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Godrome").FontSize(20).FontWeight(700).Grow(1).SingleLine()
			a.historyButtons(c)
		})
		section := a.screen.nav()
		if ui.Sidebar(c, &section, func() {
			ui.SidebarItem(c, "library", nil, "曲库")
			ui.SidebarItem(c, "albums", nil, "专辑")
			ui.SidebarItem(c, "starred", nil, "喜爱")
			ui.SidebarItem(c, "playlists", nil, "歌单")
			ui.SidebarItem(c, "settings", nil, "设置")
		}).Grow(1).Changed() {
			switch section {
			case "library":
				a.showLibrary()
			case "albums":
				if a.client != nil {
					a.browse(a.pageOffset)
				}
			case "starred":
				a.showStarred()
			case "playlists":
				a.showPlaceholder(screenPlaylists, "歌单")
			case "settings":
				a.showSettings()
			}
		}
	})
}

func (a *app) showSettings() {
	a.rememberPlace()
	if a.pageCancel != nil {
		a.pageCancel()
	}
	a.screen = screenSettings
	a.albumName = "设置"
	a.albumID = ""
	a.visit()
	a.busy = false
	a.status = ""
}

func (a *app) settingsView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
		ui.Text(c, "设置").FontSize(30).FontWeight(700).Padding(6, 2, 0, 2)
		ui.Tabs(c, &a.settingsTab, "外观", "声音", "账号").Padding(0, 2, 0, 2)
		ui.Scroll(c).Grow(1).MinHeight(120).TrackScroll(&a.settingsScroll).Children(func() {
			ui.Column(c).Padding(16, 18, 20, 2).MaxWidth(520).Children(func() {
				switch a.settingsTab {
				case 1:
					a.equalizerSection(c, t)
				case 2:
					a.accountSection(c, t)
				default:
					a.lookSection(c, t)
				}
			})
		})
	})
}

func (a *app) lookSection(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(22).Children(func() {
		a.appearanceSection(c, t)
		a.accentSection(c, t)
	})
}

func (a *app) appearanceSection(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(10).Children(func() {
		if next, ok := a.appearanceChoice(c); ok {
			a.appearance = appearanceValue(next)
			a.applyAppearance()
			a.persistLook()
		}
		ui.Text(c, appearanceHint(a.appearance)).FontSize(12).TextColor(t.TextMuted)
	})
}

// appearanceChoice is sun, moon, and a monitor. The labels stay so tests
// and the keyboard can find 浅色, 深色, and 跟随系统.
func (a *app) appearanceChoice(c *ui.Context) (int, bool) {
	t := c.Theme()
	glyphs := []*ui.SVG{iconSun, iconMoon, iconMonitor}
	labels := []string{"浅色", "深色", "跟随系统"}
	current := appearanceIndex(a.appearance)
	picked, ok := current, false
	ui.Row(c).Gap(8).Children(func() {
		for i, label := range labels {
			on := i == current
			btn := ui.Button(c, "").Label(label).Tooltip(label)
			if on {
				btn.TextColor(t.Accent)
			}
			btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
				if on {
					p.Fill(ui.Rect{X: r.X + 12, Y: r.Y + r.H - 3, W: r.W - 24, H: 2}, t.Accent, 1)
				}
			})
			btn.Children(func() {
				ui.Column(c).Gap(4).AlignItems(ui.Center).Padding(4, 8, 8, 8).Children(func() {
					ui.Icon(c, glyphs[i]).Size(18, 18)
					ui.Text(c, label).FontSize(12)
				})
			})
			if btn.Clicked() {
				picked, ok = i, true
			}
		}
	})
	return picked, ok
}

func (a *app) accentSection(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(10).Children(func() {
		ui.Text(c, "主题色").FontWeight(600)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.accentWell(c).Changed() {
				a.persistLook()
			}
			for _, swatch := range accentSwatches() {
				color := swatch
				on := accentHex(a.accent) == accentHex(color.Color)
				btn := ui.Button(c, "").Label(color.Name).Tooltip(color.Name).Padding(2).Background(ui.Color{}).BorderWidth(0)
				if on {
					btn.Border(2, t.Text)
				}
				btn.Children(func() {
					ui.Box(c).Size(18, 18).Radius(9).Background(color.Color)
				})
				if btn.Clicked() {
					a.accent = color.Color
					a.persistLook()
				}
			}
			if ui.Button(c, "恢复默认").Clicked() {
				a.accent = appleMusicPink(t.Dark)
				a.persistLook()
			}
		})
	})
}

// accentWell opens the color picker from a circle the same size as the
// preset dots.
func (a *app) accentWell(c *ui.Context) *ui.Element {
	t := c.Theme()
	well := ui.Button(c, "").Label("自定义颜色").Tooltip("自定义颜色")
	well.Padding(0).Size(22, 22).Radius(11).Background(ui.Color{}).BorderWidth(0)
	now := a.accent
	well.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, now, r.W/2)
		p.Stroke(r, t.Border, r.W/2, 1)
	})
	if well.Clicked() {
		a.accentOpen = !a.accentOpen
	}
	ui.Popover(c, well, &a.accentOpen, func() {
		if ui.ColorPicker(c, &a.accent).Changed() {
			a.persistLook()
		}
	})
	return well
}

func (a *app) equalizerSection(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "均衡器").FontWeight(600).Grow(1).Shrink(1)
			names := make([]string, 0, len(eqPresetList()))
			for _, p := range eqPresetList() {
				names = append(names, eqPresetLabel(p))
			}
			selected := eqPresetLabel(a.eqPreset)
			if ui.Select(c, &selected, names).Label("均衡器预设").Changed() {
				a.eqPreset = eqPresetFromLabel(selected)
				a.eq = presetCurve(a.eqPreset, a.eq)
				a.syncEQGains()
				a.applyEqualizer()
				a.persistLook()
			}
		})
		ui.ScrollHorizontal(c).Gap(4).AlignItems(ui.End).Shrink(0).Children(func() {
			for i, hz := range eqFreqs {
				band := i
				ui.Column(c).Gap(6).AlignItems(ui.Center).Width(46).Children(func() {
					ui.Textf(c, "%+.0f", a.eqGain[band]).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum")
					if eqSlider(c, &a.eqGain[band]).Label(eqBandLabel(hz)).Changed() {
						a.eqGain[band] = clampGain(a.eqGain[band])
						a.eq[band] = a.eqGain[band]
						a.eqPreset = eqCustom
						a.applyEqualizer()
						a.persistLook()
					}
					ui.Text(c, eqHzLabel(hz)).FontSize(11).TextColor(t.TextMuted)
				})
			}
		})
	})
}

// eqSlider is one vertical equalizer band. The low gain sits at the bottom.
func eqSlider(c *ui.Context, value *float64) *ui.Element {
	t := c.Theme()
	s := ui.SliderBase(c, value, eqGainMin, eqGainMax).Vertical().Size(28, 140).PaddingY(8)
	frac := float32((*value - eqGainMin) / (eqGainMax - eqGainMin))
	s.Draw(func(p *ui.Painter, r ui.Rect) {
		track := ui.Rect{X: r.X + r.W/2 - 2, Y: r.Y + 8, W: 4, H: r.H - 16}
		p.Fill(track, t.Border, 2)
		knob := float32(8)
		y := track.Y + track.H - knob/2 - frac*(track.H-knob)
		p.Fill(ui.Rect{X: r.X + (r.W-knob)/2, Y: y - knob/2, W: knob, H: knob}, t.Accent, knob/2)
	})
	return s
}

func (a *app) accountSection(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(12).MaxWidth(280).Children(func() {
		if a.client == nil {
			ui.Text(c, "当前未登录。").FontSize(13).TextColor(t.TextMuted)
			return
		}
		if a.username != "" {
			ui.Textf(c, "已登录 %s", a.username).TextColor(t.TextMuted)
		}
		if ui.Button(c, "退出登录").Clicked() {
			a.logout()
		}
	})
}

func (a *app) syncEQGains() {
	for i := range a.eqGain {
		a.eqGain[i] = a.eq[i]
	}
}

func (a *app) applyEqualizer() {
	if err := a.engine.SetEqualizer(a.eq.filter()); err != nil {
		a.errorText = err.Error()
	}
}

func (a *app) applyAppearance() {
	if a.setAppearance == nil {
		return
	}
	switch a.appearance {
	case "light":
		a.setAppearance(mygo.ThemeLight)
	case "dark":
		a.setAppearance(mygo.ThemeDark)
	default:
		a.setAppearance(mygo.ThemeSystem)
	}
}

func (a *app) persistLook() {
	a.settings.Accent = accentHex(a.accent)
	a.settings.Appearance = a.appearance
	a.settings.EQPreset = string(a.eqPreset)
	a.settings.EQGains = a.eq[:]
	if a.configPath == "" {
		return
	}
	if err := config.Save(a.configPath, a.settings); err != nil {
		a.errorText = "保存设置失败"
	}
}

func (a *app) historyButtons(c *ui.Context) {
	back := ui.Button(c, "").Label("后退").Tooltip("后退").Disabled(!a.nav.canBack())
	back.Children(func() { ui.Icon(c, iconBack).Size(14, 14) })
	if back.Clicked() {
		a.goBack()
	}
	forward := ui.Button(c, "").Label("前进").Tooltip("前进").Disabled(!a.nav.canForward())
	forward.Children(func() { ui.Icon(c, iconForward).Size(14, 14) })
	if forward.Clicked() {
		a.goForward()
	}
}

func (a *app) starredView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Text(c, "喜爱").FontSize(30).FontWeight(700).Padding(6, 2, 8, 2)
		ui.Tabs(c, &a.starredTab, "歌曲", "喜爱专辑", "喜爱艺人").Padding(0, 2, 8, 2)
		if !a.busy && len(a.artists) == 0 && len(a.albums) == 0 && len(a.songs) == 0 {
			ui.Text(c, "还没有喜爱。在歌曲或专辑上标记喜欢。").TextColor(t.TextMuted).Padding(8, 2, 12, 2)
			return
		}
		switch a.starredTab {
		case 1:
			a.starredAlbums(c, t)
		case 2:
			a.starredArtists(c, t)
		default:
			ui.Textf(c, "%d 首", len(a.songs)).TextColor(t.TextMuted).Padding(0, 2, 8, 2)
			a.songList(c, "还没有喜爱的歌曲。")
		}
	})
}

func (a *app) starredAlbums(c *ui.Context, t *ui.Theme) {
	ui.Textf(c, "%d 张", len(a.albums)).TextColor(t.TextMuted).Padding(0, 2, 8, 2)
	if !a.busy && len(a.albums) == 0 {
		ui.Text(c, "还没有喜欢的专辑。").TextColor(t.TextMuted).Padding(8, 2)
		return
	}
	a.starredGrid.Selected = &a.selectedStarredAlbum
	a.starredGrid.Key = func(i int) any { return a.albums[i].ID }
	a.starredGrid.Label = func(i int) string { return a.albums[i].Name }
	openID := ""
	grid := ui.GridView(c, &a.starredGrid, len(a.albums), 148, 236, func(i int) {
		album := a.albums[i]
		card := ui.Column(c).Width(132).AlignSelf(ui.Center).Padding(8, 0, 6, 0).Gap(6).Cursor(ui.CursorPointer)
		card.Children(func() {
			a.coverArt(c, album.CoverArt, 132)
			ui.Text(c, album.Name).FontWeight(600).SingleLine()
			artist := album.Artist
			if artist == "" {
				artist = "未知艺人"
			}
			ui.Text(c, artist).FontSize(12).TextColor(t.TextMuted).SingleLine()
			a.albumStarButton(c, album)
		})
		if card.Clicked() {
			openID = album.ID
		}
	}).Grow(1)
	if grid.Submitted() && a.selectedStarredAlbum >= 0 && a.selectedStarredAlbum < len(a.albums) {
		openID = a.albums[a.selectedStarredAlbum].ID
	}
	if openID != "" {
		a.openAlbum(openID)
	}
}

func (a *app) starredArtists(c *ui.Context, t *ui.Theme) {
	ui.Textf(c, "%d 位", len(a.artists)).TextColor(t.TextMuted).Padding(0, 2, 8, 2)
	if !a.busy && len(a.artists) == 0 {
		ui.Text(c, "还没有喜欢的艺人。").TextColor(t.TextMuted).Padding(8, 2)
		return
	}
	a.artistList.Selected = &a.selectedStarredArtist
	a.artistList.Key = func(i int) any { return a.artists[i].ID }
	a.artistList.Label = func(i int) string {
		if a.artists[i].Name == "" {
			return "未知艺人"
		}
		return a.artists[i].Name
	}
	drop := -1
	ui.List(c, &a.artistList, len(a.artists), func(i int) {
		artist := a.artists[i]
		name := artist.Name
		if name == "" {
			name = "未知艺人"
		}
		ui.Row(c).Padding(7, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).Grow(1).SingleLine()
			if ui.Button(c, "").Label("取消喜欢 " + name).Tooltip("取消喜欢").Children(func() {
				ui.Icon(c, iconHeartOn).Size(14, 14).TextColor(t.Accent)
			}).Clicked() {
				drop = i
			}
		})
	}).Grow(1)
	if drop >= 0 && drop < len(a.artists) {
		a.setArtistStar(a.artists[drop], false)
	}
}

func (a *app) placeholderView(c *ui.Context) {
	t := c.Theme()
	title := a.albumName
	if title == "" {
		title = "这一页"
	}
	ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		ui.Text(c, title).FontSize(30).FontWeight(700)
		ui.Text(c, "还没有这一页。从左侧回到曲库。").TextColor(t.TextMuted)
	})
}

func (a *app) topBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(12, 22, 8, 22).AlignItems(ui.Center).Children(func() {
		if a.client != nil {
			if ui.SearchField(c, &a.query).Label("搜索歌曲").Placeholder("搜索歌曲").Grow(1).MaxWidth(460).Changed() {
				a.search()
			}
		} else {
			ui.Text(c, "本地音频验证").TextColor(t.TextMuted).Grow(1)
		}
	})
}

func (a *app) message(c *ui.Context) {
	if a.errorText == "" && a.status == "" {
		return
	}
	ui.Column(c).Key("notice").Padding(0, 22).Gap(2).Transition(noticeMotion).Children(func() {
		if a.errorText != "" {
			ui.Text(c, a.errorText).TextColor(c.Theme().Danger)
		}
		if a.status != "" {
			ui.Text(c, a.status).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *app) loginView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Children(func() {
		ui.Column(c).Width(420).Padding(36, 32).Gap(14).Radius(18).Background(t.Surface).Children(func() {
			ui.Text(c, "Godrome").FontSize(32).FontWeight(700).TextAlign(ui.Center)
			ui.Text(c, "连接 Navidrome / Subsonic").TextColor(t.TextMuted).TextAlign(ui.Center)
			ui.TextInput(c, &a.server).Label("服务器地址").Placeholder("https://music.example.com/navidrome").Disabled(a.busy)
			ui.TextInput(c, &a.username).Label("用户名").Placeholder("用户名").Disabled(a.busy)
			submitted := ui.TextInput(c, &a.password).Label("密码").Placeholder("密码").Password().Disabled(a.busy).Submitted()
			ui.Checkbox(c, &a.remember, "记住密码").Disabled(a.busy)
			if a.autoLogin && !a.busy && a.client == nil && a.password != "" {
				a.login()
			} else if ui.PrimaryButton(c, "登录").Disabled(a.busy).Clicked() || submitted {
				a.login()
			}
			a.message(c)
		})
	})
}

func (a *app) coverArt(c *ui.Context, id string, size float32) {
	t := c.Theme()
	var bitmap *ui.Bitmap
	if a.covers != nil {
		bitmap = a.covers.Get(id)
	}
	box := ui.Box(c).Size(size, size).Radius(8).Clip().Background(t.SurfaceHover).Shrink(0)
	if bitmap != nil {
		box.Shadow(0, 8, 18, 0, ui.RGBA(0, 0, 0, 0.16))
	}
	box.Children(func() {
		if bitmap != nil {
			image := ui.Image(c, bitmap).Size(size, size).Fit(ui.Cover).Radius(8).Transition(coverEnter)
			if id != "" {
				image.Key(id)
			}
			return
		}
		ui.Column(c).Size(size, size).Center().Children(func() {
			ui.Icon(c, iconDisc).Size(24, 24).TextColor(t.TextMuted)
		})
	})
}

// heartButton is a like mark with no resting face. The label stays so tests
// and keyboard focus can find it.
func (a *app) heartButton(c *ui.Context, label string, liked bool, size float32) bool {
	glyph := iconHeart
	if liked {
		glyph = iconHeartOn
	}
	btn := ui.Button(c, "").Label(label).Tooltip(label)
	btn.Background(ui.Color{}).BorderWidth(0).Padding(2).Shrink(0)
	// A column stretches a row button to its width and the button centers
	// its icon. Size it to the icon so the album header keeps it at the start.
	btn.Width(size + 4)
	return btn.Children(func() {
		icon := ui.Icon(c, glyph).Size(size, size)
		if liked {
			icon.TextColor(c.Theme().Accent)
		}
	}).Clicked()
}

func (a *app) albumStarButton(c *ui.Context, album subsonic.Album) {
	if a.client == nil || album.ID == "" {
		return
	}
	liked := a.albumLiked(album)
	name := album.Name
	if name == "" {
		name = "专辑"
	}
	label := "喜欢 " + name
	if liked {
		label = "取消喜欢 " + name
	}
	if a.heartButton(c, label, liked, 14) {
		a.setAlbumStar(album, !liked)
	}
}

func (a *app) albumView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.End).Padding(6, 2, 12, 2).Children(func() {
		ui.Text(c, "专辑").FontSize(30).FontWeight(700).Grow(1)
		ui.Textf(c, "第 %d 页", a.pageOffset/subsonic.PageSize+1).TextColor(t.TextMuted)
	})
	a.albumsGrid.Selected = &a.selectedAlbum
	a.albumsGrid.Key = func(i int) any { return a.albums[i].ID }
	a.albumsGrid.Label = func(i int) string { return a.albums[i].Name }
	openID := ""
	grid := ui.GridView(c, &a.albumsGrid, len(a.albums), 188, 268, func(i int) {
		album := a.albums[i]
		// The selection fills the cell. The cover is a fixed square, so the
		// card stays as wide as the cover and centers in the leftover width.
		card := ui.Column(c).Width(172).AlignSelf(ui.Center).Padding(8, 0, 6, 0).Gap(8).Cursor(ui.CursorPointer)
		card.Children(func() {
			a.coverArt(c, album.CoverArt, 172)
			ui.Text(c, album.Name).FontWeight(600).SingleLine()
			artist := album.Artist
			if artist == "" {
				artist = "未知艺人"
			}
			ui.Text(c, artist).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		if card.Clicked() {
			openID = album.ID
		}
	}).Grow(1)
	if grid.Submitted() && a.selectedAlbum >= 0 && a.selectedAlbum < len(a.albums) {
		openID = a.albums[a.selectedAlbum].ID
	}
	if !a.busy && len(a.albums) == 0 {
		ui.Text(c, "没有专辑").TextColor(t.TextMuted).Padding(12)
	}
	ui.Row(c).Gap(8).Padding(10, 0, 4, 0).Children(func() {
		if ui.Button(c, "上一页").Disabled(a.busy || a.pageOffset == 0).Clicked() {
			a.browse(a.pageOffset - subsonic.PageSize)
		}
		if ui.Button(c, "下一页").Disabled(a.busy || !a.more).Clicked() {
			a.browse(a.pageOffset + subsonic.PageSize)
		}
		if ui.Button(c, "刷新").Disabled(a.busy).Clicked() {
			a.pages = nil
			a.browse(a.pageOffset)
		}
	})
	if openID != "" {
		a.openAlbum(openID)
	}
}

func (a *app) songView(c *ui.Context) {
	switch a.screen {
	case screenSearch:
		a.searchHeader(c)
	case screenLibrary:
		a.libraryHeader(c)
	case screenAlbum:
		a.albumHeader(c)
	default:
		a.localHeader(c)
	}
	empty := ""
	if a.screen == screenSearch && a.query != "" {
		empty = "没有找到与「" + a.query + "」匹配的歌曲"
	}
	a.songList(c, empty)
	if a.screen == screenLibrary {
		ui.Row(c).Gap(8).Padding(10, 0, 4, 0).Children(func() {
			if ui.Button(c, "上一页").Disabled(a.busy || a.songOffset == 0).Clicked() {
				a.loadSongs(a.songOffset - subsonic.PageSize)
			}
			if ui.Button(c, "下一页").Disabled(a.busy || !a.more).Clicked() {
				a.loadSongs(a.songOffset + subsonic.PageSize)
			}
		})
	}
}

func (a *app) libraryHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.End).MinWidth(0).Padding(8, 2, 16, 2).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "曲库").FontSize(30).FontWeight(700).SingleLine()
			ui.Textf(c, "本页 %d 首", len(a.songs)).TextColor(t.TextMuted)
			ui.Textf(c, "第 %d 页", a.songOffset/subsonic.PageSize+1).TextColor(t.TextMuted)
		})
		if ui.Button(c, "播放本页").Disabled(a.busy || len(a.songs) == 0).Clicked() {
			a.playSongs(0)
		}
	})
}

func (a *app) searchHeader(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).MinWidth(0).Gap(4).Padding(8, 2, 16, 2).Children(func() {
		ui.Text(c, "搜索结果").FontSize(30).FontWeight(700).SingleLine()
		if a.query != "" {
			ui.Text(c, a.query).TextColor(t.TextMuted).SingleLine()
		}
		ui.Textf(c, "本次返回 %d 首", len(a.songs)).TextColor(t.TextMuted)
		if len(a.songs) == subsonic.PageSize {
			ui.Text(c, "最多显示 100 首").FontSize(12).TextColor(t.TextMuted)
		}
		if a.searching {
			ui.Text(c, a.status).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *app) albumHeader(c *ui.Context) {
	t := c.Theme()
	head := a.albumHead
	if head.Name == "" {
		head.Name = a.albumName
	}
	if head.Name == "" {
		head.Name = "专辑"
	}
	if head.ID == "" {
		head.ID = a.albumID
	}
	header := func() {
		ui.Text(c, head.Name).FontSize(30).FontWeight(700).SingleLine()
		if head.ID != "" {
			a.albumStarButton(c, subsonic.Album{ID: head.ID, Name: head.Name})
		}
		if head.Artist != "" {
			ui.Text(c, head.Artist).TextColor(t.TextMuted)
		}
		ui.Textf(c, "%d 首", len(a.songs)).TextColor(t.TextMuted)
		if ui.Button(c, "播放").Disabled(a.busy || len(a.songs) == 0).Clicked() {
			a.playSongs(0)
		}
	}
	// Row padding, the gap, and room for the title plus the heart.
	stacked := a.pageWidth(c) < 148+18+96
	if stacked {
		coverSize := min(float32(148), max(float32(72), a.pageWidth(c)-4))
		ui.Column(c).Gap(12).MinWidth(0).Padding(8, 2, 16, 2).Children(func() {
			a.coverArt(c, head.CoverArt, coverSize)
			ui.Column(c).MinWidth(0).Gap(4).Children(header)
		})
		return
	}
	ui.Row(c).Gap(18).AlignItems(ui.End).MinWidth(0).Padding(8, 2, 16, 2).Children(func() {
		a.coverArt(c, head.CoverArt, 148)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(header)
	})
}

func (a *app) localHeader(c *ui.Context) {
	t := c.Theme()
	title := a.albumName
	if title == "" {
		title = "本地音频"
	}
	ui.Column(c).MinWidth(0).Gap(4).Padding(8, 2, 16, 2).Children(func() {
		ui.Text(c, title).FontSize(30).FontWeight(700).SingleLine()
		ui.Textf(c, "%d 首", len(a.songs)).TextColor(t.TextMuted)
	})
}

// albumHeaderData is the album page's own title. It is not the first song.
type albumHeaderData struct {
	ID, Name, Artist, CoverArt string
}

// songList is the virtual song list. emptyTitle is the copy shown when the
// page has no songs and is not still loading. An empty emptyTitle keeps
// the existing "没有歌曲" copy used by album, search, and library pages.
func (a *app) songList(c *ui.Context, emptyTitle string) {
	t := c.Theme()
	// The list writes its scroll into this state while laying out, after
	// the view returns. A copy would drop that place on the next frame.
	a.songsList.Selected = &a.selectedSong
	a.songsList.Key = func(i int) any { return a.songs[i].ID }
	a.songsList.Label = func(i int) string { return a.songs[i].Title }
	play, add := -1, -1
	type starChange struct {
		index int
		on    bool
	}
	star := starChange{index: -1}
	// The scroll thumb is painted over the row. Right padding keeps the
	// selection highlight clear of it.
	list := ui.List(c, &a.songsList, len(a.songs), func(i int) {
		song := a.songs[i]
		playing := false
		if current, ok := a.play.queue.Current(); ok && current.ID == song.ID && a.play.queue.Index >= 0 {
			playing = a.playback.State == player.Playing || a.playback.State == player.Paused || a.playback.State == player.Buffering
		}
		// The list keeps a gutter so the selection highlight clears the
		// scrollbar. The two actions sit in that gutter, against the edge.
		gutter := listScrollGutter(t)
		ui.Row(c).Padding(7, 0, 7, 8).Gap(12).AlignItems(ui.Center).Children(func() {
			mark := fmt.Sprintf("%d", i+1)
			if playing {
				mark = "▶"
			}
			num := ui.Text(c, mark).Width(28).TextAlign(ui.End).TextColor(t.TextMuted).FontFeatures("tnum")
			if playing && a.selectedSong != i {
				num.TextColor(t.Accent)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				name := ui.Text(c, song.Title).SingleLine()
				if playing && a.selectedSong != i {
					name.TextColor(t.Accent).FontWeight(600)
				}
				artist := ui.Text(c, song.Artist).FontSize(12).TextColor(t.TextMuted).SingleLine()
				if a.selectedSong == i {
					artist.TextColor(t.AccentText.Alpha(0.82))
				}
			})
			ui.Text(c, clockText(song.Duration)).TextColor(t.TextMuted).FontFeatures("tnum").Shrink(1).MinWidth(0)
			// The list gutter is wider than these two buttons. Pull them by
			// what is left, so the last icon meets the list's right edge.
			pull := gutter - 40
			if pull < 0 {
				pull = 0
			}
			ui.Row(c).Gap(0).Margin(0, -pull, 0, 0).AlignItems(ui.Center).Shrink(0).Children(func() {
				index := i
				liked := a.songLiked(song)
				heartLabel := "喜欢"
				if liked {
					heartLabel = "取消喜欢"
				}
				if song.Path == "" && a.client != nil && a.heartButton(c, heartLabel, liked, 14) {
					star = starChange{index: index, on: !liked}
				}
				more := ui.Button(c, "").Label("更多").Tooltip("更多")
				more.Background(ui.Color{}).BorderWidth(0).Padding(2)
				more.Menu(func(m *ui.Menu) {
					if m.Item("播放").Chosen() {
						play = index
					}
					if m.Item("加入队列").Chosen() {
						add = index
					}
					if song.Path == "" && a.client != nil {
						label := "喜欢"
						if liked {
							label = "取消喜欢"
						}
						if m.Item(label).Chosen() {
							star = starChange{index: index, on: !liked}
						}
					}
				}).Children(func() { ui.Icon(c, iconMore).Size(14, 14) })
			})
		})
	}).Padding(0, listScrollGutter(t), 0, 0).Grow(1).Children(func() {
		if !a.busy && len(a.songs) == 0 && emptyTitle != "" {
			ui.Text(c, emptyTitle).TextColor(t.TextMuted).Padding(12)
		} else if !a.busy && len(a.songs) == 0 {
			ui.Text(c, "没有歌曲").TextColor(t.TextMuted).Padding(12)
		}
	})
	if list.Submitted() {
		play = a.selectedSong
	}
	if add >= 0 && add < len(a.songs) {
		a.play.queue.Add(a.songs[add])
	}
	if star.index >= 0 && star.index < len(a.songs) {
		a.setSongStar(a.songs[star.index], star.on)
	}
	if play >= 0 {
		a.playSongs(play)
	}
}

func (a *app) queueView(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "播放队列").FontSize(13).FontWeight(700).Padding(4, 8, 2, 8)
	ui.Textf(c, "%d 首", len(a.play.queue.Songs)).FontSize(12).TextColor(t.TextMuted).Padding(0, 8, 8, 8)
	// The list writes its scroll into this state while laying out, after
	// the view returns. A copy would drop that place on the next frame.
	a.queueList.Selected = &a.selectedQueue
	// Index keys allow adding the same song more than once.
	a.queueList.Label = func(i int) string { return a.play.queue.Songs[i].Title }
	chosen := -1
	list := ui.List(c, &a.queueList, len(a.play.queue.Songs), func(i int) {
		song := a.play.queue.Songs[i]
		current := i == a.play.queue.Index
		ui.Row(c).Padding(5, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			if current {
				ui.Icon(c, iconPlay).Size(11, 11).TextColor(t.Accent)
			} else {
				ui.Box(c).Size(11, 11)
			}
			name := ui.Text(c, song.Title).Grow(1).SingleLine()
			if current && a.selectedQueue != i {
				name.TextColor(t.Accent).FontWeight(600)
			}
			index := i
			ui.Toolbar(c, func() {
				if ui.Button(c, "").Label("播放此曲").Tooltip("播放此曲").Children(func() {
					ui.Icon(c, iconPlay).Size(14, 14)
				}).Clicked() {
					chosen = index
				}
			})
		})
	}).Padding(0, listScrollGutter(t), 0, 0).Grow(1).Children(func() {
		if len(a.play.queue.Songs) == 0 {
			ui.Text(c, "队列为空").TextColor(t.TextMuted).Padding(8, 10)
		}
	})
	if list.Submitted() {
		chosen = a.selectedQueue
	}
	if chosen >= 0 && a.play.queue.Select(chosen) {
		a.playCurrent()
	}
}

func (a *app) iconButton(c *ui.Context, glyph *ui.SVG, label string, primary, disabled bool) bool {
	var btn *ui.Element
	if primary {
		btn = ui.PrimaryButton(c, "")
	} else {
		btn = ui.Button(c, "")
	}
	btn.Label(label).Tooltip(label).Disabled(disabled).Children(func() {
		ui.Icon(c, glyph).Size(16, 16)
	})
	return btn.Clicked()
}

func (a *app) toggleMute() {
	if a.volume > 0 {
		a.volumeBefore = a.volume
		a.volume = 0
	} else {
		if a.volumeBefore <= 0 {
			a.volumeBefore = 70
		}
		a.volume = a.volumeBefore
	}
	a.control(a.engine.SetVolume(a.volume))
}

// volumeControl keeps mute one click away. A narrow window opens the
// slider from the speaker instead of keeping it in the bar.
func (a *app) volumeControl(c *ui.Context, t *ui.Theme, compact bool) {
	glyph, tip := iconVolume, "静音"
	if a.volume <= 0 {
		glyph, tip = iconVolumeMute, "取消静音"
	}
	mute := ui.Button(c, "").Label("静音").Tooltip(tip).Padding(2)
	mute.Background(ui.Color{}).BorderWidth(0)
	mute.Children(func() { ui.Icon(c, glyph).Size(16, 16).TextColor(t.TextMuted) })
	if mute.Clicked() {
		if compact {
			a.volumeShown = !a.volumeShown
		} else {
			a.toggleMute()
		}
	}
	// A narrow bar hides the slider until the speaker is pressed. The slider
	// stays in the bar, so the press that revealed it does not dismiss it.
	if compact && !a.volumeShown {
		return
	}
	a.volumeSlider(c)
}

func (a *app) volumeSlider(c *ui.Context) {
	if ui.Slider(c, &a.volume, 0, 100).Label("音量").Width(110).Changed() {
		if a.volume > 0 {
			a.volumeBefore = a.volume
		}
		a.control(a.engine.SetVolume(a.volume))
	}
}

// queueOverlay is the queue when the window cannot dock it. Closing it
// does not clear the saved request to show the queue.
func (a *app) queueOverlay(c *ui.Context) {
	ui.Overlay(c, func() {
		t := c.Theme()
		panel := ui.Column(c).Absolute().Width(240).Padding(12).Gap(6).Radius(12).Background(t.Surface).Border(1, t.Border)
		panel.Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
		panel.Right(12).Bottom(92)
		panel.Children(func() {
			ui.Text(c, "播放队列").FontSize(13).FontWeight(700)
			ui.Textf(c, "%d 首", len(a.play.queue.Songs)).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "窗口较窄，队列已折叠。加宽窗口后恢复。").FontSize(12).TextColor(t.TextMuted)
		})
	})
}

func (a *app) controlsView(c *ui.Context) {
	t := c.Theme()
	mode := a.layoutMode(c)
	song, has := a.play.queue.Current()
	title, artist := "未在播放", ""
	cover := ""
	if has {
		title = song.Title
		artist = song.Artist
		cover = song.CoverArt
	}
	ui.Column(c).Shrink(0).Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Padding(8, 16, 10, 16).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, clockText(a.position)).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum").Width(40).TextAlign(ui.End)
			playable := a.playback.State == player.Playing || a.playback.State == player.Paused || a.playback.State == player.Buffering
			if ui.Slider(c, &a.position, 0, max(1, a.playback.Duration)).Label("播放进度").Grow(1).MinWidth(160).MaxWidth(560).Disabled(!playable || a.playback.Duration <= 0).Changed() {
				a.control(a.engine.Seek(a.position))
			}
			ui.Text(c, clockText(a.playback.Duration)).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum").Width(40)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			if has && a.screen != screenLocal {
				a.coverArt(c, cover, 42)
			}
			ui.Column(c).Grow(1).Shrink(1).Basis(0).MinWidth(0).MaxWidth(220).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, title).FontWeight(600).SingleLine().Shrink(1)
					if a.playback.State == player.Loading || a.playback.State == player.Buffering {
						ui.Spinner(c).Size(12, 12).Label("缓冲")
					}
				})
				if artist != "" {
					ui.Text(c, artist).FontSize(12).TextColor(t.TextMuted).SingleLine()
				}
			})
			ui.Toolbar(c, func() {
				glyph, label := iconOrder, "顺序播放"
				switch a.play.repeat {
				case repeatAll:
					glyph, label = iconRepeat, "列表循环"
				case repeatOne:
					glyph, label = iconRepeatOne, "单曲循环"
				}
				if a.iconButton(c, glyph, label, false, false) {
					a.play.cycleRepeat()
				}
				if a.iconButton(c, iconPrevious, "上一曲", false, a.play.atStart()) && a.play.previous() {
					a.playCurrent()
				}
				label, glyph = "暂停", iconPause
				if a.playback.State == player.Paused {
					label, glyph = "继续", iconPlay
				}
				playable := a.playback.State == player.Playing || a.playback.State == player.Paused || a.playback.State == player.Buffering
				if a.iconButton(c, glyph, label, true, !playable) {
					a.control(a.engine.Pause(a.playback.State != player.Paused))
				}
				if a.iconButton(c, iconNext, "下一曲", false, a.play.atEnd()) && a.play.next() {
					a.playCurrent()
				}
				if a.iconButton(c, iconRetry, "重试", false, !has) {
					a.playCurrent()
				}
				if a.iconButton(c, iconStop, "停止", false, !has) {
					a.generation++
					a.control(a.engine.Stop())
					a.playback = player.Event{Generation: a.generation, State: player.Idle}
					a.position = 0
				}
			}).Grow(1).Justify(ui.Center)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Toolbar(c, func() {
					ui.Toggle(c, &a.queueVisible, "").Label("播放列表").Tooltip("播放列表").Children(func() {
						ui.Icon(c, iconQueue).Size(16, 16)
					})
				})
				a.volumeControl(c, t, mode.compact)
			})
		})
	})
}
