package main

import "testing"

func TestEqualizerFilter(t *testing.T) {
	if got := (eqCurve{}).filter(); got != "" {
		t.Fatalf("flat filter %q", got)
	}
	got := presetCurve(eqBass, eqCurve{}).filter()
	want := "lavfi=[equalizer=f=32:width_type=q:w=1:g=6,equalizer=f=64:width_type=q:w=1:g=5,equalizer=f=125:width_type=q:w=1:g=3,equalizer=f=250:width_type=q:w=1:g=1,equalizer=f=500:width_type=q:w=1:g=0,equalizer=f=1000:width_type=q:w=1:g=0,equalizer=f=2000:width_type=q:w=1:g=0,equalizer=f=4000:width_type=q:w=1:g=0,equalizer=f=8000:width_type=q:w=1:g=0,equalizer=f=16000:width_type=q:w=1:g=0]"
	if got != want {
		t.Fatalf("bass filter %q", got)
	}
	rock := presetCurve(eqRock, eqCurve{})
	if rock.filter() == "" || !presetCurve(eqFlat, rock).flat() {
		t.Fatal("preset apply")
	}
	custom := eqCurve{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if presetCurve(eqCustom, custom) != custom {
		t.Fatal("custom preset replaced the curve")
	}
	over := eqCurve{40}
	over[1] = -40
	clamped := over.clamped()
	if clamped[0] != eqGainMax || clamped[1] != eqGainMin {
		t.Fatalf("clamp %v", clamped)
	}
}
