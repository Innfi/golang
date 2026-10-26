// Package awscreds turns an Account row into an aws.Config that assumes the
// account's role, with credentials cached and auto-refreshed per account.
package awscreds

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/innfi/awsinv/store"
)

// Factory builds per-account configs from one "hub" identity
// (the identity this process runs as: EC2/ECS/EKS role, SSO profile, etc.).
type Factory struct {
	base        aws.Config
	stsClient   *sts.Client
	sessionName string

	mu    sync.Mutex
	cache map[string]*aws.CredentialsCache // key: roleARN|externalID
}

func NewFactory(base aws.Config, sessionName string) *Factory {
	return &Factory{
		base:        base,
		stsClient:   sts.NewFromConfig(base),
		sessionName: sessionName,
		cache:       map[string]*aws.CredentialsCache{},
	}
}

// ConfigFor returns a config for the account in the given region.
// Credentials are shared across regions of the same account, so AssumeRole is
// called once per account (and again only when the session nears expiry).
func (f *Factory) ConfigFor(acct store.Account, region string) aws.Config {
	cfg := f.base.Copy()
	cfg.Region = region
	cfg.Credentials = f.credsFor(acct)
	return cfg
}

func (f *Factory) credsFor(acct store.Account) *aws.CredentialsCache {
	key := acct.RoleARN + "|" + acct.ExternalID

	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cache[key]; ok {
		return c
	}

	p := stscreds.NewAssumeRoleProvider(f.stsClient, acct.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = f.sessionName
		o.Duration = time.Hour
		if acct.ExternalID != "" {
			o.ExternalID = aws.String(acct.ExternalID)
		}
	})
	c := aws.NewCredentialsCache(p)
	f.cache[key] = c
	return c
}

// Verify does a cheap GetCallerIdentity to fail fast on broken trust policies.
func (f *Factory) Verify(ctx context.Context, acct store.Account, region string) (string, error) {
	out, err := sts.NewFromConfig(f.ConfigFor(acct, region)).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Arn), nil
}
