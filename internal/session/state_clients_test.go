package session

import (
	"testing"

	"github.com/Marguelgtz/Stint/internal/config"
)

func TestStatePersistsClients(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir: root + "/config",
		StateDir:  root + "/state",
		SSHDir:    root + "/ssh",
	}
	state := State{InstanceID: 42, Clients: 2}
	if err := Save(paths, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Clients != 2 {
		t.Fatalf("loaded clients = %d, want 2", loaded.Clients)
	}
}
