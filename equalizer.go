package main

import (
	"fmt"
	"strconv"
	"strings"
)

// eqBands is the 10-band graphic equalizer, one slider per octave from
// 32 Hz. Gains are decibels. Flat is every band at 0.
const eqBands = 10

var eqFreqs = [eqBands]int{32, 64, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

const (
	eqGainMin = -12.0
	eqGainMax = 12.0
)

// eqPreset names a stored curve. Empty and 自定义 are the same: the
// sliders are the source of truth.
type eqPreset string

const (
	eqCustom    eqPreset = ""
	eqFlat      eqPreset = "平坦"
	eqRock      eqPreset = "摇滚"
	eqPop       eqPreset = "流行"
	eqJazz      eqPreset = "爵士"
	eqClassical eqPreset = "古典"
	eqVocal     eqPreset = "人声"
	eqBass      eqPreset = "低音"
)

func eqPresetList() []eqPreset {
	return []eqPreset{eqCustom, eqFlat, eqRock, eqPop, eqJazz, eqClassical, eqVocal, eqBass}
}

func eqPresetLabel(p eqPreset) string {
	if p == eqCustom {
		return "自定义"
	}
	return string(p)
}

// eqCurve is ten gains in decibels, low frequency first.
type eqCurve [eqBands]float64

func (c eqCurve) flat() bool {
	for _, g := range c {
		if g != 0 {
			return false
		}
	}
	return true
}

func clampGain(v float64) float64 {
	return max(eqGainMin, min(eqGainMax, v))
}

func (c eqCurve) clamped() eqCurve {
	for i := range c {
		c[i] = clampGain(c[i])
	}
	return c
}

// presetCurve returns the curve a named preset applies. 自定义 keeps the
// curve the caller already has.
func presetCurve(p eqPreset, current eqCurve) eqCurve {
	switch p {
	case eqFlat:
		return eqCurve{}
	case eqRock:
		return eqCurve{5, 4, 2, 0, -1, -1, 1, 3, 4, 4}
	case eqPop:
		return eqCurve{-1, 1, 3, 4, 3, 0, -1, -1, 1, 2}
	case eqJazz:
		return eqCurve{3, 2, 1, 2, -1, -1, 0, 1, 2, 3}
	case eqClassical:
		return eqCurve{4, 3, 2, 1, -1, -1, 0, 2, 3, 4}
	case eqVocal:
		return eqCurve{-2, -1, 0, 2, 4, 4, 3, 1, 0, -1}
	case eqBass:
		return eqCurve{6, 5, 3, 1, 0, 0, 0, 0, 0, 0}
	default:
		return current.clamped()
	}
}

// eqFilter is the libmpv af value for this curve. A flat curve is empty,
// which removes the filter. Each band is one lavfi equalizer, Q=1,
// chained with commas. libmpv accepts that as one af entry.
func (c eqCurve) filter() string {
	c = c.clamped()
	if c.flat() {
		return ""
	}
	parts := make([]string, 0, eqBands)
	for i, hz := range eqFreqs {
		parts = append(parts, fmt.Sprintf("equalizer=f=%d:width_type=q:w=1:g=%s", hz, formatGain(c[i])))
	}
	return "lavfi=[" + strings.Join(parts, ",") + "]"
}

func formatGain(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
