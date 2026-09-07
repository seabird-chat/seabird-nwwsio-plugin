package nwwsio

import (
	"regexp"
	"strings"
)

var (
	sequenceLineRe = regexp.MustCompile(`^\d{3}$`)
	wmoHeadingRe   = regexp.MustCompile(`^[A-Z]{4}\d{2} [A-Z]{4} \d{6}( [A-Z]{3})?$`)
	awipsLineRe    = regexp.MustCompile(`^[A-Z0-9]{4,6}$`)
)

// NWWS-OI puts a blank line after every line and a three-line transmission
// envelope (sequence number, WMO heading, AWIPS ID) on top.
func ProductBody(text string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	if doubleSpaced(lines) {
		single := make([]string, 0, len(lines)/2+1)
		for i := 0; i < len(lines); i += 2 {
			single = append(single, lines[i])
		}
		lines = single
	}

	lines = trimLeadingBlank(lines)
	if len(lines) > 0 && sequenceLineRe.MatchString(lines[0]) {
		lines = trimLeadingBlank(lines[1:])
	}
	if len(lines) > 0 && wmoHeadingRe.MatchString(lines[0]) {
		lines = trimLeadingBlank(lines[1:])
		if len(lines) > 0 && awipsLineRe.MatchString(lines[0]) {
			lines = trimLeadingBlank(lines[1:])
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func doubleSpaced(lines []string) bool {
	if len(lines) < 4 {
		return false
	}
	for i := 1; i < len(lines); i += 2 {
		if lines[i] != "" {
			return false
		}
	}
	return true
}

func trimLeadingBlank(lines []string) []string {
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return lines
}
