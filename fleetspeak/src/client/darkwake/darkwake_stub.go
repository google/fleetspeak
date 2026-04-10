//go:build !darwin
// +build !darwin

// Package darkwake provides utilities for handling macOS dark wake events.
package darkwake

// RegisterXPC is a no-op on non-darwin platforms.
func RegisterXPC() {}
