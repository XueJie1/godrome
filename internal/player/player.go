package player

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

type State int

const (
	Idle State = iota
	Loading
	Playing
	Paused
	Buffering
	Ended
	Failed
)

type Event struct {
	Generation         uint64
	State              State
	Position, Duration float64
	Err                error
}
type command struct {
	kind       string
	path       string
	value      float64
	flag       bool
	generation uint64
}

// Engine owns libmpv on one worker. UI methods only enqueue work.
type Engine struct {
	commands    chan command
	events      chan Event
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	once        sync.Once
	audioOutput string
}
type Options struct{ AudioOutput string } // Empty uses the system default; "null" is only for automated tests.
func New(options Options) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Engine{commands: make(chan command, 32), events: make(chan Event, 32), ctx: ctx, cancel: cancel, done: make(chan struct{}), audioOutput: options.AudioOutput}
	go p.run()
	return p
}
func (p *Engine) Events() <-chan Event { return p.events }
func (p *Engine) send(c command) error {
	if p.ctx.Err() != nil {
		return errors.New("播放器已关闭")
	}
	select {
	case p.commands <- c:
		return nil
	default:
		return errors.New("播放器忙，请稍后重试")
	}
}
func (p *Engine) Load(path string, generation uint64) error {
	return p.send(command{kind: "load", path: path, generation: generation})
}
func (p *Engine) Pause(value bool) error { return p.send(command{kind: "pause", flag: value}) }
func (p *Engine) Seek(seconds float64) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return errors.New("无效播放位置")
	}
	return p.send(command{kind: "seek", value: max(0, seconds)})
}
func (p *Engine) SetVolume(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("无效音量")
	}
	return p.send(command{kind: "volume", value: max(0, min(100, value))})
}
func (p *Engine) SetVisible(value bool) error { return p.send(command{kind: "visible", flag: value}) }
func (p *Engine) Stop() error                 { return p.send(command{kind: "stop"}) }
func (p *Engine) Close()                      { p.once.Do(p.cancel); <-p.done }

func (p *Engine) run() {
	defer close(p.done)
	defer close(p.events)
	var n *native
	defer func() {
		if n != nil {
			n.close()
		}
	}()
	e := Event{State: Idle}
	volume := 70.0
	visible := true
	loaded, paused, buffering, seeking := false, false, false, false
	entry := int64(-1)
	lastProgress := time.Time{}
	emit := func() {
		select {
		case p.events <- e:
		case <-p.ctx.Done():
		}
	}
	fail := func(err error) {
		if err != nil {
			e.State = Failed
			e.Err = err
			loaded = false
			seeking = false
			buffering = false
			emit()
		}
	}
	updateState := func() {
		if !loaded {
			return
		}
		// Cache waits also set pause. Buffering has to win, or the spinner never appears.
		state := displayState(paused, buffering, seeking)
		if state != e.State {
			e.State = state
			emit()
		}
	}
	for {
		if p.ctx.Err() != nil {
			return
		}
		select {
		case <-p.ctx.Done():
			return
		case c := <-p.commands:
			switch c.kind {
			case "load":
				// Drain the previous file before assigning the new generation. Ignore its pending EOF.
				if n != nil {
					for n.wait(0).kind != eventNone {
					}
				}
				e = Event{Generation: c.generation, State: Loading}
				loaded = false
				paused = false
				buffering = false
				seeking = false
				entry = -1
				emit()
				if n == nil {
					var err error
					n, err = openNative(p.audioOutput)
					if err != nil {
						fail(err)
						continue
					}
					if err = n.volume(volume); err != nil {
						fail(err)
						continue
					}
				}
				fail(n.load(c.path))
			case "volume":
				volume = c.value
				if n != nil {
					fail(n.volume(volume))
				}
			case "visible":
				visible = c.flag
			case "pause":
				if n != nil {
					fail(n.pause(c.flag))
				}
			case "seek":
				// Show the wait immediately. A seek into cached audio clears it
				// when mpv reports seeking=false; an uncached seek keeps it through paused-for-cache.
				seeking = true
				e.Position = c.value
				if loaded {
					e.State = Buffering
					emit()
				}
				if n != nil {
					fail(n.seek(c.value))
				}
			case "stop":
				if n != nil {
					fail(n.stop())
				}
				entry = -1
				loaded = false
				seeking = false
				buffering = false
				e.State = Idle
				e.Position = 0
				e.Err = nil
				emit()
			}
		default:
			if n == nil {
				select {
				case <-p.ctx.Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
				continue
			}
			ev := n.wait(0.02)
			switch ev.kind {
			case eventStart:
				entry = ev.entry
			case eventLoaded:
				loaded = true
				e.Err = nil
				updateState()
			case eventEnd:
				if ev.entry != entry || entry < 0 {
					continue
				}
				loaded = false
				entry = -1
				if ev.reason == endEOF {
					e.State = Ended
					emit()
				}
				if ev.reason == endError {
					fail(fmt.Errorf("音频播放失败（libmpv 代码 %d），可重试或下一曲", ev.code))
				}
			case eventProperty:
				switch ev.name {
				case "time-pos":
					e.Position = ev.number
				case "duration":
					e.Duration = ev.number
				case "pause":
					paused = ev.flag
					updateState()
				case "paused-for-cache":
					buffering = ev.flag
					updateState()
				case "seeking":
					seeking = ev.flag
					updateState()
				}
			}
			if loaded && !paused && visible && time.Since(lastProgress) >= 500*time.Millisecond {
				lastProgress = time.Now()
				emit()
			}
		}
	}
}
