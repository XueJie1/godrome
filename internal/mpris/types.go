// Package mpris serves the Linux desktop media interface on the session bus.
// The player state lives in the UI; the server only keeps the latest snapshot
// and forwards commands through callbacks.
package mpris

import "time"

const (
	BusName = "org.mpris.MediaPlayer2.godrome"
	Path    = "/org/mpris/MediaPlayer2"

	RootInterface       = "org.mpris.MediaPlayer2"
	PlayerInterface     = "org.mpris.MediaPlayer2.Player"
	PropertiesInterface = "org.freedesktop.DBus.Properties"
)

type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "Playing"
	StatusPaused  PlaybackStatus = "Paused"
	StatusStopped PlaybackStatus = "Stopped"
)

type LoopStatus string

const (
	LoopNone     LoopStatus = "None"
	LoopTrack    LoopStatus = "Track"
	LoopPlaylist LoopStatus = "Playlist"
)

// Snapshot is the player state a media widget may show. Durations are
// converted to microseconds at the D-Bus edge.
type Snapshot struct {
	TrackID  string
	Title    string
	Artist   string
	Album    string
	ArtURL   string
	Length   time.Duration
	Position time.Duration
	// PositionAdvancing lets the server extrapolate Position between the
	// app's roughly 2 Hz updates, including while the window is hidden.
	// It is not a D-Bus property.
	PositionAdvancing bool
	Status            PlaybackStatus
	Loop              LoopStatus
	Volume            float64 // 0 to 1
	CanPlay           bool
	CanPause          bool
	CanSeek           bool
	CanGoNext         bool
	CanGoPrevious     bool
}

// Controls are called from the D-Bus thread. The app must post them to the
// UI thread before touching player or view state.
type Controls struct {
	Play        func()
	Pause       func()
	PlayPause   func()
	Stop        func()
	Next        func()
	Previous    func()
	Seek        func(time.Duration) // relative
	SetPosition func(trackID string, position time.Duration)
	SetVolume   func(float64) // 0 to 1
	SetLoop     func(LoopStatus)
	Raise       func()
	Quit        func()
}
