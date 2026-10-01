package main

import (
	"github.com/c360studio/semstreams/config"
	"testing"
)

func TestAuthoredConfigUsesDeclaredAuthority(t *testing.T) {
	cfg, err := loadConfig("../../configs/flock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Platform.Org != "c360" || cfg.Platform.ID != "semboids" {
		t.Fatalf("authority=%+v", cfg.Platform)
	}
}
func TestExtractPlatformUsesEffectiveIdentity(t *testing.T) {
	cfg := &config.Config{}
	cfg.Platform.Org = "c360"
	cfg.Platform.ID = "semboids-minted"
	got := extractPlatformMeta(cfg)
	if got.Org != "c360" || got.Platform != "semboids-minted" {
		t.Fatalf("effective platform=%+v", got)
	}
}
