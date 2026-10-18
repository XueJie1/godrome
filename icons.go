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
	// Lucide disc-3, shown where a cover has not arrived.
	iconDisc = strokeIcon(`<circle cx="12" cy="12" r="10"/><path d="M6 12c0-1.7.7-3.2 1.8-4.2"/><circle cx="12" cy="12" r="2"/><path d="M18 12c0 1.7-.7 3.2-1.8 4.2"/>`)
	// Lucide chevron-left and chevron-right, for the history buttons.
	iconBack    = strokeIcon(`<path d="m15 18-6-6 6-6"/>`)
	iconForward = strokeIcon(`<path d="m9 18 6-6-6-6"/>`)
	// Lucide heart. The outline is the unset star; the filled path is the set star.
	iconHeart   = strokeIcon(`<path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z"/>`)
	iconHeartOn = icon(`<path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z"/>`)
	// Lucide sun, moon, and monitor, for the appearance choice.
	iconSun     = strokeIcon(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`)
	iconMoon    = strokeIcon(`<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`)
	iconMonitor = strokeIcon(`<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/>`)
	// Lucide library-big, disc-album, list-music, and settings, for the
	// sidebar entries. 喜爱 reuses iconHeart above. The label leaves the
	// entry when the sidebar is narrow.
	iconLibrary   = strokeIcon(`<path d="m16 6 4 14"/><path d="M12 6v14"/><path d="M8 8v12"/><path d="M4 4v16"/>`)
	iconAlbums    = strokeIcon(`<rect width="18" height="18" x="3" y="3" rx="2"/><circle cx="12" cy="12" r="3.5"/><path d="M7 6h.01"/><path d="M17 18h.01"/>`)
	iconPlaylists = strokeIcon(`<path d="M21 15V6"/><path d="M18.5 18a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z"/><path d="M12 12H3"/><path d="M16 6H3"/><path d="M12 18H3"/>`)
	iconSettings  = strokeIcon(`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`)
	// Lucide volume-2 and volume-x, beside the volume slider.
	iconVolume     = strokeIcon(`<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298z"/><path d="M16 9a5 5 0 0 1 0 6"/><path d="M19.364 18.364a9 9 0 0 0 0-12.728"/>`)
	iconVolumeMute = strokeIcon(`<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298z"/><line x1="22" x2="16" y1="9" y2="15"/><line x1="16" x2="22" y1="9" y2="15"/>`)
	// Lucide database-check, beside the page title when cached data is shown.
	iconOffline = strokeIcon(`<path d="m16 19 2 2 4-4"/><path d="M21 13.127V5"/><path d="M3 12A9 3 0 0 0 21 12"/><path d="M3 5V19A9 3 0 0 0 13.318 21.968"/><ellipse cx="12" cy="5" rx="9" ry="3"/>`)
)
