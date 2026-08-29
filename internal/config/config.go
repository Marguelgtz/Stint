package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir       string
	StateDir        string
	CredentialsFile string
	SSHDir          string
	SSHPrivateKey   string
	SSHPublicKey    string
}

func DefaultPaths() (Paths, error) {
	configBase, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config dir: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home dir: %w", err)
	}
	stateBase := os.Getenv("XDG_STATE_HOME")
	if stateBase == "" {
		stateBase = filepath.Join(home, ".local", "state")
	}
	configDir := filepath.Join(configBase, "stint")
	sshDir := filepath.Join(configDir, "ssh")
	return Paths{
		ConfigDir:       configDir,
		StateDir:        filepath.Join(stateBase, "stint"),
		CredentialsFile: filepath.Join(configDir, "credentials.json"),
		SSHDir:          sshDir,
		SSHPrivateKey:   filepath.Join(sshDir, "id_ed25519"),
		SSHPublicKey:    filepath.Join(sshDir, "id_ed25519.pub"),
	}, nil
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.ConfigDir, p.StateDir, p.SSHDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("secure %s: %w", dir, err)
		}
	}
	return nil
}

type Credentials struct {
	Vast VastCredentials `json:"vast"`
}

type VastCredentials struct {
	APIKey string `json:"api_key"`
}

func LoadCredentials(paths Paths) (Credentials, error) {
	data, err := os.ReadFile(paths.CredentialsFile)
	if err != nil {
		return Credentials{}, err
	}
	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	if credentials.Vast.APIKey == "" {
		return Credentials{}, errors.New("Vast API key is not configured")
	}
	return credentials, nil
}

func SaveCredentials(paths Paths, credentials Credentials) error {
	if credentials.Vast.APIKey == "" {
		return errors.New("refusing to save an empty Vast API key")
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(paths.ConfigDir, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create credential temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure credential temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close credentials: %w", err)
	}
	if err := os.Rename(tmpName, paths.CredentialsFile); err != nil {
		return fmt.Errorf("install credentials: %w", err)
	}
	if err := os.Chmod(paths.CredentialsFile, 0o600); err != nil {
		return fmt.Errorf("secure credentials: %w", err)
	}
	return nil
}
