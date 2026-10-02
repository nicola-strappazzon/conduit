package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// PluginBinary is the session-manager-plugin executable that speaks the
// Session Manager data-channel protocol. The AWS SDK only negotiates the
// session; this external binary (the same one the AWS CLI shells out to)
// is what actually moves bytes.
const PluginBinary = "session-manager-plugin"

// SessionParams describes the SSM session to start.
type SessionParams struct {
	Target     string
	Document   string
	Parameters map[string][]string
}

// Session is an active SSM session, holding the pieces needed to hand off
// to session-manager-plugin.
type Session struct {
	ID       string
	response []byte
	request  []byte
}

// StartSession calls ssm:StartSession and prepares the response/request
// payloads session-manager-plugin expects, in the same wire format the AWS
// CLI passes to it.
func (c *Client) StartSession(ctx context.Context, p SessionParams) (*Session, error) {
	out, err := c.ssm.StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(p.Target),
		DocumentName: aws.String(p.Document),
		Parameters:   p.Parameters,
	})
	if err != nil {
		return nil, fmt.Errorf("StartSession: %w", err)
	}

	id := aws.ToString(out.SessionId)

	response, err := json.Marshal(struct {
		SessionId  string `json:"SessionId"`
		TokenValue string `json:"TokenValue"`
		StreamUrl  string `json:"StreamUrl"`
	}{
		SessionId:  id,
		TokenValue: aws.ToString(out.TokenValue),
		StreamUrl:  aws.ToString(out.StreamUrl),
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling session response: %w", err)
	}

	request, err := json.Marshal(struct {
		Target       string              `json:"Target"`
		DocumentName string              `json:"DocumentName"`
		Parameters   map[string][]string `json:"Parameters"`
	}{
		Target:       p.Target,
		DocumentName: p.Document,
		Parameters:   p.Parameters,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request parameters: %w", err)
	}

	return &Session{ID: id, response: response, request: request}, nil
}

// PluginArgs returns the exact arguments session-manager-plugin needs to
// open the data channel for this session, in the order the AWS CLI passes
// them: response, region, "StartSession", profile, request, endpoint.
func (c *Client) PluginArgs(s *Session) []string {
	return []string{
		string(s.response),
		c.Region,
		"StartSession",
		c.Profile,
		string(s.request),
		c.Endpoint(),
	}
}
