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
