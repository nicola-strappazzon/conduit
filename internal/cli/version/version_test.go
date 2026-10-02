package version_test

import (
	"bytes"
	"strings"
	"testing"

	"conduit/internal/cli/version"
	"github.com/stretchr/testify/assert"
)

func TestNewCommandMetadata(t *testing.T) {
	cmd := version.NewCommand()

	assert.Equal(t, "version", cmd.Use)
	assert.Equal(t, "Print the version number", cmd.Short)
}

func TestNewCommandPrintsVersion(t *testing.T) {
	previous := version.VERSION
	version.VERSION = "v0.0.0-test"
	t.Cleanup(func() { version.VERSION = previous })

	cmd := version.NewCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)

	assert.NoError(t, cmd.Execute())
	assert.Equal(t, "v0.0.0-test", strings.TrimSpace(output.String()))
}
