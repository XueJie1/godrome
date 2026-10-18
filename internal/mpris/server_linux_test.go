//go:build linux

package mpris

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestMetadata(t *testing.T) {
	snapshot := normalize(Snapshot{
		TrackID: "/org/godrome/godrome/track/abc",
		Title:   "歌名",
		Artist:  "歌手",
		Album:   "专辑",
		ArtURL:  "file:///tmp/cover.jpg",
		Length:  3 * time.Minute,
		Status:  StatusPlaying,
		Volume:  2,
	})
	md := metadata(snapshot)
	if got := md["mpris:trackid"].Value(); got != dbus.ObjectPath("/org/godrome/godrome/track/abc") {
		t.Fatalf("track id %v", got)
	}
	if got := md["mpris:length"].Value(); got != int64(180_000_000) {
		t.Fatalf("length %v", got)
	}
	if got := md["xesam:artist"].Value(); len(got.([]string)) != 1 || got.([]string)[0] != "歌手" {
		t.Fatalf("artist %v", got)
	}
	if got := md["mpris:artUrl"].Value(); got != "file:///tmp/cover.jpg" {
		t.Fatalf("art url %v", got)
	}
	if snapshot.Volume != 1 {
		t.Fatalf("volume was not clamped: %v", snapshot.Volume)
	}
	empty := metadata(Snapshot{})
	if got := empty["mpris:trackid"].Value(); got != dbus.ObjectPath(noTrack) {
		t.Fatalf("empty track id %v", got)
	}
}

func TestChangedProperties(t *testing.T) {
	before := normalize(Snapshot{Status: StatusStopped, Volume: 0.5})
	after := normalize(Snapshot{Status: StatusPlaying, Volume: 0.5, Position: time.Minute})
	changed := changedProperties(playerProperties(before), playerProperties(after))
	if _, ok := changed["PlaybackStatus"]; !ok {
		t.Fatal("PlaybackStatus change was missed")
	}
	if _, ok := changed["Volume"]; ok {
		t.Fatal("unchanged volume was announced")
	}
	if _, ok := changed["Position"]; ok {
		t.Fatal("Position must not be announced")
	}
	if len(changedProperties(playerProperties(after), playerProperties(after))) != 0 {
		t.Fatal("identical snapshots produced a change")
	}
}

func TestPositionExtrapolation(t *testing.T) {
	server := &Server{
		snapshot: normalize(Snapshot{Status: StatusPlaying, PositionAdvancing: true, Position: 30 * time.Second, Length: time.Minute}),
		updated:  time.Now().Add(-2 * time.Second),
	}
	position := server.current().Position
	if position < 31*time.Second || position > 33*time.Second {
		t.Fatalf("playing position %v", position)
	}
	server.snapshot.Status = StatusPaused
	server.snapshot.PositionAdvancing = false
	if got := server.current().Position; got != 30*time.Second {
		t.Fatalf("paused position moved to %v", got)
	}
}

// TestServerOnSessionBus talks to a real bus. Run it under a private bus:
//
//	GODROME_MPRIS_TEST=1 dbus-run-session -- go test ./internal/mpris
func TestServerOnSessionBus(t *testing.T) {
	if os.Getenv("GODROME_MPRIS_TEST") == "" {
		t.Skip("set GODROME_MPRIS_TEST=1 under dbus-run-session")
	}
	played := make(chan string, 16)
	controls := Controls{
		Play:        func() { played <- "play" },
		Pause:       func() { played <- "pause" },
		PlayPause:   func() { played <- "playpause" },
		Stop:        func() { played <- "stop" },
		Next:        func() { played <- "next" },
		Previous:    func() { played <- "previous" },
		Seek:        func(offset time.Duration) { played <- "seek:" + offset.String() },
		SetPosition: func(track string, pos time.Duration) { played <- "position:" + track + ":" + pos.String() },
		SetVolume:   func(volume float64) { played <- "volume:" + dbusDouble(volume) },
		SetLoop:     func(loop LoopStatus) { played <- "loop:" + string(loop) },
	}
	server, err := Start(controls)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	server.Update(Snapshot{
		TrackID:       "/org/godrome/godrome/track/abc",
		Title:         "歌名",
		Artist:        "歌手",
		Album:         "专辑",
		Length:        3 * time.Minute,
		Position:      42 * time.Second,
		Status:        StatusPlaying,
		Loop:          LoopNone,
		Volume:        0.5,
		CanPlay:       true,
		CanPause:      true,
		CanSeek:       true,
		CanGoNext:     true,
		CanGoPrevious: true,
	})

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	object := conn.Object(BusName, dbus.ObjectPath(Path))
	var status dbus.Variant
	if err := object.Call(PropertiesInterface+".Get", 0, PlayerInterface, "PlaybackStatus").Store(&status); err != nil {
		t.Fatal(err)
	}
	if status.Value() != "Playing" {
		t.Fatalf("status %v", status.Value())
	}
	var all map[string]dbus.Variant
	if err := object.Call(PropertiesInterface+".GetAll", 0, PlayerInterface).Store(&all); err != nil {
		t.Fatal(err)
	}
	if _, ok := all["Position"]; !ok {
		t.Fatal("Position missing from GetAll")
	}
	md := all["Metadata"].Value().(map[string]dbus.Variant)
	if md["xesam:title"].Value() != "歌名" {
		t.Fatalf("title %v", md["xesam:title"].Value())
	}

	if err := object.Call(PlayerInterface+".PlayPause", 0).Err; err != nil {
		t.Fatal(err)
	}
	if got := waitCall(t, played); got != "playpause" {
		t.Fatalf("PlayPause called %q", got)
	}
	if err := object.Call(PlayerInterface+".Seek", 0, int64(5_000_000)).Err; err != nil {
		t.Fatal(err)
	}
	if got := waitCall(t, played); got != "position:/org/godrome/godrome/track/abc:47s" {
		t.Fatalf("Seek called %q", got)
	}
	if err := object.Call(PlayerInterface+".SetPosition", 0, dbus.ObjectPath("/org/godrome/godrome/track/abc"), int64(10_000_000)).Err; err != nil {
		t.Fatal(err)
	}
	if got := waitCall(t, played); got != "position:/org/godrome/godrome/track/abc:10s" {
		t.Fatalf("SetPosition called %q", got)
	}
	if err := object.Call(PropertiesInterface+".Set", 0, PlayerInterface, "Volume", dbus.MakeVariant(0.25)).Err; err != nil {
		t.Fatal(err)
	}
	if got := waitCall(t, played); got != "volume:0.250000" {
		t.Fatalf("Set Volume called %q", got)
	}
	if err := object.Call(PropertiesInterface+".Set", 0, PlayerInterface, "LoopStatus", dbus.MakeVariant("Track")).Err; err != nil {
		t.Fatal(err)
	}
	if got := waitCall(t, played); got != "loop:Track" {
		t.Fatalf("Set LoopStatus called %q", got)
	}

	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='"+PropertiesInterface+"',member='PropertiesChanged'").Err; err != nil {
		t.Fatal(err)
	}
	server.Update(Snapshot{Status: StatusPaused, Volume: 0.25})
	changed := waitSignal(t, signals)
	if changed.Name != PropertiesInterface+".PropertiesChanged" {
		t.Fatalf("signal %q", changed.Name)
	}
	values := changed.Body[1].(map[string]dbus.Variant)
	if values["PlaybackStatus"].Value() != "Paused" {
		t.Fatalf("changed %v", values)
	}
}

func dbusDouble(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func waitCall(t *testing.T, calls <-chan string) string {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("D-Bus method did not reach the controls")
		return ""
	}
}

func waitSignal(t *testing.T, signals <-chan *dbus.Signal) *dbus.Signal {
	t.Helper()
	select {
	case signal := <-signals:
		return signal
	case <-time.After(3 * time.Second):
		t.Fatal("PropertiesChanged signal did not arrive")
		return nil
	}
}
