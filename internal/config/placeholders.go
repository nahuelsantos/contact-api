package config

import "regexp"

var placeholderRe = regexp.MustCompile(`\{\{\s*([a-z0-9_]+)\s*\}\}`)

// Placeholders returns the names used inside {{ name }} markers, in order.
func Placeholders(s string) []string {
	var names []string
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		names = append(names, m[1])
	}

	return names
}
