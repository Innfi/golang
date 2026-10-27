// Package inventory fetches resources across accounts/regions.
package inventory

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/store"
)

// VPC is our own flattened shape — don't leak SDK types into storage/APIs.
type VPC struct {
	AccountID string
	Region    string
	VpcID     string
	CIDR      string
	IsDefault bool
	State     string
	Name      string
	Tags      map[string]string
}

// ScanError records a per-target failure without aborting the whole run.
type ScanError struct {
	AccountID string
	Region    string
	Err       error
}

func (e ScanError) Error() string {
	return fmt.Sprintf("%s/%s: %v", e.AccountID, e.Region, e.Err)
}

type Collector struct {
	Creds              *awscreds.Factory
	DefaultRegions     []string // used when an account row has no regions
	Concurrency        int      // max parallel (account, region) scans
	AccountConcurrency int      // max parallel regions per account; 0 => Concurrency
}

// CollectVPCs scans every (account, region) pair. One bad account (revoked
// role, SCP deny, opt-in region disabled) is reported, not fatal.
// Cancelling ctx stops queued scans; they are reported with ctx.Err().
func (c *Collector) CollectVPCs(ctx context.Context, accounts []store.Account) ([]VPC, []ScanError) {
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		vpcs      []VPC
		errs      []ScanError
		limit     = max(c.Concurrency, 1)
		acctLimit = limit
		sem       = make(chan struct{}, limit)
	)
	if c.AccountConcurrency > 0 {
		acctLimit = min(c.AccountConcurrency, limit)
	}

	for _, acct := range accounts {
		regions := acct.Regions
		if len(regions) == 0 {
			regions = c.DefaultRegions
		}
		// API throttling is per account, so cap each account on its own too.
		acctSem := make(chan struct{}, acctLimit)
		for _, region := range regions {
			wg.Add(1)
			go func() {
				defer wg.Done()

				got, err := c.scanLimited(ctx, acctSem, sem, acct, region)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, ScanError{acct.AccountID, region, err}) // keep going
					return
				}
				vpcs = append(vpcs, got...)
			}()
		}
	}
	wg.Wait()
	return vpcs, errs
}

// scanLimited takes the account slot before the global one, so a goroutine
// waiting on a busy account doesn't hold a global slot other accounts could use.
func (c *Collector) scanLimited(ctx context.Context, acctSem, sem chan struct{}, acct store.Account, region string) ([]VPC, error) {
	if err := acquire(ctx, acctSem); err != nil {
		return nil, err
	}
	defer func() { <-acctSem }()
	if err := acquire(ctx, sem); err != nil {
		return nil, err
	}
	defer func() { <-sem }()
	return c.vpcsIn(ctx, acct, region)
}

func acquire(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	// select picks randomly when both are ready; don't start work after cancel.
	if err := ctx.Err(); err != nil {
		<-sem
		return err
	}
	return nil
}

func (c *Collector) vpcsIn(ctx context.Context, acct store.Account, region string) ([]VPC, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []VPC
	p := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Vpcs {
			tags := make(map[string]string, len(v.Tags))
			for _, t := range v.Tags {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			out = append(out, VPC{
				AccountID: acct.AccountID,
				Region:    region,
				VpcID:     aws.ToString(v.VpcId),
				CIDR:      aws.ToString(v.CidrBlock),
				IsDefault: aws.ToBool(v.IsDefault),
				State:     string(v.State),
				Name:      tags["Name"],
				Tags:      tags,
			})
		}
	}
	return out, nil
}
