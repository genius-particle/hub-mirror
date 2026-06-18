package pkg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseIssueForm(t *testing.T) {
	body := "### Platform\n\nlinux/arm64\n\n### Image\n\nmy-app\n\n### Tag\n\nv1.0\n\n### Dockerfile\n\n```dockerfile\nFROM golang:1.21\nWORKDIR /app\n```\n"

	sections := ParseIssueForm(body)

	assert.Equal(t, "linux/arm64", sections["Platform"])
	assert.Equal(t, "my-app", sections["Image"])
	assert.Equal(t, "v1.0", sections["Tag"])
}

func TestParseIssueForm_EmptyPlatform(t *testing.T) {
	body := "### Platform\n\n\n### Image\n\nmy-app\n\n### Tag\n\nv1.0\n\n### Dockerfile\n\n```dockerfile\nFROM scratch\n```\n"

	sections := ParseIssueForm(body)

	assert.Equal(t, "", sections["Platform"])
	assert.Equal(t, "my-app", sections["Image"])
	assert.Equal(t, "v1.0", sections["Tag"])
}

func TestExtractFenced(t *testing.T) {
	in := "```dockerfile\nFROM golang:1.21\nWORKDIR /app\nRUN echo 'hello'\n```"
	want := "FROM golang:1.21\nWORKDIR /app\nRUN echo 'hello'"
	assert.Equal(t, want, ExtractFenced(in))
}

func TestExtractFenced_NoFence(t *testing.T) {
	in := "FROM scratch"
	assert.Equal(t, "FROM scratch", ExtractFenced(in))
}

func TestExtractFenced_PreservesInnerBackticks(t *testing.T) {
	in := "```dockerfile\nRUN echo `date`\n```"
	assert.Equal(t, "RUN echo `date`", ExtractFenced(in))
}

func TestParseIssueForm_NoResponsePlaceholder(t *testing.T) {
	// GitHub renders an unfilled optional Issue Form field as "_No response_";
	// it must be treated as empty so defaulting/validation work downstream.
	body := "### Platform\n\n_No response_\n\n### Image\n\nmy-app\n\n### Tag\n\nv1.0\n\n### Dockerfile\n\n```dockerfile\nFROM scratch\n```\n"

	sections := ParseIssueForm(body)

	assert.Equal(t, "", sections["Platform"], `"_No response_" should be treated as empty`)
	assert.Equal(t, "my-app", sections["Image"])
	assert.Equal(t, "v1.0", sections["Tag"])
}
