//go:build ignore

// A separate native acceptance driver, compiled with app.go and metrics.go.
// It shares the production views/API/player; it is never part of Godrome builds.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"godrome/internal/config"
	"godrome/internal/player"
	"godrome/internal/subsonic"
)

func main() {
	scenario := flag.String("scenario", "empty", "empty, albums, library, covers, mp3, flac, m4a, opus, paused, switch, desktop, live")
	duration := flag.Duration("quit-after", time.Minute, "actual measurement duration")
	metrics := flag.String("metrics", "", "Go heap CSV; no forced GC")
	report := flag.String("report", "", "safe acceptance counters JSON")
	samples := flag.String("samples", "testdata/audio", "generated audio directory")
	audio := flag.String("audio-output", "null", "null or system default (empty)")
	volume := flag.Float64("volume", 50, "test volume, capped at 50 percent")
	_ = flag.String("config", "", "reserved for measure.py; no configuration is saved")
	flag.Parse()
	if *duration <= 0 {
		panic("positive duration required")
	}
	valid := map[string]bool{"empty": true, "albums": true, "library": true, "covers": true, "mp3": true, "flac": true, "m4a": true, "opus": true, "paused": true, "switch": true, "desktop": true, "live": true}
	if !valid[*scenario] {
		panic("unknown scenario")
	}
	stopMetrics, err := startMetrics(*metrics)
	if err != nil {
		panic("cannot create measurement file")
	}
	defer stopMetrics()
	settings := config.Defaults()
	settings.Volume = max(0, min(50, *volume))
	a := newApp(settings, "", acceptancePlayback{player.New(player.Options{AudioOutput: *audio})})
	defer a.close()
	var coverRequests, albumRequests atomic.Int64
	var fixture *httptest.Server
	if *scenario == "live" {
		a.server, a.username, a.password = os.Getenv("GODROME_TEST_SERVER"), os.Getenv("GODROME_TEST_USER"), os.Getenv("GODROME_TEST_PASSWORD")
		if a.server == "" || a.password == "" {
			panic("live session credentials required")
		}
	} else if *scenario == "albums" || *scenario == "library" || *scenario == "covers" || *scenario == "desktop" {
		fixture = acceptanceFixture(*scenario != "albums", *samples, &coverRequests, &albumRequests)
		defer fixture.Close()
		var secret [24]byte
		if _, err := rand.Read(secret[:]); err != nil {
			panic("cannot generate fixture session")
		}
		a.server, a.username, a.password = fixture.URL, "fixture", hex.EncodeToString(secret[:])
	} else if *scenario != "empty" {
		formats := []string{*scenario}
		if *scenario == "paused" {
			formats = []string{"flac"}
		} else if *scenario == "switch" {
			formats = []string{"mp3", "flac", "m4a", "opus"}
		}
		var paths []string
		for _, format := range formats {
			path, err := filepath.Abs(filepath.Join(*samples, "sample."+format))
			if err != nil {
				panic("invalid sample path")
			}
			if _, err := os.Stat(path); err != nil {
				panic("generate audio samples first")
			}
			paths = append(paths, path)
		}
		a.local(paths)
	}
	started := time.Now()
	steps, pauses, playingEvents, errorsSeen, maxPages, maxSongs := 0, 0, 0, 0, 0, 0
	lastState := player.Idle
	var lastPlayingGeneration uint64
	var timeline []map[string]any
	var firstError string
	writeReport := func() {
		if *report == "" {
			return
		}
		data, _ := json.MarshalIndent(map[string]any{"scenario": *scenario, "elapsed_s": time.Since(started).Seconds(), "steps": steps, "generation": a.generation, "playing_transitions": playingEvents, "pause_transitions": pauses, "error_count": errorsSeen, "first_error": firstError, "cover_requests": coverRequests.Load(), "album_requests": albumRequests.Load(), "max_cached_pages": maxPages, "max_songs": maxSongs, "queue_length": len(a.play.queue.Songs), "position": a.position, "state": playbackLabel(a.playback.State), "timeline": timeline}, "", "  ")
		if err := os.WriteFile(*report, data, 0600); err != nil {
			panic("cannot save acceptance report")
		}
	}
	mygo.App.WhenReady(func() {
		view := func(c *ui.Context) {
			a.volume = min(50, a.volume)
			a.view(c)
		}
		if *scenario == "empty" {
			view = func(c *ui.Context) { ui.Column(c).Fill() }
		}
		win := mygo.NewWindow(mygo.WindowOptions{Title: "Godrome 验收 · " + *scenario, Width: 1080, Height: 720, Content: ui.View(view)})
		a.post = win.Update
		a.listen()
		a.control(a.engine.SetVolume(settings.Volume))
		win.OnHide(func() { a.control(a.engine.SetVisible(false)) })
		win.OnShow(func() { a.control(a.engine.SetVisible(true)) })
		win.OnMinimize(func() { a.control(a.engine.SetVisible(false)) })
		win.OnRestore(func() { a.control(a.engine.SetVisible(true)) })
		win.OnClose(func(*mygo.CloseEvent) { writeReport(); a.close() })
		win.OnClosed(func() { mygo.App.Quit() })
		if *scenario == "live" || (fixture != nil && *scenario != "desktop") {
			a.login()
		} else if len(a.songs) > 0 {
			a.playSongs(0)
		}
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-ticker.C:
					win.Update(func() {
						maxPages = max(maxPages, len(a.pages))
						maxSongs = max(maxSongs, len(a.songs))
						if a.errorText != "" && firstError == "" {
							firstError = a.errorText
							// The production error paths redact authenticated URLs.
							errorsSeen++
						}
						if a.playback.State != lastState {
							if a.playback.State == player.Paused {
								pauses++
							}
							lastState = a.playback.State
						}
						if a.playback.State == player.Playing && a.generation != lastPlayingGeneration {
							playingEvents++
							lastPlayingGeneration = a.generation
						}
						if *scenario == "desktop" || *scenario == "live" {
							timeline = append(timeline, map[string]any{"elapsed_s": time.Since(started).Seconds(), "page": a.screen.legacyPage(), "state": playbackLabel(a.playback.State), "generation": a.generation, "position": a.position, "selected_album": a.selectedAlbum, "selected_song": a.selectedSong, "selected_queue": a.selectedQueue, "queue_length": len(a.play.queue.Songs)})
						}
						if time.Since(started) >= *duration {
							win.Close()
							return
						}
						if a.busy || *scenario == "desktop" {
							return
						}
						steps++
						switch *scenario {
						case "library":
							if steps%12 == 0 {
								if a.screen == screenAlbums && len(a.albums) > 0 {
									a.openAlbum(a.albums[0].ID)
								} else if a.screen == screenLibrary || a.screen == screenAlbum {
									a.query = "测试"
									a.search()
								} else if a.screen == screenSearch {
									a.browse((steps / 12 % 100) * 100)
								}
							} else if (a.screen == screenLibrary || a.screen == screenAlbum) && len(a.songs) > 0 {
								a.songsList.ScrollTo((steps*577)%len(a.songs), ui.Start)
							}
						case "live":
							if a.screen == screenAlbums && len(a.albums) > 0 {
								a.openAlbum(a.albums[0].ID)
							} else if a.screen == screenAlbum && len(a.songs) > 0 && len(a.play.queue.Songs) == 0 {
								a.playSongs(0)
							}
						case "albums", "covers":
							if a.screen == screenAlbums && len(a.albums) > 0 {
								a.albumsGrid.ScrollTo((steps*9)%len(a.albums), ui.Start)
								if steps%12 == 0 {
									a.browse((steps / 12 % 100) * 100)
								}
							}
						case "paused":
							if a.playback.State == player.Playing {
								a.control(a.engine.Pause(true))
							}
						case "switch":
							if steps%4 == 0 {
								index := (a.play.queue.Index + 1) % len(a.play.queue.Songs)
								a.play.queue.Select(index)
								a.playCurrent()
							}
						case "mp3", "flac", "m4a", "opus":
							if a.playback.State == player.Ended {
								a.playCurrent()
							}
						}
						if len(a.pages) > 5 {
							panic("album page budget exceeded")
						}
					})
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "native acceptance window failed")
		os.Exit(1)
	}
	if errorsSeen > 0 {
		os.Exit(1)
	}
}

// Test output is capped even when someone interacts with the volume slider.
type acceptancePlayback struct{ playback }

func (p acceptancePlayback) SetVolume(value float64) error {
	return p.playback.SetVolume(min(50, value))
}

func acceptanceFixture(covers bool, samples string, coverRequests, albumRequests *atomic.Int64) *httptest.Server {
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := range 96 {
		for x := range 96 {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), 160, 255})
		}
	}
	var cover bytes.Buffer
	_ = png.Encode(&cover, img)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any
		switch r.URL.Path {
		case "/rest/ping.view":
			payload = map[string]any{"status": "ok"}
		case "/rest/getAlbumList2.view":
			albumRequests.Add(1)
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			albums := make([]subsonic.Album, 100)
			for i := range albums {
				id := strconv.Itoa(offset + i)
				albums[i] = subsonic.Album{ID: id, Name: fmt.Sprintf("测试专辑 %05d", offset+i), Artist: "自生成曲库"}
				if covers {
					albums[i].CoverArt = id
				}
			}
			payload = map[string]any{"status": "ok", "albumList2": map[string]any{"album": albums}}
		case "/rest/getAlbum.view", "/rest/search3.view":
			songs := make([]subsonic.Song, 10000)
			for i := range songs {
				songs[i] = subsonic.Song{ID: strconv.Itoa(i), Title: fmt.Sprintf("测试歌曲 %05d", i), Artist: "自生成曲库", Duration: 20}
			}
			if r.URL.Path == "/rest/search3.view" {
				payload = map[string]any{"status": "ok", "searchResult3": map[string]any{"song": songs[:100]}}
			} else {
				payload = map[string]any{"status": "ok", "album": subsonic.Album{Name: "一万首测试歌曲", Songs: songs}}
			}
		case "/rest/getCoverArt.view":
			coverRequests.Add(1)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(cover.Bytes())
			return
		case "/rest/stream.view":
			id, _ := strconv.Atoi(r.URL.Query().Get("id"))
			formats := []string{"mp3", "flac", "m4a", "opus"}
			http.ServeFile(w, r, filepath.Join(samples, "sample."+formats[max(0, id)%4]))
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": payload})
	}))
}
