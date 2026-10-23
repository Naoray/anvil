//go:build !windows

package warm

// platformPathForms has no alternate path names outside Windows.
func platformPathForms(string) []string { return nil }
