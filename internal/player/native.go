//go:build cgo

package player

/*
#cgo linux LDFLAGS: -ldl
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"
)

type native struct{ ptr *C.gd_mpv }
type nativeEvent struct {
	kind   int
	name   string
	number float64
	flag   bool
	reason int
	code   int
	entry  int64
}

const (
	eventNone     = int(C.MPV_EVENT_NONE)
	eventStart    = int(C.MPV_EVENT_START_FILE)
	eventLoaded   = int(C.MPV_EVENT_FILE_LOADED)
	eventEnd      = int(C.MPV_EVENT_END_FILE)
	eventProperty = int(C.MPV_EVENT_PROPERTY_CHANGE)
	endEOF        = int(C.MPV_END_FILE_REASON_EOF)
	endError      = int(C.MPV_END_FILE_REASON_ERROR)
)

func openNative(audioOutput string) (*native, error) {
	n := &native{ptr: C.gd_open()}
	if n.ptr == nil {
		return nil, errors.New("无法加载 libmpv 2：请按 README 安装系统动态库")
	}
	fail := func(err error) (*native, error) { n.close(); return nil, err }
	if C.gd_create(n.ptr) < 0 {
		return fail(errors.New("无法创建 libmpv 播放器"))
	}
	options := [][2]string{
		// Audio backends otherwise publish authenticated stream URLs as media.name.
		{"force-media-title", "Godrome"}, {"audio-client-name", "Godrome"},
		{"config", "no"}, {"load-scripts", "no"}, {"video", "no"}, {"audio-display", "no"}, {"autoload-files", "no"},
		{"terminal", "no"}, {"msg-level", "all=no"}, {"idle", "yes"}, {"keep-open", "no"},
		{"cache", "yes"}, {"demuxer-max-bytes", "4MiB"}, {"demuxer-max-back-bytes", "0"}, {"demuxer-readahead-secs", "5"}, {"network-timeout", "15"},
	}
	if audioOutput != "" {
		options = append(options, [2]string{"ao", audioOutput})
	}
	for _, o := range options {
		if err := n.option(o[0], o[1]); err != nil {
			return fail(err)
		}
	}
	if code := C.gd_init(n.ptr); code < 0 {
		return fail(fmt.Errorf("libmpv 初始化失败（代码 %d）", int(code)))
	}
	for _, o := range []struct {
		name   string
		format C.int
	}{{"time-pos", C.MPV_FORMAT_DOUBLE}, {"duration", C.MPV_FORMAT_DOUBLE}, {"pause", C.MPV_FORMAT_FLAG}, {"paused-for-cache", C.MPV_FORMAT_FLAG}, {"seeking", C.MPV_FORMAT_FLAG}} {
		name := C.CString(o.name)
		code := C.gd_observe(n.ptr, name, o.format)
		C.free(unsafe.Pointer(name))
		if code < 0 {
			return fail(errors.New("libmpv 属性监听失败"))
		}
	}
	return n, nil
}
func (n *native) option(name, value string) error {
	a, b := C.CString(name), C.CString(value)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	if code := C.gd_option(n.ptr, a, b); code < 0 {
		return fmt.Errorf("libmpv 不支持选项 %s（代码 %d）", name, int(code))
	}
	return nil
}
func (n *native) set(name, value string) error {
	a, b := C.CString(name), C.CString(value)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	if code := C.gd_set(n.ptr, a, b); code < 0 {
		return fmt.Errorf("播放器操作失败（代码 %d）", int(code))
	}
	return nil
}
func (n *native) command(args ...string) error {
	var p [3]*C.char
	for i, arg := range args {
		p[i] = C.CString(arg)
		defer C.free(unsafe.Pointer(p[i]))
	}
	if code := C.gd_command(n.ptr, p[0], p[1], p[2]); code < 0 {
		return fmt.Errorf("播放器命令失败（代码 %d）", int(code))
	}
	return nil
}
func (n *native) load(path string) error {
	if err := n.set("pause", "no"); err != nil {
		return err
	}
	return n.command("loadfile", path, "replace")
}
func (n *native) pause(paused bool) error {
	value := "no"
	if paused {
		value = "yes"
	}
	return n.set("pause", value)
}
func (n *native) seek(seconds float64) error {
	return n.command("seek", strconv.FormatFloat(seconds, 'f', 3, 64), "absolute")
}
func (n *native) volume(value float64) error {
	return n.set("volume", strconv.FormatFloat(value, 'f', 1, 64))
}
func (n *native) stop() error { return n.command("stop") }
func (n *native) close()      { C.gd_close(n.ptr); n.ptr = nil }
func (n *native) wait(seconds float64) nativeEvent {
	e := C.gd_wait(n.ptr, C.double(seconds))
	out := nativeEvent{kind: int(e.event_id)}
	switch out.kind {
	case eventStart:
		data := (*C.mpv_event_start_file)(e.data)
		out.entry = int64(data.playlist_entry_id)
	case eventEnd:
		data := (*C.mpv_event_end_file)(e.data)
		out.reason = int(data.reason)
		out.code = int(data.error)
		out.entry = int64(data.playlist_entry_id)
	case eventProperty:
		p := (*C.mpv_event_property)(e.data)
		out.name = C.GoString(p.name)
		if p.data != nil {
			if p.format == C.MPV_FORMAT_DOUBLE {
				out.number = float64(*(*C.double)(p.data))
			}
			if p.format == C.MPV_FORMAT_FLAG {
				out.flag = *(*C.int)(p.data) != 0
			}
		}
	}
	return out
}
