package main

import "github.com/egoist/mygo/ui"

func icon(svg string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + svg + `</svg>`))
}

// strokeIcon parses a Lucide icon. Those icons are ISC licensed; the notice is in docs/dependencies.md.
func strokeIcon(svg string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + svg + `</svg>`))
}

var (
	iconPrevious = icon(`<path d="M6 5h2.2v14H6zM18.8 6.2v11.6L9.6 12z"/>`)
	iconNext     = icon(`<path d="M15.8 5H18v14h-2.2zM5.2 6.2 14.4 12l-9.2 5.8z"/>`)
	iconPlay     = icon(`<path d="M8 5.8v12.4L18.2 12z"/>`)
	iconPause    = icon(`<path d="M6.5 5h3.4v14H6.5zM14.1 5h3.4v14h-3.4z"/>`)
	iconStop     = icon(`<path d="M6 6h12v12H6z"/>`)
	iconRetry    = icon(`<path fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" d="M19 12a7 7 0 1 1-2-4.9"/><path d="M19 4.5V9h-4.5z"/>`)
	iconMore     = icon(`<circle cx="6" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="18" cy="12" r="1.6"/>`)
	iconQueue    = icon(`<path d="M4 6h16v1.8H4zM4 11.1h16v1.8H4zM4 16.2h11v1.8H4z"/>`)
	// Lucide arrow-right, repeat, and repeat-1.
	iconOrder     = strokeIcon(`<path d="M5 12h14"/><path d="m12 5 7 7-7 7"/>`)
	iconRepeat    = strokeIcon(`<path d="m17 2 4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="m7 22-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/>`)
	iconRepeatOne = strokeIcon(`<path d="m17 2 4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="m7 22-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/><path d="M11 10h1v4"/>`)
)
