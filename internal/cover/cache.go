package cover

import (
	"bytes"
	"container/list"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

const Budget = 8 << 20

type entry struct {
	id     string
	bitmap *ui.Bitmap
	size   int
}

// Cache has two download/decode workers, a bounded request queue and a decoded LRU.
// Each page owns one cache: closing a page cancels its work and drops its images.
type Cache struct {
	ctx     context.Context
	mu      sync.Mutex
	items   map[string]*list.Element
	pending map[string]bool
	lru     list.List
	bytes   int
	jobs    chan string
	fetch   func(context.Context, string) ([]byte, error)
	changed func()
}

func New(ctx context.Context, fetch func(context.Context, string) ([]byte, error), changed func()) *Cache {
	c := &Cache{ctx: ctx, items: make(map[string]*list.Element), pending: make(map[string]bool), jobs: make(chan string, 32), fetch: fetch, changed: changed}
	for range 2 {
		go c.worker()
	}
	return c
}

// Get never waits for I/O and only schedules visible rows.
func (c *Cache) Get(id string) *ui.Bitmap {
	if id == "" || c.ctx.Err() != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.items[id]; e != nil {
		c.lru.MoveToFront(e)
		return e.Value.(entry).bitmap
	}
	if !c.pending[id] {
		select {
		case c.jobs <- id:
			c.pending[id] = true
		default:
		}
	}
	return nil
}
func decode(data []byte) (*ui.Bitmap, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	// Bound hostile full-size responses before decoding. Requested cover size is 256.
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1024 || cfg.Height > 1024 {
		return nil, 1
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 1
	}
	const side = 256
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	// Include the mip pyramid used when the cover is drawn smaller than its pixels.
	return ui.NewBitmap(dst), side * side * 4 * 4 / 3
}
func (c *Cache) worker() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case id := <-c.jobs:
			data, err := c.fetch(c.ctx, id)
			var b *ui.Bitmap
			cost := 1
			if err == nil {
				b, cost = decode(data)
			}
			if c.ctx.Err() != nil {
				return
			}
			c.mu.Lock()
			delete(c.pending, id)
			c.items[id] = c.lru.PushFront(entry{id: id, bitmap: b, size: cost})
			c.bytes += cost
			for c.bytes > Budget || len(c.items) > 256 {
				e := c.lru.Back()
				v := e.Value.(entry)
				delete(c.items, v.id)
				c.bytes -= v.size
				c.lru.Remove(e)
			}
			c.mu.Unlock()
			if c.changed != nil {
				c.changed()
			}
		}
	}
}
