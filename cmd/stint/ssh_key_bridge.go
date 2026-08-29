package main

import (
	"github.com/Marguelgtz/Stint/internal/config"
	localenv "github.com/Marguelgtz/Stint/internal/local"
)

func ensureLocalSSHKey(paths config.Paths) (string, bool, error) {
	return localenv.EnsureSSHKey(paths)
}
