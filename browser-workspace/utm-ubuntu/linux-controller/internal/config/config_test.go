package config

import "testing"

func TestRejectsIncompleteVPSConfiguration(t *testing.T) {
	value := VPSConfig{Schema: Schema, Version: 1}
	if value.Validate() == nil {
		t.Fatal("expected incomplete VPS configuration to fail")
	}
}
