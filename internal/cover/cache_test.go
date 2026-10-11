package cover

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedWorkersCacheAndCancellation(t *testing.T) {
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 96, 96)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, peak, calls atomic.Int32
	changed := make(chan struct{}, 512)
	c := New(ctx, func(ctx context.Context, id string) ([]byte, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		calls.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
			return data.Bytes(), nil
		}
	}, func() { changed <- struct{}{} })
	for batch := range 8 {
		for i := range 30 {
			c.Get(fmt.Sprint(batch*30 + i))
		}
		for range 30 {
			select {
			case <-changed:
			case <-time.After(3 * time.Second):
				t.Fatal("downloads stalled")
			}
		}
	}
	if peak.Load() > 2 || calls.Load() != 240 {
		t.Fatalf("workers peak=%d calls=%d", peak.Load(), calls.Load())
	}
	c.mu.Lock()
	used, count := c.bytes, len(c.items)
	c.mu.Unlock()
	if used > Budget || count >= 240 {
		t.Fatalf("cache not bounded: %d bytes, %d entries", used, count)
	}
	before := calls.Load()
	if c.Get("239") == nil {
		t.Fatal("recent thumbnail not cached")
	}
	if calls.Load() != before {
		t.Fatal("cache hit scheduled download")
	}
	cancel()
	if c.Get("after exit") != nil {
		t.Fatal("canceled cache is live")
	}
}
