// Copyright 2023 Percona LLC
//
// Minimal macro expansion logic to replace macros.ApplyMacros for PMM-dump.
// Only supports $__range, $__from, $__to, and similar time macros.

package templating

import (
	"strings"
	"time"
)

// ApplyMacros replaces supported Grafana macros in the query string.
func ApplyMacros(query string, from, to time.Time) string {
	duration := to.Sub(from)
	query = strings.ReplaceAll(query, "$__range", duration.String())
	query = strings.ReplaceAll(query, "$__from", formatUnix(from))
	query = strings.ReplaceAll(query, "$__to", formatUnix(to))
	// Add more macro replacements as needed
	return query
}

func formatUnix(t time.Time) string {
	return strings.TrimSuffix(strings.TrimPrefix(t.Format("2006-01-02T15:04:05Z07:00"), "T"), "Z")
}
