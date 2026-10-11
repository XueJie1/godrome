//go:build !cgo

package player

import "errors"

type native struct{}
type nativeEvent struct {
	kind         int
	name         string
	number       float64
	flag         bool
	reason, code int
	entry        int64
}

const (
	eventNone     = 0
	eventStart    = 6
	eventLoaded   = 8
	eventEnd      = 7
	eventProperty = 22
	endEOF        = 0
	endError      = 4
)

func openNative(string) (*native, error) {
	return nil, errors.New("此构建未启用 cgo，无法播放；请使用 CGO_ENABLED=1 重新构建")
}
func (*native) load(string) error         { return nil }
func (*native) pause(bool) error          { return nil }
func (*native) seek(float64) error        { return nil }
func (*native) volume(float64) error      { return nil }
func (*native) setEqualizer(string) error { return nil }
func (*native) stop() error               { return nil }
func (*native) close()                    {}
func (*native) wait(float64) nativeEvent  { return nativeEvent{} }
