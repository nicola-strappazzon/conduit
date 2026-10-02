package aws

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEndpoint(t *testing.T) {
	client := Client{Region: "eu-central-1"}

	assert.Equal(t, "https://ssm.eu-central-1.amazonaws.com", client.Endpoint())
}
