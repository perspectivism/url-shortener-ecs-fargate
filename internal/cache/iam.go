package cache

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// emptyPayloadHash is the SHA-256 of an empty body (SigV4 presigned GET).
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// tokenTTL is the IAM auth token validity (ElastiCache: 15 minutes).
const tokenTTL = 15 * time.Minute

// IAMTokenProvider generates ElastiCache IAM auth tokens: a SigV4-presigned
// "GET http://{cache-name}/?Action=connect&User={user-id}[&ResourceType=ServerlessCache]"
// for service "elasticache", with the "http://" prefix stripped. See
// https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/auth-iam.html
type IAMTokenProvider struct {
	Credentials aws.CredentialsProvider
	UserID      string
	CacheName   string // lowercase (ElastiCache lowercases names at creation)
	Region      string
	Serverless  bool
	Now         func() time.Time
	signer      *v4.Signer
}

// Token returns a fresh token. It signs locally; only credential refresh (if
// due) touches the network.
func (p *IAMTokenProvider) Token(ctx context.Context) (string, error) {
	creds, err := p.Credentials.Retrieve(ctx)
	if err != nil {
		return "", fmt.Errorf("iam token: credentials: %w", err)
	}
	q := url.Values{}
	q.Set("Action", "connect")
	q.Set("User", p.UserID)
	if p.Serverless {
		q.Set("ResourceType", "ServerlessCache")
	}
	q.Set("X-Amz-Expires", fmt.Sprint(int(tokenTTL.Seconds())))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+p.CacheName+"/?"+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("iam token: request: %w", err)
	}
	signer := p.signer
	if signer == nil {
		signer = v4.NewSigner()
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	signed, _, err := signer.PresignHTTP(ctx, creds, req, emptyPayloadHash, "elasticache", p.Region, now().UTC())
	if err != nil {
		return "", fmt.Errorf("iam token: presign: %w", err)
	}
	return strings.TrimPrefix(signed, "http://"), nil
}
