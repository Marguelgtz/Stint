package router

import (
	"fmt"
	"github.com/Marguelgtz/Stint/internal/core"
)

func ResolveProfile(name string) (core.Profile, error) {
	profile, ok := core.BuiltinProfiles[name]
	if !ok {
		return core.Profile{}, fmt.Errorf("unknown profile %q: expected interactive or deep", name)
	}
	return profile, nil
}
