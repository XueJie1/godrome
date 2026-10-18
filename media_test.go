package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"godrome/internal/mpris"
	"godrome/internal/player"
	"godrome/internal/subsonic"
)

func TestSpaceTogglesPlayback(t *testing.T) {
	a, p, _ := testApp(t)
	a.client = &subsonic.Client{}
	a.screen = screenLibrary
	a.songs = []subsonic.Song{{ID: "s1", Title: "测试歌曲"}}
	a.play.queue.Replace(a.songs, 0)
	a.playback = player.Event{Generation: a.generation, State: player.Playing, Duration: 100}
	a.position = 5
	tt := ui.NewTester(a.view, 1080, 720)
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	if !p.paused {
		t.Fatal("Space did not pause playback")
	}
	a.onPlayback(player.Event{Generation: a.generation, State: player.Paused, Duration: 100, Position: 5})
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	if p.paused {
		t.Fatal("Space did not resume playback")
	}
	a.playback = player.Event{Generation: a.generation, State: player.Ended}
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	if len(p.loads) != 1 {
		t.Fatalf("Space did not start the current track after the end: %d loads", len(p.loads))
	}
}

func TestSpaceDoesNotStealTextInput(t *testing.T) {
	a, p, _ := testApp(t)
	a.play.queue.Replace([]subsonic.Song{{ID: "s1", Title: "测试歌曲"}}, 0)
	a.playback = player.Event{Generation: a.generation, State: player.Playing, Duration: 100}
	tt := ui.NewTester(a.view, 1080, 720)
	for range 64 {
		if tt.Focused("服务器地址") {
			break
		}
		tt.Key(0, ui.KeyTab)
	}
	if !tt.Focused("服务器地址") {
		t.Fatal("keyboard cannot reach the server field")
	}
	tt.Key(0, ui.KeySpace)
	if p.paused {
		t.Fatal("Space paused while a text field had focus")
	}
	tt.Type(" ")
	if a.server != " " {
		t.Fatalf("space text did not reach the field: %q", a.server)
	}
}

func TestMediaSnapshot(t *testing.T) {
	a, _, _ := testApp(t)
	a.volume = 42
	a.position = 30
	a.playback = player.Event{State: player.Playing, Duration: 120}
	a.play.repeat = repeatAll
	a.play.queue.Replace([]subsonic.Song{{
		ID:       "song-1",
		Title:    "歌名",
		Artist:   "歌手",
		Album:    "专辑",
		CoverArt: "cover-1",
		Duration: 100,
	}}, 0)
	a.artFiles["cover-1"] = "file:///tmp/cover.jpg"

	snapshot := a.mediaSnapshot()
	if snapshot.TrackID != mediaTrackID("song-1") || !strings.HasPrefix(snapshot.TrackID, "/org/godrome/godrome/track/") {
		t.Fatalf("track id %q", snapshot.TrackID)
	}
	if snapshot.Title != "歌名" || snapshot.Artist != "歌手" || snapshot.Album != "专辑" {
		t.Fatalf("metadata %+v", snapshot)
	}
	if snapshot.ArtURL != "file:///tmp/cover.jpg" {
		t.Fatalf("art url %q", snapshot.ArtURL)
	}
	if snapshot.Status != mpris.StatusPlaying || snapshot.Loop != mpris.LoopPlaylist {
		t.Fatalf("state %+v", snapshot)
	}
	if snapshot.Length != 120*time.Second || snapshot.Position != 30*time.Second {
		t.Fatalf("length %v position %v", snapshot.Length, snapshot.Position)
	}
	if snapshot.Volume != 0.42 {
		t.Fatalf("volume %v", snapshot.Volume)
	}
	if !snapshot.CanPlay || !snapshot.CanPause || !snapshot.CanSeek || !snapshot.CanGoNext || !snapshot.CanGoPrevious {
		t.Fatalf("capabilities %+v", snapshot)
	}

	a.play.queue = newPlayback().queue
	snapshot = a.mediaSnapshot()
	if snapshot.Status != mpris.StatusPlaying || snapshot.CanPlay || snapshot.CanSeek || snapshot.CanGoNext || snapshot.CanGoPrevious {
		t.Fatalf("empty queue snapshot %+v", snapshot)
	}
	a.playback = player.Event{State: player.Idle}
	if got := a.mediaSnapshot().Status; got != mpris.StatusStopped {
		t.Fatalf("idle status %q", got)
	}
}
