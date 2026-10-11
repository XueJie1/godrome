package player

import "testing"

func TestDisplayStateKeepsBufferingOverPause(t *testing.T) {
	if displayState(true, true, false) != Buffering {
		t.Fatal("cache wait hidden by pause")
	}
	if displayState(false, false, true) != Buffering {
		t.Fatal("seek wait not shown")
	}
	if displayState(true, false, false) != Paused {
		t.Fatal("user pause lost")
	}
}
