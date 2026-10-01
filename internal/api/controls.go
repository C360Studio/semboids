package api

import "sync"

// RuleControl is the dedicated live rule activation contract. It does not
// mutate component composition, which is sealed at boot.
type RuleControl interface {
	GetRuntimeConfig() map[string]any
	ValidateConfigUpdate(changes map[string]any) error
	ApplyConfigUpdate(changes map[string]any) error
}

// SimControl contains the simulation controls exposed by the UI.
type SimControl interface {
	ClearModifierKind(kind string) error
	SetGraphHz(hz float64) error
	GraphHz() float64
	GraphCounts() (snapshots, entities, dropped uint64)
	SpawnBoids(n int)
	ChurnHz() float64
	SetChurnHz(hz float64) error
}

// Controls holds only the two named application controls captured at factory
// construction. Services construct before components, so binding is deferred.
// It is deliberately not a component discovery or lifecycle registry.
type Controls struct {
	mu    sync.RWMutex
	rules RuleControl
	sim   SimControl
}

// BindRules captures the rule activation handle during boot composition.
func (c *Controls) BindRules(rules RuleControl) { c.mu.Lock(); defer c.mu.Unlock(); c.rules = rules }

// BindSim captures the simulation controls during boot composition.
func (c *Controls) BindSim(sim SimControl) { c.mu.Lock(); defer c.mu.Unlock(); c.sim = sim }

func (c *Controls) ruleControl() RuleControl {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rules
}
func (c *Controls) simControl() SimControl {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sim
}
