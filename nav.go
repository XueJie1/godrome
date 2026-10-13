package main

import "fmt"

// place is one visit the back and forward buttons can return to.
// It names the screen and the arguments that screen needs. Playback,
// the queue, and column widths stay outside it.
// restore is where the user was on a page. It names songs and albums by id,
// never by a song slice, a bitmap, or a stream address.
type restore struct {
	selectedID string
	visibleID  string
	index      int
	albumID    string
}

type place struct {
	screen     screen
	pageOffset int
	songOffset int
	albumID    string
	albumName  string
	query      string
	restore    restore
	opened     bool
}

// same compares the page, not where the user had scrolled on it.
func (p place) same(o place) bool {
	p.restore, o.restore = restore{}, restore{}
	p.opened, o.opened = false, false
	return p == o
}

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

// mark stores where the user was on the page now shown. A later visit does
// not copy it.
func (h *history) mark(r restore) {
	if _, ok := h.current(); !ok {
		return
	}
	h.entries[h.at].restore = r
}

// replaceSearch keeps one search visit while the query changes. A search
// that is not the page now shown is a new visit, and it drops forward.
func (h *history) replaceSearch(query string) {
	cur, ok := h.current()
	if !ok || cur.screen != screenSearch {
		h.push(place{screen: screenSearch, query: query})
		return
	}
	h.entries[h.at].query = query
	h.entries[h.at].restore = restore{}
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
