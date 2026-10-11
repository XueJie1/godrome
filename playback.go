package main

import "godrome/internal/queue"

// playbackState owns the queue and repeat mode. Views ask it to move; they do not apply the rule.
type playbackState struct {
	queue  queue.Queue
	repeat repeatMode
}

func newPlayback() playbackState {
	return playbackState{queue: queue.New(), repeat: repeatOff}
}

func (p *playbackState) cycleRepeat() { p.repeat = p.repeat.next() }

func (p *playbackState) previous() bool {
	if p.queue.Previous() {
		return true
	}
	if p.repeat == repeatAll && len(p.queue.Songs) > 0 {
		return p.queue.Select(len(p.queue.Songs) - 1)
	}
	return false
}

func (p *playbackState) next() bool {
	if p.queue.Next() {
		return true
	}
	if p.repeat == repeatAll && len(p.queue.Songs) > 0 {
		return p.queue.Select(0)
	}
	return false
}

// advance reports whether EOF should load the current track.
func (p *playbackState) advance() bool {
	switch p.repeat {
	case repeatOne:
		_, ok := p.queue.Current()
		return ok
	case repeatAll:
		if !p.queue.Next() && len(p.queue.Songs) > 0 {
			p.queue.Select(0)
		}
		_, ok := p.queue.Current()
		return ok
	default:
		return p.queue.Next()
	}
}

func (p *playbackState) atStart() bool {
	return p.queue.Index <= 0 && p.repeat != repeatAll
}

func (p *playbackState) atEnd() bool {
	return p.queue.Index+1 >= len(p.queue.Songs) && p.repeat != repeatAll
}
