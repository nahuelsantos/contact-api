package guard

import "strings"

// honeypotField is the hidden field real users never fill in.
const honeypotField = "_gotcha"

// Honeypot reports whether the honeypot field was filled in.
func Honeypot(in map[string]string) bool {
	return strings.TrimSpace(in[honeypotField]) != ""
}
