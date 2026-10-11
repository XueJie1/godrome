package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "godrome", "settings.json")
	s, err := Load(p)
	if err != nil || s.Volume != 70 {
		t.Fatal("defaults")
	}
	s.Server = "https://music.example.com"
	s.Username = "user"
	s.Volume = 35
	s.SidebarWidth = 180
	s.QueueWidth = 240
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got != s {
		t.Fatalf("round trip: %v", err)
	}
	b, _ := os.ReadFile(p)
	for _, secret := range []string{"password", "token", "stream"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("persisted credential field")
		}
	}
}
