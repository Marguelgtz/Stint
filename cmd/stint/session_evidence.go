package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Marguelgtz/Stint/internal/config"
)

const sessionEvidenceDirectory = "sessions"

func sessionEvidenceDir(paths config.Paths, instanceID int64) string {
	return filepath.Join(paths.StateDir, sessionEvidenceDirectory, strconv.FormatInt(instanceID, 10))
}

func ensureSessionEvidenceDir(paths config.Paths, instanceID int64) (string, error) {
	if instanceID <= 0 {
		return "", errors.New("session evidence requires a positive instance id")
	}
	if err := paths.Ensure(); err != nil {
		return "", err
	}
	dir := sessionEvidenceDir(paths, instanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session evidence directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure session evidence directory: %w", err)
	}
	return dir, nil
}

func sessionEvidenceLogPath(paths config.Paths, instanceID int64, name string) (string, error) {
	if filepath.Base(name) != name || name == "." || name == "" {
		return "", fmt.Errorf("invalid session evidence file name %q", name)
	}
	dir, err := ensureSessionEvidenceDir(paths, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func sessionEvidenceRelativePath(instanceID int64, name string) string {
	return filepath.Join(sessionEvidenceDirectory, strconv.FormatInt(instanceID, 10), name)
}
