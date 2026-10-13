package main

import "testing"

func TestHistoryDropsForwardAfterANewVisit(t *testing.T) {
	var h history
	h.push(place{screen: screenLibrary})
	h.push(place{screen: screenAlbums, pageOffset: 0})
	h.push(place{screen: screenAlbum, albumID: "a"})
	if !h.canBack() || h.canForward() {
		t.Fatalf("after three visits: back=%v forward=%v", h.canBack(), h.canForward())
	}
	got, ok := h.back()
	if !ok || got.screen != screenAlbums || !h.canForward() {
		t.Fatalf("back: ok=%v place=%+v forward=%v", ok, got, h.canForward())
	}
	h.push(place{screen: screenStarred})
	if h.canForward() {
		t.Fatal("a new visit kept the forward page")
	}
	got, ok = h.back()
	if !ok || got.screen != screenAlbums {
		t.Fatalf("back after the new visit: %+v", got)
	}
	got, ok = h.forward()
	if !ok || got.screen != screenStarred {
		t.Fatalf("forward: %+v", got)
	}
}

func TestHistoryIgnoresTheSamePlace(t *testing.T) {
	var h history
	p := place{screen: screenAlbums, pageOffset: 100}
	h.push(p)
	h.push(p)
	if len(h.entries) != 1 || h.canBack() {
		t.Fatalf("same place recorded twice: %+v", h.entries)
	}
}

func TestSearchTypingReplacesTheCurrentSearchVisit(t *testing.T) {
	var h history
	h.push(place{screen: screenAlbums})
	h.push(place{screen: screenSearch, query: "a"})
	h.replaceSearch("ab")
	h.replaceSearch("abc")
	if len(h.entries) != 2 {
		t.Fatalf("each key grew history: %+v", h.entries)
	}
	got, ok := h.current()
	if !ok || got.query != "abc" || h.canForward() {
		t.Fatalf("search visit: %+v forward=%v", got, h.canForward())
	}
	if back, ok := h.back(); !ok || back.screen != screenAlbums {
		t.Fatalf("one back did not leave search: %+v", back)
	}
}

func TestSearchAfterLeavingStartsANewVisit(t *testing.T) {
	var h history
	h.push(place{screen: screenSearch, query: "old"})
	h.push(place{screen: screenAlbums})
	if _, ok := h.back(); !ok {
		t.Fatal("back failed")
	}
	h.push(place{screen: screenSearch, query: "new"})
	if h.canForward() || len(h.entries) != 2 {
		t.Fatalf("new search kept the forward branch: %+v at=%d", h.entries, h.at)
	}
}

func TestHistoryKeepsARestoreMark(t *testing.T) {
	var h history
	h.push(place{screen: screenLibrary, songOffset: 100})
	h.mark(restore{selectedID: "song-40", visibleID: "song-30", index: 30})
	h.push(place{screen: screenAlbum, albumID: "al"})
	got, ok := h.back()
	if !ok || got.restore.selectedID != "song-40" || got.restore.visibleID != "song-30" {
		t.Fatalf("restore mark was dropped: %+v", got)
	}
}
