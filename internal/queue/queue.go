package queue

import "godrome/internal/subsonic"

type Queue struct {
	Songs []subsonic.Song
	Index int
}

func New() Queue { return Queue{Index: -1} }
func (q *Queue) Replace(songs []subsonic.Song, index int) bool {
	if index < 0 || index >= len(songs) {
		return false
	}
	q.Songs = append([]subsonic.Song(nil), songs...)
	q.Index = index
	return true
}
func (q *Queue) Add(song subsonic.Song) { q.Songs = append(q.Songs, song) }
func (q *Queue) Select(index int) bool {
	if index < 0 || index >= len(q.Songs) {
		return false
	}
	q.Index = index
	return true
}
func (q *Queue) Next() bool     { return q.Select(q.Index + 1) }
func (q *Queue) Previous() bool { return q.Select(q.Index - 1) }
func (q *Queue) Current() (subsonic.Song, bool) {
	if q.Index < 0 || q.Index >= len(q.Songs) {
		return subsonic.Song{}, false
	}
	return q.Songs[q.Index], true
}
