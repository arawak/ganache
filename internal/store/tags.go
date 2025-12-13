package store

import (
	"sort"
	"strings"
)

func NormalizeTag(in string) string {
	trimmed := strings.TrimSpace(in)
	if trimmed == "" {
		return ""
	}
	collapsed := strings.Join(strings.Fields(trimmed), " ")
	return strings.ToLower(collapsed)
}

func NormalizeTags(tags []string) []string {
	set := make(map[string]struct{})
	for _, t := range tags {
		n := NormalizeTag(t)
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func TagText(tags []string) string {
	norm := NormalizeTags(tags)
	return strings.Join(norm, " ")
}
