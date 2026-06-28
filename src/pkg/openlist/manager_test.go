package openlist

import "testing"

func TestNewExternalManagerNormalizesEndpoint(t *testing.T) {
	manager := NewExternalManager(" http://127.0.0.1:5244/ ", " token ")

	if !manager.IsExternal() {
		t.Fatal("expected external manager")
	}
	if got := manager.GetAPIEndpoint(); got != "http://127.0.0.1:5244" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
	if got := manager.GetAPIToken(); got != "token" {
		t.Fatalf("unexpected token: %q", got)
	}
	if got := manager.GetWebUIPath(); got != "/remotetools/tool/openlist/" {
		t.Fatalf("unexpected web ui path: %q", got)
	}
}
