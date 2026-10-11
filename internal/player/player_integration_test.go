//go:build cgo

package player

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitFor(t *testing.T, p *Engine, generation uint64, match func(Event) bool) Event {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-p.Events():
			if e.Generation != generation {
				continue
			}
			if e.Err != nil {
				t.Fatal(e.Err)
			}
			if match(e) {
				return e
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for libmpv event")
			return Event{}
		}
	}
}
func TestFormatsPauseSeekEOFAndStream(t *testing.T) {
	dir := os.Getenv("GODROME_AUDIO_SAMPLES")
	if dir == "" {
		t.Skip("run make test-audio to generate and play samples (null output)")
	}
	p := New(Options{AudioOutput: "null"})
	defer p.Close()
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	for i, format := range []string{"mp3", "flac", "m4a", "opus"} {
		t.Run(format, func(t *testing.T) {
			generation := uint64(i + 1)
			path := filepath.Join(dir, "sample."+format)
			if i%2 == 1 {
				path = server.URL + "/sample." + format
			} // libmpv directly opens an HTTP stream.
			if err := p.Load(path, generation); err != nil {
				t.Fatal(err)
			}
			ready := waitFor(t, p, generation, func(e Event) bool { return e.State == Playing && e.Duration > 0 })
			if ready.Duration < 3 {
				t.Fatal("wrong sample duration")
			}
			if err := p.Pause(true); err != nil {
				t.Fatal(err)
			}
			waitFor(t, p, generation, func(e Event) bool { return e.State == Paused })
			if err := p.Seek(2); err != nil {
				t.Fatal(err)
			}
			if err := p.SetVolume(25); err != nil {
				t.Fatal(err)
			}
			if err := p.Pause(false); err != nil {
				t.Fatal(err)
			}
			waitFor(t, p, generation, func(e Event) bool { return e.State == Playing && e.Position >= 1.8 })
			if err := p.Seek(ready.Duration - .3); err != nil {
				t.Fatal(err)
			}
			waitFor(t, p, generation, func(e Event) bool { return e.State == Ended })
		})
	}
	// Repeated replacement must not let an old EOF end the newest file.
	for i := uint64(10); i < 30; i++ {
		if err := p.Load(filepath.Join(dir, "sample.flac"), i); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, p, 29, func(e Event) bool { return e.State == Playing && e.Duration > 0 })
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, p, 29, func(e Event) bool { return e.State == Idle })
	if err := p.Load(filepath.Join(dir, "does-not-exist.flac"), 30); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-p.Events():
		for e.State != Failed || e.Generation != 30 {
			select {
			case e = <-p.Events():
			case <-time.After(8 * time.Second):
				t.Fatal("missing playback error")
			}
		}
		if e.Err == nil {
			t.Fatal("failed state needs an error")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("missing failed event")
	}
	p.Close()
	p.Close() // Shutdown joins the worker and can be called repeatedly.
}
