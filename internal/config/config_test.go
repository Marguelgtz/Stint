package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadCredentials(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		ConfigDir:       filepath.Join(root, "config"),
		StateDir:        filepath.Join(root, "state"),
		CredentialsFile: filepath.Join(root, "config", "credentials.json"),
		SSHDir:          filepath.Join(root, "config", "ssh"),
		SSHPrivateKey:   filepath.Join(root, "config", "ssh", "id_ed25519"),
		SSHPublicKey:    filepath.Join(root, "config", "ssh", "id_ed25519.pub"),
	}
	want := Credentials{Vast: VastCredentials{APIKey: "test-secret"}}
	if err := SaveCredentials(paths, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.CredentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("credential permissions = %o, want 600", got)
	}
	got, err := LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got.Vast.APIKey != want.Vast.APIKey {
		t.Fatalf("API key = %q, want %q", got.Vast.APIKey, want.Vast.APIKey)
	}
}
