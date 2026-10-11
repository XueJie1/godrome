package main

import (
	"context"
	"encoding/csv"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Optional measurement only: no forced GC, and no credentials or app content.
func startMetrics(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	writer := csv.NewWriter(f)
	_ = writer.Write([]string{"elapsed_s", "heap_alloc_bytes", "heap_sys_bytes", "goroutines"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	start := time.Now()
	sample := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		_ = writer.Write([]string{strconv.FormatFloat(time.Since(start).Seconds(), 'f', 3, 64), strconv.FormatUint(m.HeapAlloc, 10), strconv.FormatUint(m.HeapSys, 10), strconv.Itoa(runtime.NumGoroutine())})
		writer.Flush()
	}
	sample()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				sample()
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done; f.Close() }) }, nil
}
