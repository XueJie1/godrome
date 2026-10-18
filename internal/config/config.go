package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Settings is the JSON file. Passwords stay in the OS keyring; tokens,
// stream URLs, queues and API results are not persisted.
type Settings struct {
	Server       string  `json:"server"`
	Username     string  `json:"username"`
	Volume       float64 `json:"volume"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	SidebarWidth float32 `json:"sidebarWidth,omitempty"`
	QueueWidth   float32 `json:"queueWidth,omitempty"`
	// Accent is #rrggbb. Empty keeps Apple Music pink.
	Accent string `json:"accent,omitempty"`
	// Appearance is system, light, or dark. Empty follows the system.
	Appearance string `json:"appearance,omitempty"`
	// EQPreset names a curve. Empty means the gains are custom.
	EQPreset string `json:"eqPreset,omitempty"`
	// EQGains holds ten decibel values, low frequency first. Omitted means flat.
	EQGains []float64 `json:"eqGains,omitempty"`
}

func Defaults() Settings { return Settings{Volume: 70, Width: 1080, Height: 720} }
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "godrome", "settings.json"), nil
}
func Load(path string) (Settings, error) {
	s := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return Defaults(), err
	}
	s.Volume = max(0, min(100, s.Volume))
	s.Width = max(800, min(3840, s.Width))
	s.Height = max(560, min(2160, s.Height))
	if s.SidebarWidth != 0 {
		s.SidebarWidth = max(64, min(360, s.SidebarWidth))
	}
	if s.QueueWidth != 0 {
		s.QueueWidth = max(200, min(480, s.QueueWidth))
	}
	if !validAccent(s.Accent) {
		s.Accent = ""
	}
	switch s.Appearance {
	case "", "system", "light", "dark":
	default:
		s.Appearance = ""
	}
	if len(s.EQGains) != 10 {
		s.EQGains = nil
	} else {
		for i, g := range s.EQGains {
			s.EQGains[i] = max(-12, min(12, g))
		}
	}
	return s, nil
}

func validAccent(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*.json")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
