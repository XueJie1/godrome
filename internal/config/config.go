package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// No password, token, stream URL, queue or API result is persisted.
type Settings struct {
	Server       string  `json:"server"`
	Username     string  `json:"username"`
	Volume       float64 `json:"volume"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	SidebarWidth float32 `json:"sidebarWidth,omitempty"`
	QueueWidth   float32 `json:"queueWidth,omitempty"`
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
		s.SidebarWidth = max(160, min(360, s.SidebarWidth))
	}
	if s.QueueWidth != 0 {
		s.QueueWidth = max(200, min(480, s.QueueWidth))
	}
	return s, nil
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
