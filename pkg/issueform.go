package pkg

import "strings"

// parseIssueForm parses a GitHub Issue Form issue body into a map keyed by
// section label. Sections are delimited by lines starting with "### "; a
// section's value is the trimmed text between its header and the next header
// (or end of body).
func parseIssueForm(body string) map[string]string {
	sections := map[string]string{}
	var cur string
	var buf strings.Builder

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "### ") {
			if cur != "" {
				sections[cur] = strings.TrimSpace(buf.String())
			}
			cur = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			buf.Reset()
		} else if cur != "" {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	if cur != "" {
		sections[cur] = strings.TrimSpace(buf.String())
	}
	return sections
}

// extractFenced strips a surrounding markdown code fence (```...```) if present
// and returns the inner content verbatim. Content without a fence is returned
// unchanged.
func extractFenced(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
