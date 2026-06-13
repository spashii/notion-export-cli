package exporter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Untitled"
	}

	var builder strings.Builder
	lastWasSpace := false
	for _, r := range value {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == 0:
			if !lastWasSpace {
				builder.WriteRune(' ')
				lastWasSpace = true
			}
		case unicode.IsSpace(r):
			if !lastWasSpace {
				builder.WriteRune(' ')
				lastWasSpace = true
			}
		case r < 32:
			continue
		default:
			builder.WriteRune(r)
			lastWasSpace = false
		}
	}

	name := strings.Trim(strings.TrimSpace(builder.String()), ".")
	if name == "" {
		name = "Untitled"
	}

	return truncateRunes(name, 120)
}

func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}

	var builder strings.Builder
	for i, r := range value {
		if i >= max {
			break
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}
