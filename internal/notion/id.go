package notion

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var compactIDPattern = regexp.MustCompile(`(?i)[0-9a-f]{32}`)
var dashedIDPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func ParseID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("notion id is required")
	}

	search := value
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		search = parsed.Path
	}

	if match := dashedIDPattern.FindString(search); match != "" {
		return NormalizeID(match), nil
	}
	if matches := compactIDPattern.FindAllString(search, -1); len(matches) > 0 {
		return NormalizeID(matches[len(matches)-1]), nil
	}

	return "", fmt.Errorf("could not find a Notion page, block, database, or data source id in %q", value)
}

func NormalizeID(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(value) != 32 {
		return value
	}
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}

func ShortID(value string) string {
	value = strings.ReplaceAll(value, "-", "")
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
