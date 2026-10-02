package aws

import (
	"context"
	"errors"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/stretchr/testify/assert"
)

type fakeSSMClient struct {
	input  *ssm.StartSessionInput
	output *ssm.StartSessionOutput
	err    error
}

func (c *fakeSSMClient) StartSession(_ context.Context, input *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	c.input = input
	return c.output, c.err
}

func TestStartSession(t *testing.T) {
	ssmClient := &fakeSSMClient{output: &ssm.StartSessionOutput{
		SessionId:  awssdk.String("session-1"),
		TokenValue: awssdk.String("token"),
		StreamUrl:  awssdk.String("wss://session"),
	}}
	client := Client{ssm: ssmClient, Profile: "profile", Region: "eu-central-1"}
	params := SessionParams{
		Target:   "i-bastion",
		Document: "AWS-StartPortForwardingSession",
		Parameters: map[string][]string{
			"portNumber":      {"3306"},
			"localPortNumber": {"13306"},
		},
	}

	session, err := client.StartSession(context.Background(), params)

	if assert.NoError(t, err) {
		assert.Equal(t, "session-1", session.ID)
		assert.JSONEq(t, `{"SessionId":"session-1","TokenValue":"token","StreamUrl":"wss://session"}`, string(session.response))
		assert.JSONEq(t, `{"Target":"i-bastion","DocumentName":"AWS-StartPortForwardingSession","Parameters":{"portNumber":["3306"],"localPortNumber":["13306"]}}`, string(session.request))
	}
	assert.Equal(t, "i-bastion", awssdk.ToString(ssmClient.input.Target))
	assert.Equal(t, "AWS-StartPortForwardingSession", awssdk.ToString(ssmClient.input.DocumentName))
	assert.Equal(t, params.Parameters, ssmClient.input.Parameters)
	assert.Equal(t, []string{
		string(session.response),
		"eu-central-1",
		"StartSession",
		"profile",
		string(session.request),
		"https://ssm.eu-central-1.amazonaws.com",
	}, client.PluginArgs(session))
}

func TestStartSessionReturnsSSMError(t *testing.T) {
	ssmClient := &fakeSSMClient{err: errors.New("access denied")}
	client := Client{ssm: ssmClient}

	_, err := client.StartSession(context.Background(), SessionParams{})

	assert.ErrorContains(t, err, "StartSession: access denied")
}
