package entitlement

import "testing"

func TestEntitledRejectsAmbiguousScopeComponents(t *testing.T) {
	identity := Identity{Entitlements: []string{"eu|standard|general"}}
	if identity.Entitled("eu|standard", "general") {
		t.Fatal("delimiter-containing audience matched an ambiguous entitlement")
	}
	if identity.Entitled("eu", "standard|general") {
		t.Fatal("delimiter-containing service class matched an ambiguous entitlement")
	}
}

func TestEntitledMatchesExactScope(t *testing.T) {
	identity := Identity{Entitlements: []string{"eu-standard|general"}}
	if !identity.Entitled("eu-standard", "general") {
		t.Fatal("exact entitlement did not match")
	}
}
