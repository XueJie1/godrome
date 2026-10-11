package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"godrome/internal/config"
	"godrome/internal/cover"
	"godrome/internal/player"
	"godrome/internal/subsonic"

	"github.com/egoist/mygo/ui"
)

type playback interface {
	Load(string, uint64) error
	Pause(bool) error
	Seek(float64) error
	SetVolume(float64) error
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
	post                                       func(func())
	engine                                     playback
	client                                     *subsonic.Client
	settings                                   config.Settings
	configPath                                 string
	server, username, password, query          string
	status, errorText                          string
	busy                                       bool
	screen                                     screen
	pageOffset, songOffset                     int
	pages                                      []albumPage
	albums                                     []subsonic.Album
	songs                                      []subsonic.Song
	albumName                                  string
	more                                       bool
	pageCancel                                 context.CancelFunc
	pageCtx                                    context.Context
	requestID                                  uint64
	covers                                     *cover.Cache
	play                                       playbackState
	playback                                   player.Event
	generation                                 uint64
	position, volume                           float64
	albumsGrid                                 ui.GridState
	songsList, queueList                       ui.ListState
	selectedAlbum, selectedSong, selectedQueue int
	queueVisible                               bool
	sidebarWidth                               float32
	queueWidth                                 float32
	albumID                                    string
	nav                                        history
	restoring                                  bool
	closeOnce                                  sync.Once
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
	return &app{ctx: ctx, cancel: cancel, engine: engine, settings: settings, configPath: path, server: settings.Server, username: settings.Username, volume: settings.Volume, play: newPlayback(), playback: player.Event{State: player.Idle}, selectedAlbum: -1, selectedSong: -1, selectedQueue: -1, queueVisible: true, sidebarWidth: sidebar, queueWidth: queue, screen: screenLogin}
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
	a.songsList = ui.ListState{}
	a.albumsGrid = ui.GridState{}
	return a.pageCtx, a.requestID
}
func (a *app) finish(id uint64, err error) bool {
	if id != a.requestID {
		return false
	}
	a.busy = false
	a.status = ""
	if err != nil && !errors.Is(err, context.Canceled) {
		a.errorText = err.Error()
	}
	return err == nil
}
func (a *app) setupCovers(ctx context.Context) {
	client := a.client
	a.covers = cover.New(ctx, client.Cover, func() { a.dispatch(func() {}) })
}
func (a *app) login() {
	client, err := subsonic.New(a.server, a.username, a.password)
	if err != nil {
		a.errorText = err.Error()
		return
	}
	ctx, id := a.beginPage()
	go func() {
		err := client.Ping(ctx)
		a.dispatch(func() {
			if !a.finish(id, err) {
				return
			}
			a.client = client
			a.password = ""
			a.pages = nil
			a.settings.Server = a.server
			a.settings.Username = a.username
			a.browse(0)
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
		a.showPlaceholder(screenStarred, "喜爱")
	case screenPlaylists:
		a.showPlaceholder(screenPlaylists, "歌单")
	case screenLocal:
		a.screen = screenLocal
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
	a.screen = screenLibrary
	ctx, id := a.beginPage()
	a.songOffset = max(0, offset)
	offset = a.songOffset
	a.albumName = ""
	a.albumID = ""
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
		})
	}()
}

func (a *app) showPlaceholder(s screen, title string) {
	if a.pageCancel != nil {
		a.pageCancel()
	}
	a.screen = s
	a.albumName = title
	a.albumID = ""
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
	a.screen = screenAlbums
	ctx, id := a.beginPage()
	a.pageOffset = max(0, offset)
	offset = a.pageOffset
	a.albumName = ""
	a.albumID = ""
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
	a.screen = screenAlbum
	a.albumID = id
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
			a.nav.note(a.here())
		})
	}()
}
func (a *app) search() {
	query := a.query
	if query == "" {
		a.browse(a.pageOffset)
		return
	}
	a.searchNow(query)
}

// searchNow runs a search whose query is already chosen. The debounce
// still applies, so a history return does not hit the server at once.
func (a *app) searchNow(query string) {
	if query == "" {
		a.browse(a.pageOffset)
		return
	}
	a.query = query
	a.screen = screenSearch
	a.albumID = ""
	a.albumName = ""
	a.visit()
	ctx, id := a.beginPage()
	client := a.client
	a.setupCovers(ctx)
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
			a.songs = songs
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
func (a *app) logout() {
	a.generation++
	a.control(a.engine.Stop())
	a.screen = screenLogin
	a.nav = history{}
	a.albumID = ""
	a.beginPage()
	a.busy = false
	a.status = ""
	a.client = nil
	a.pages = nil
	a.play = newPlayback()
	a.query = ""
	a.password = ""
	a.position = 0
	a.playback = player.Event{Generation: a.generation, State: player.Idle}
}
func (a *app) local(paths []string) {
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

// Apple Music's pink, used as the accent while the system theme still supplies
// surfaces, text, fonts and scaling.
func appleMusic(c *ui.Context) *ui.Theme {
	t := *c.Theme()
	if t.Dark {
		t.Accent = ui.Hex("#fc3c44")
		t.AccentHover = ui.Hex("#ff5a60")
		t.AccentPressed = ui.Hex("#d12b36")
		t.Focus = ui.RGBA(252, 60, 68, 0.6)
		t.Selection = ui.RGBA(252, 60, 68, 0.35)
	} else {
		t.Accent = ui.Hex("#fa233b")
		t.AccentHover = ui.Hex("#e11d34")
		t.AccentPressed = ui.Hex("#c4162c")
		t.Focus = ui.RGBA(250, 35, 59, 0.5)
		t.Selection = ui.RGBA(250, 35, 59, 0.22)
	}
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
	t := appleMusic(c)
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		if a.screen == screenLogin {
			a.loginView(c)
			return
		}
		ui.Split(c, &a.sidebarWidth, func() { a.sidebar(c) }, func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.topBar(c)
				a.message(c)
				if a.queueOpen() {
					// The value Split stores is the content width. The queue
					// width is what is left, so the drag matches the pane on
					// the right.
					content := a.contentWidth(c)
					ui.Split(c, &content, func() { a.mainColumn(c) }, func() { a.queuePane(c) }).Grow(1).MinHeight(0)
					a.queueWidth = a.widthFromContent(c, content)
				} else {
					a.mainColumn(c)
				}
			})
		}).Grow(1).MinHeight(0)
		a.clampColumns(c)
		a.controlsView(c)
	})
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

// columnRoom is the window width and the width beside the sidebar divider.
func (a *app) columnRoom(c *ui.Context) (window, afterSidebar float32) {
	window, _ = c.Size()
	return window, window - a.sidebarWidth - 1
}

// clampColumns keeps both panes inside their limits after a drag or a
// resize. Split itself only keeps 40 DIPs from either edge.
func (a *app) clampColumns(c *ui.Context) {
	a.sidebarWidth = clampWidth(a.sidebarWidth, sidebarMinWidth, sidebarMaxWidth)
	_, after := a.columnRoom(c)
	if after > 0 {
		a.sidebarWidth = min(a.sidebarWidth, max(sidebarMinWidth, after-40))
	}
	if !a.queueVisible || after <= 1 {
		a.queueWidth = clampWidth(a.queueWidth, queueMinWidth, queueMaxWidth)
		return
	}
	hi := min(queueMaxWidth, after-40-1)
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
		case screenStarred, screenPlaylists:
			a.placeholderView(c)
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
		}).Grow(1).Changed() {
			switch section {
			case "library":
				a.showLibrary()
			case "albums":
				if a.client != nil {
					a.browse(a.pageOffset)
				}
			case "starred":
				a.showPlaceholder(screenStarred, "喜爱")
			case "playlists":
				a.showPlaceholder(screenPlaylists, "歌单")
			}
		}
		if a.client != nil {
			ui.Column(c).Padding(8, 12, 14, 12).Children(func() {
				if ui.Button(c, "退出登录").Clicked() {
					a.logout()
				}
			})
		}
	})
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
			submitted := ui.TextInput(c, &a.password).Label("密码").Placeholder("密码（仅本次会话）").Password().Disabled(a.busy).Submitted()
			if ui.PrimaryButton(c, "登录").Disabled(a.busy).Clicked() || submitted {
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
		card := ui.Column(c).Padding(8, 8, 6, 8).Gap(8).Cursor(ui.CursorPointer)
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
	t := c.Theme()
	title := a.albumName
	if title == "" {
		title = "歌曲"
	}
	library := a.screen == screenLibrary
	switch a.screen {
	case screenSearch:
		title = "搜索结果"
	case screenLibrary:
		title = "曲库"
	}
	artist := ""
	if len(a.songs) > 0 {
		artist = a.songs[0].Artist
	}
	cover := ""
	if len(a.songs) > 0 {
		cover = a.songs[0].CoverArt
	}
	ui.Row(c).Gap(18).AlignItems(ui.End).Padding(8, 2, 16, 2).Children(func() {
		if a.screen != screenLocal && !library {
			a.coverArt(c, cover, 148)
		}
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(30).FontWeight(700).SingleLine()
			if artist != "" {
				ui.Text(c, artist).TextColor(t.TextMuted)
			}
			ui.Textf(c, "%d 首", len(a.songs)).TextColor(t.TextMuted)
		})
	})
	a.songsList.Selected = &a.selectedSong
	a.songsList.Key = func(i int) any { return a.songs[i].ID }
	a.songsList.Label = func(i int) string { return a.songs[i].Title }
	play, add := -1, -1
	// The scroll thumb is painted over the row. Right padding keeps the
	// selection highlight clear of it.
	list := ui.List(c, &a.songsList, len(a.songs), func(i int) {
		song := a.songs[i]
		playing := false
		if current, ok := a.play.queue.Current(); ok && current.ID == song.ID && a.play.queue.Index >= 0 {
			playing = a.playback.State == player.Playing || a.playback.State == player.Paused || a.playback.State == player.Buffering
		}
		ui.Row(c).Padding(7, 8).Gap(12).AlignItems(ui.Center).Children(func() {
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
			ui.Text(c, clockText(song.Duration)).TextColor(t.TextMuted).FontFeatures("tnum")
			index := i
			ui.Toolbar(c, func() {
				ui.Button(c, "").Label("更多").Tooltip("更多").Menu(func(m *ui.Menu) {
					if m.Item("播放").Chosen() {
						play = index
					}
					if m.Item("加入队列").Chosen() {
						add = index
					}
				}).Children(func() { ui.Icon(c, iconMore).Size(16, 16) })
			})
		})
	}).Padding(0, listScrollGutter(t), 0, 0).Grow(1).Children(func() {
		if !a.busy && len(a.songs) == 0 {
			ui.Text(c, "没有歌曲").TextColor(t.TextMuted).Padding(12)
		}
	})
	if list.Submitted() {
		play = a.selectedSong
	}
	if add >= 0 {
		a.play.queue.Add(a.songs[add])
	}
	if play >= 0 {
		a.playSongs(play)
	}
	if library {
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

func (a *app) queueView(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, "播放队列").FontSize(13).FontWeight(700).Padding(4, 8, 2, 8)
	ui.Textf(c, "%d 首", len(a.play.queue.Songs)).FontSize(12).TextColor(t.TextMuted).Padding(0, 8, 8, 8)
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

func (a *app) controlsView(c *ui.Context) {
	t := c.Theme()
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
			ui.Column(c).Width(220).MinWidth(0).Children(func() {
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
				ui.Text(c, "音量").FontSize(12).TextColor(t.TextMuted)
				if ui.Slider(c, &a.volume, 0, 100).Label("音量").Width(110).Changed() {
					a.control(a.engine.SetVolume(a.volume))
				}
			})
		})
	})
}
