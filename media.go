package main

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"godrome/internal/mpris"
	"godrome/internal/player"
)

// startMPRIS claims the Linux session-bus name. Other systems return no
// server, and every update below then does nothing.
func (a *app) startMPRIS(raise, quit func()) {
	if a.media != nil {
		return
	}
	server, err := mpris.Start(mpris.Controls{
		Play:        func() { a.dispatch(a.mediaPlay) },
		Pause:       func() { a.dispatch(a.mediaPause) },
		PlayPause:   func() { a.dispatch(a.mediaPlayPause) },
		Stop:        func() { a.dispatch(a.mediaStop) },
		Next:        func() { a.dispatch(a.mediaNext) },
		Previous:    func() { a.dispatch(a.mediaPrevious) },
		Seek:        func(offset time.Duration) { a.dispatch(func() { a.mediaSeek(offset) }) },
		SetPosition: func(track string, position time.Duration) { a.dispatch(func() { a.mediaSetPosition(track, position) }) },
		SetVolume:   func(volume float64) { a.dispatch(func() { a.setVolume(volume * 100) }) },
		SetLoop:     func(loop mpris.LoopStatus) { a.dispatch(func() { a.mediaSetLoop(loop) }) },
		Raise:       raise,
		Quit:        quit,
	})
	if err != nil {
		return // MPRIS is optional: no session bus means no media widget.
	}
	a.media = server
	a.updateMedia()
}

// updateMedia publishes the current track and transport state. It runs on
// the UI thread, and starts one background cover fetch when needed.
func (a *app) updateMedia() {
	if a.media == nil {
		return
	}
	a.media.Update(a.mediaSnapshot())
	if song, ok := a.play.queue.Current(); ok && song.CoverArt != "" && a.client != nil {
		a.fetchArtwork(song.CoverArt)
	}
}

func (a *app) mediaSnapshot() mpris.Snapshot {
	snapshot := mpris.Snapshot{
		Status:            mpris.StatusStopped,
		Loop:              loopStatus(a.play.repeat),
		Volume:            clamp01(a.volume / 100),
		PositionAdvancing: a.playback.State == player.Playing,
	}
	switch a.playback.State {
	case player.Playing, player.Buffering, player.Loading:
		snapshot.Status = mpris.StatusPlaying
	case player.Paused:
		snapshot.Status = mpris.StatusPaused
	}
	song, has := a.play.queue.Current()
	if !has {
		return snapshot
	}
	snapshot.TrackID = mediaTrackID(song.ID)
	snapshot.Title = song.Title
	snapshot.Artist = song.Artist
	snapshot.Album = song.Album
	snapshot.ArtURL = a.artFiles[song.CoverArt]
	length := song.Duration
	if a.playback.Duration > 0 {
		length = a.playback.Duration
	}
	position := max(0, a.position)
	if length > 0 {
		position = min(position, length)
		snapshot.Length = secondsDuration(length)
	}
	snapshot.Position = secondsDuration(position)
	snapshot.CanPlay = true
	snapshot.CanPause = snapshot.Status != mpris.StatusStopped
	snapshot.CanSeek = length > 0
	snapshot.CanGoNext = !a.play.atEnd()
	snapshot.CanGoPrevious = !a.play.atStart()
	return snapshot
}

func secondsDuration(seconds float64) time.Duration {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func clamp01(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return max(0, min(1, value))
}

func loopStatus(mode repeatMode) mpris.LoopStatus {
	switch mode {
	case repeatAll:
		return mpris.LoopPlaylist
	case repeatOne:
		return mpris.LoopTrack
	default:
		return mpris.LoopNone
	}
}

func (a *app) mediaSetLoop(loop mpris.LoopStatus) {
	switch loop {
	case mpris.LoopPlaylist:
		a.play.repeat = repeatAll
	case mpris.LoopTrack:
		a.play.repeat = repeatOne
	default:
		a.play.repeat = repeatOff
	}
	a.updateMedia()
}

// mediaTrackID is a stable object-path-safe id. Local paths and server ids
// share the same rule and never leak into the bus name.
func mediaTrackID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "/org/godrome/godrome/track/" + hex.EncodeToString(sum[:8])
}

func (a *app) mediaPlay() {
	switch a.playback.State {
	case player.Playing, player.Buffering, player.Loading:
		return
	case player.Paused:
		a.control(a.engine.Pause(false))
		return
	}
	if _, ok := a.play.queue.Current(); ok {
		a.playCurrent()
	}
}

func (a *app) mediaPause() {
	switch a.playback.State {
	case player.Idle, player.Ended, player.Failed, player.Paused:
		return
	}
	if _, ok := a.play.queue.Current(); ok {
		a.control(a.engine.Pause(true))
	}
}

func (a *app) mediaPlayPause() {
	if a.playback.State == player.Paused {
		a.mediaPlay()
		return
	}
	if a.playback.State == player.Playing || a.playback.State == player.Buffering || a.playback.State == player.Loading {
		a.mediaPause()
		return
	}
	a.mediaPlay()
}

func (a *app) mediaStop() {
	a.stopPlayback()
}

func (a *app) mediaNext() {
	if a.play.next() {
		a.playCurrent()
	}
}

func (a *app) mediaPrevious() {
	if a.play.previous() {
		a.playCurrent()
	}
}

func (a *app) mediaSeek(offset time.Duration) {
	if _, ok := a.play.queue.Current(); !ok {
		return
	}
	a.seekTo(a.position + offset.Seconds())
}

func (a *app) mediaSetPosition(track string, position time.Duration) {
	song, ok := a.play.queue.Current()
	if !ok || track != mediaTrackID(song.ID) {
		return
	}
	a.seekTo(position.Seconds())
}

// setVolume is the one path for the slider, the speaker, and MPRIS.
// A non-zero value also becomes the level mute restores.
func (a *app) setVolume(value float64) {
	value = max(0, min(100, value))
	a.volume = value
	if value > 0 {
		a.volumeBefore = value
	}
	a.control(a.engine.SetVolume(value))
	a.updateMedia()
}

func (a *app) seekTo(seconds float64) {
	seconds = max(0, seconds)
	if a.playback.Duration > 0 {
		seconds = min(seconds, a.playback.Duration)
	}
	if err := a.engine.Seek(seconds); err != nil {
		a.control(err)
		return
	}
	a.position = seconds
	if a.media != nil {
		a.media.Seeked(secondsDuration(seconds))
	}
	a.updateMedia()
}

func (a *app) stopPlayback() {
	a.generation++
	a.control(a.engine.Stop())
	a.playback = player.Event{Generation: a.generation, State: player.Idle}
	a.position = 0
	a.updateMedia()
}

// fetchArtwork saves the current cover for media widgets, once per cover id
// per session. It never blocks the view; the result comes back on the UI
// thread. Errors only mean the widget shows no image.
func (a *app) fetchArtwork(id string) {
	if id == "" || a.artFiles[id] != "" || a.artTried[id] {
		return
	}
	client := a.client
	if client == nil {
		return
	}
	a.artTried[id] = true
	go func() {
		data, err := client.Cover(a.ctx, id)
		if err != nil || len(data) == 0 {
			return
		}
		path, err := writeArtwork(id, data)
		if err != nil {
			return
		}
		artURL := (&url.URL{Scheme: "file", Path: path}).String()
		a.dispatch(func() {
			a.artFiles[id] = artURL
			a.updateMedia()
		})
	}()
}

func writeArtwork(id string, data []byte) (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "godrome", "covers")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(id))
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+imageExtension(data))
	file, err := os.CreateTemp(dir, "cover-*.tmp")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	if _, err = file.Write(data); err != nil {
		file.Close()
		os.Remove(temp)
		return "", err
	}
	if err = file.Close(); err != nil {
		os.Remove(temp)
		return "", err
	}
	if err = os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return "", err
	}
	return path, nil
}

func imageExtension(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".img"
	}
}
