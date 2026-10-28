//go:build !windows

package platform

func uiLanguages() []string { return envLanguages() }
