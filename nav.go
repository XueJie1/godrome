package main

import "fmt"

// place is one visit the back and forward buttons can return to.
// It names the screen and the arguments that screen needs. Playback,
// the queue, and column widths stay outside it.
type place struct {
	screen     screen
	pageOffset int
	songOffset int
	albumID    string
	albumName  string
	query      string
}

func (p place) same(o place) bool { return p == o }

func (p place) title() string {
	switch p.screen {
	case screenLibrary:
		return "曲库"
	case screenAlbums:
		return "专辑"
	case screenAlbum:
		if p.albumName != "" {
			return p.albumName
		}
		return "专辑"
	case screenSearch:
		if p.query != "" {
			return "搜索：" + p.query
		}
		return "搜索"
	case screenStarred:
		return "喜爱"
	case screenPlaylists:
		return "歌单"
	case screenLocal:
		return "本地音频"
	case screenSettings:
		return "设置"
	default:
		return "Godrome"
	}
}

func (p place) String() string {
	return fmt.Sprintf("%s %s", p.screen, p.title())
}

// history is a browser history of places. A new visit drops whatever
// forward would have reached. Back and forward do not record again.
type history struct {
	entries []place
	at      int
}

func (h *history) current() (place, bool) {
	if h == nil || len(h.entries) == 0 || h.at < 0 || h.at >= len(h.entries) {
		return place{}, false
	}
	return h.entries[h.at], true
}

func (h *history) canBack() bool { return h != nil && h.at > 0 }

func (h *history) canForward() bool {
	return h != nil && h.at >= 0 && h.at < len(h.entries)-1
}

// push records a visit after the one shown. The same place again is not
// a new entry. A different place drops the forward list.
func (h *history) push(p place) {
	if cur, ok := h.current(); ok && cur.same(p) {
		return
	}
	if h.at+1 < len(h.entries) {
		h.entries = h.entries[:h.at+1]
	}
	h.entries = append(h.entries, p)
	if len(h.entries) > 100 {
		h.entries = append([]place(nil), h.entries[len(h.entries)-100:]...)
	}
	h.at = len(h.entries) - 1
}

// note keeps the shown entry's display name current without a new visit.
func (h *history) note(p place) {
	cur, ok := h.current()
	if !ok || cur.screen != p.screen {
		return
	}
	if p.screen == screenAlbum && cur.albumID == p.albumID {
		h.entries[h.at].albumName = p.albumName
	}
}

func (h *history) back() (place, bool) {
	if !h.canBack() {
		return place{}, false
	}
	h.at--
	return h.entries[h.at], true
}

func (h *history) forward() (place, bool) {
	if !h.canForward() {
		return place{}, false
	}
	h.at++
	return h.entries[h.at], true
}
