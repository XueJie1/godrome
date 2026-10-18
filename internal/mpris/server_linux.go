//go:build linux

package mpris

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const noTrack = "/org/mpris/MediaPlayer2/TrackList/NoTrack"

// Server owns one session-bus connection and the exported MPRIS objects.
// Update and Seeked may run on the UI thread; methods run on D-Bus threads.
type Server struct {
	conn     *dbus.Conn
	controls Controls
	close    sync.Once

	mu       sync.Mutex
	snapshot Snapshot
	updated  time.Time
}

// Start connects to the session bus and claims BusName. A missing session
// bus or a name already owned by another player is an error; MPRIS is
// optional, so the caller may ignore it.
func Start(controls Controls) (*Server, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, err
	}
	if err = conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, errors.New("another process owns " + BusName)
	}
	s := &Server{conn: conn, controls: controls, snapshot: normalize(Snapshot{}), updated: time.Now()}
	for _, export := range []struct {
		iface   string
		obj     any
		mapping map[string]string
	}{
		{RootInterface, rootObject{s}, nil},
		// SeekOffset keeps vet from reading the MPRIS method as io.Seeker.
		{PlayerInterface, playerObject{s}, map[string]string{"SeekOffset": "Seek"}},
		{PropertiesInterface, propertiesObject{s}, nil},
	} {
		if err := conn.ExportWithMap(export.obj, export.mapping, dbus.ObjectPath(Path), export.iface); err != nil {
			conn.ReleaseName(BusName)
			conn.Close()
			return nil, err
		}
	}
	return s, nil
}

// Update stores the latest state and emits PropertiesChanged for the
// properties that actually changed. Position is expected to move often and
// is not announced, as MPRIS specifies.
func (s *Server) Update(snapshot Snapshot) {
	snapshot = normalize(snapshot)
	s.mu.Lock()
	previous := s.snapshot
	s.snapshot = snapshot
	s.updated = time.Now()
	s.mu.Unlock()
	changed := changedProperties(playerProperties(previous), playerProperties(snapshot))
	if len(changed) > 0 {
		s.emitProperties(PlayerInterface, changed)
	}
}

// Seeked moves the cached position and tells clients the track jumped.
func (s *Server) Seeked(position time.Duration) {
	if position < 0 {
		position = 0
	}
	s.mu.Lock()
	s.snapshot.Position = position
	s.updated = time.Now()
	s.mu.Unlock()
	_ = s.conn.Emit(dbus.ObjectPath(Path), PlayerInterface+".Seeked", position.Microseconds())
}

// Close releases the bus name and the private connection.
func (s *Server) Close() {
	s.close.Do(func() {
		_, _ = s.conn.ReleaseName(BusName)
		_ = s.conn.Close()
	})
}

func (s *Server) current() Snapshot {
	s.mu.Lock()
	snapshot := s.snapshot
	elapsed := time.Since(s.updated)
	s.mu.Unlock()
	if snapshot.PositionAdvancing && snapshot.Status == StatusPlaying && elapsed > 0 {
		snapshot.Position += elapsed
		if snapshot.Length > 0 && snapshot.Position > snapshot.Length {
			snapshot.Position = snapshot.Length
		}
	}
	return snapshot
}

func (s *Server) emitProperties(iface string, changed map[string]dbus.Variant) {
	_ = s.conn.Emit(dbus.ObjectPath(Path), PropertiesInterface+".PropertiesChanged", iface, changed, []string{})
}

func normalize(snapshot Snapshot) Snapshot {
	if snapshot.Status != StatusPlaying && snapshot.Status != StatusPaused {
		snapshot.Status = StatusStopped
	}
	if snapshot.Loop != LoopTrack && snapshot.Loop != LoopPlaylist {
		snapshot.Loop = LoopNone
	}
	snapshot.Volume = clamp01(snapshot.Volume)
	snapshot.Length = max(0, snapshot.Length)
	snapshot.Position = max(0, snapshot.Position)
	return snapshot
}

func clamp01(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return max(0, min(1, value))
}

func playerProperties(snapshot Snapshot) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(string(snapshot.Status)),
		"LoopStatus":     dbus.MakeVariant(string(snapshot.Loop)),
		"Rate":           dbus.MakeVariant(1.0),
		"Shuffle":        dbus.MakeVariant(false),
		"Metadata":       dbus.MakeVariant(metadata(snapshot)),
		"Volume":         dbus.MakeVariant(snapshot.Volume),
		"MinimumRate":    dbus.MakeVariant(1.0),
		"MaximumRate":    dbus.MakeVariant(1.0),
		"CanGoNext":      dbus.MakeVariant(snapshot.CanGoNext),
		"CanGoPrevious":  dbus.MakeVariant(snapshot.CanGoPrevious),
		"CanPlay":        dbus.MakeVariant(snapshot.CanPlay),
		"CanPause":       dbus.MakeVariant(snapshot.CanPause),
		"CanSeek":        dbus.MakeVariant(snapshot.CanSeek),
		"CanControl":     dbus.MakeVariant(true),
	}
}

func rootProperties() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"CanQuit":             dbus.MakeVariant(true),
		"CanRaise":            dbus.MakeVariant(true),
		"HasTrackList":        dbus.MakeVariant(false),
		"Identity":            dbus.MakeVariant("Godrome"),
		"DesktopEntry":        dbus.MakeVariant("godrome"),
		"SupportedUriSchemes": dbus.MakeVariant([]string{}),
		"SupportedMimeTypes":  dbus.MakeVariant([]string{}),
	}
}

func metadata(snapshot Snapshot) map[string]dbus.Variant {
	track := snapshot.TrackID
	if track == "" {
		track = noTrack
	}
	md := map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath(track))}
	if snapshot.Length > 0 {
		md["mpris:length"] = dbus.MakeVariant(snapshot.Length.Microseconds())
	}
	if snapshot.ArtURL != "" {
		md["mpris:artUrl"] = dbus.MakeVariant(snapshot.ArtURL)
	}
	if snapshot.Title != "" {
		md["xesam:title"] = dbus.MakeVariant(snapshot.Title)
	}
	if snapshot.Artist != "" {
		md["xesam:artist"] = dbus.MakeVariant([]string{snapshot.Artist})
	}
	if snapshot.Album != "" {
		md["xesam:album"] = dbus.MakeVariant(snapshot.Album)
	}
	return md
}

// changedProperties compares the emitted properties, not Position.
func changedProperties(previous, next map[string]dbus.Variant) map[string]dbus.Variant {
	changed := make(map[string]dbus.Variant)
	for name, value := range next {
		if old, ok := previous[name]; !ok || !reflect.DeepEqual(old, value) {
			changed[name] = value
		}
	}
	return changed
}

type rootObject struct{ server *Server }

func (r rootObject) Raise() *dbus.Error {
	if r.server.controls.Raise != nil {
		r.server.controls.Raise()
	}
	return nil
}

func (r rootObject) Quit() *dbus.Error {
	// Return the method reply before the app can close the connection.
	if quit := r.server.controls.Quit; quit != nil {
		go quit()
	}
	return nil
}

type playerObject struct{ server *Server }

func (p playerObject) Play() *dbus.Error {
	if p.server.controls.Play != nil {
		p.server.controls.Play()
	}
	return nil
}

func (p playerObject) Pause() *dbus.Error {
	if p.server.controls.Pause != nil {
		p.server.controls.Pause()
	}
	return nil
}

func (p playerObject) PlayPause() *dbus.Error {
	if p.server.controls.PlayPause != nil {
		p.server.controls.PlayPause()
	}
	return nil
}

func (p playerObject) Stop() *dbus.Error {
	if p.server.controls.Stop != nil {
		p.server.controls.Stop()
	}
	return nil
}

func (p playerObject) Next() *dbus.Error {
	if p.server.controls.Next != nil {
		p.server.controls.Next()
	}
	return nil
}

func (p playerObject) Previous() *dbus.Error {
	if p.server.controls.Previous != nil {
		p.server.controls.Previous()
	}
	return nil
}

func (p playerObject) SeekOffset(offset int64) *dbus.Error {
	snapshot := p.server.current()
	if snapshot.TrackID == "" {
		return nil
	}
	// Relative Seek starts from the extrapolated position, which stays
	// current while the app window is hidden. SetPosition carries the
	// absolute target back to the player.
	if p.server.controls.SetPosition != nil {
		target := snapshot.Position + time.Duration(offset)*time.Microsecond
		target = max(0, target)
		if snapshot.Length > 0 {
			target = min(target, snapshot.Length)
		}
		p.server.controls.SetPosition(snapshot.TrackID, target)
	} else if p.server.controls.Seek != nil {
		p.server.controls.Seek(time.Duration(offset) * time.Microsecond)
	}
	return nil
}

func (p playerObject) SetPosition(trackID dbus.ObjectPath, position int64) *dbus.Error {
	current := p.server.current().TrackID
	if current == "" {
		current = noTrack
	}
	if string(trackID) != current {
		return &dbus.Error{Name: "org.mpris.MediaPlayer2.Error.InvalidArgs", Body: []any{"track id does not match the current track"}}
	}
	if p.server.controls.SetPosition != nil {
		p.server.controls.SetPosition(string(trackID), time.Duration(max(0, position))*time.Microsecond)
	}
	return nil
}

func (p playerObject) OpenUri(string) *dbus.Error {
	return &dbus.Error{Name: "org.mpris.MediaPlayer2.Error.NotSupported", Body: []any{"OpenUri is not supported"}}
}

type propertiesObject struct{ server *Server }

func (p propertiesObject) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	all, err := p.all(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	value, ok := all[name]
	if !ok {
		return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty", Body: []any{name}}
	}
	return value, nil
}

func (p propertiesObject) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return p.all(iface)
}

func (p propertiesObject) Set(iface, name string, value dbus.Variant) *dbus.Error {
	if iface != PlayerInterface {
		return readOnly(name)
	}
	switch name {
	case "Volume":
		volume, ok := value.Value().(float64)
		if !ok {
			return invalidArgs("Volume must be a double")
		}
		if p.server.controls.SetVolume != nil {
			p.server.controls.SetVolume(clamp01(volume))
		}
		return nil
	case "LoopStatus":
		loop, ok := value.Value().(string)
		if !ok || (LoopStatus(loop) != LoopNone && LoopStatus(loop) != LoopTrack && LoopStatus(loop) != LoopPlaylist) {
			return invalidArgs("LoopStatus must be None, Track, or Playlist")
		}
		if p.server.controls.SetLoop != nil {
			p.server.controls.SetLoop(LoopStatus(loop))
		}
		return nil
	default:
		return readOnly(name)
	}
}

func (p propertiesObject) all(iface string) (map[string]dbus.Variant, *dbus.Error) {
	snapshot := p.server.current()
	player := playerProperties(snapshot)
	player["Position"] = dbus.MakeVariant(snapshot.Position.Microseconds())
	switch iface {
	case PlayerInterface:
		return player, nil
	case RootInterface:
		return rootProperties(), nil
	case "":
		all := rootProperties()
		for name, value := range player {
			all[name] = value
		}
		return all, nil
	default:
		return nil, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownInterface", Body: []any{iface}}
	}
}

func readOnly(name string) *dbus.Error {
	return &dbus.Error{Name: "org.freedesktop.DBus.Error.PropertyReadOnly", Body: []any{name}}
}

func invalidArgs(message string) *dbus.Error {
	return &dbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Body: []any{message}}
}
