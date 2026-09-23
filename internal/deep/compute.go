package deep

import (
	"errors"
	"strings"
	"time"
)

// ComputeBinding identifies the provider instance that owns an on-box
// worktree and its telemetry. Session state is never attached by recency.
type ComputeBinding struct {
	Provider   string          `json:"provider"`
	InstanceID int64           `json:"instanceId"`
	BoundAt    time.Time       `json:"boundAt"`
	Rebinds    []ComputeRebind `json:"rebinds,omitempty"`
}

// ComputeRebind is an explicit, durable transition between provider
// instances. FromInstanceID is zero only when migrating state that predates
// persisted compute identity.
type ComputeRebind struct {
	FromInstanceID int64     `json:"fromInstanceId"`
	ToInstanceID   int64     `json:"toInstanceId"`
	At             time.Time `json:"at"`
	Reason         string    `json:"reason"`
}

func (s *DeepState) BindCompute(provider string, instanceID int64, at time.Time) error {
	provider = strings.TrimSpace(provider)
	if provider == "" || instanceID <= 0 {
		return errors.New("compute binding requires a provider and positive instance id")
	}
	if s.ComputeBinding != nil {
		return errors.New("compute binding already exists; use an explicit rebind")
	}
	s.ComputeBinding = &ComputeBinding{Provider: provider, InstanceID: instanceID, BoundAt: at.UTC()}
	return nil
}

func (s *DeepState) RebindCompute(provider string, instanceID int64, reason string, at time.Time) error {
	provider = strings.TrimSpace(provider)
	reason = strings.TrimSpace(reason)
	if provider == "" || instanceID <= 0 || reason == "" {
		return errors.New("compute rebind requires a provider, positive instance id, and reason")
	}
	if s.ComputeBinding != nil && s.ComputeBinding.Provider != provider {
		return errors.New("compute rebind cannot change providers")
	}
	from := int64(0)
	if s.ComputeBinding != nil {
		from = s.ComputeBinding.InstanceID
		if from == instanceID {
			return errors.New("compute rebind target is already bound")
		}
	} else {
		s.ComputeBinding = &ComputeBinding{Provider: provider, BoundAt: at.UTC()}
	}
	s.ComputeBinding.Rebinds = append(s.ComputeBinding.Rebinds, ComputeRebind{
		FromInstanceID: from,
		ToInstanceID:   instanceID,
		At:             at.UTC(),
		Reason:         reason,
	})
	s.ComputeBinding.InstanceID = instanceID
	return nil
}
