package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

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
	for _, secret := range []string{"password", "token", "stream", "secret"} {
		if strings.Contains(strings.ToLower(string(b)), secret) {
			t.Fatal("persisted credential field")
		}
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	const server = "https://music.example/navidrome"
	const user = "user"
	if _, ok, err := LoadPassword(server, user); err != nil || ok {
		t.Fatal("missing password should be empty")
	}
	if err := SavePassword(server, user, "secret"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadPassword(server, user)
	if err != nil || !ok || got != "secret" {
		t.Fatalf("round trip ok=%t err=%v", ok, err)
	}
	if _, ok, err := LoadPassword(server, "other"); err != nil || ok {
		t.Fatal("password leaked across accounts")
	}
	if err := DeletePassword(server, user); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadPassword(server, user); err != nil || ok {
		t.Fatal("deleted password still present")
	}
	if err := DeletePassword(server, user); err != nil {
		t.Fatal(err)
	}
}
