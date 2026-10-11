package queue

import (
	"godrome/internal/subsonic"
	"testing"
)

func TestQueueBoundariesAndOwnership(t *testing.T) {
	q := New()
	if q.Next() || q.Previous() {
		t.Fatal("empty queue moved")
	}
	songs := []subsonic.Song{{ID: "a"}, {ID: "b"}}
	if !q.Replace(songs, 0) {
		t.Fatal("replace")
	}
	songs[0].ID = "changed"
	song, _ := q.Current()
	if song.ID != "a" {
		t.Fatal("queue aliases page memory")
	}
	if !q.Next() || q.Next() || q.Index != 1 {
		t.Fatal("end boundary or auto-next")
	}
	if !q.Previous() || q.Previous() || q.Index != 0 {
		t.Fatal("start boundary")
	}
	q.Add(subsonic.Song{ID: "c"})
	if !q.Select(2) || q.Select(3) {
		t.Fatal("selection")
	}
}
