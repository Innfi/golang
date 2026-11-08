// Package inventory fetches resources across accounts/regions.
package inventory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/store"
)

// Inventory is everything one Collect run found.
//
// It is not a point-in-time snapshot: AWS has no cross-API snapshot, so each
// resource type is read at a slightly different moment. All types of one
// (account, region) are scanned back-to-back to keep that gap small, and every
// item carries ScannedAt, the start of its own type's scan. References between
// items (volume -> instance, route -> NAT gateway, ...) can still dangle when a
// resource was created or deleted in between; treat them as soft.
type Inventory struct {
	VPCs                       []VPC
	Subnets                    []Subnet
	RouteTables                []RouteTable
	NATGateways                []NATGateway
	InternetGateways           []InternetGateway
	EgressOnlyInternetGateways []EgressOnlyInternetGateway
	TransitGateways            []TransitGateway
	Instances                  []Instance
	Volumes                    []Volume
	Clusters                   []Cluster
	IAM                        []IAMPrincipal
	Errors                     []ScanError `json:"-"`
}

// ScanError records a per-target failure without aborting the whole run.
type ScanError struct {
	Resource  string // "vpc", "subnet", "routetable", "natgateway", "ec2", "ebs", "eks", "iam", ...
	AccountID string
	Region    string
	Err       error
}

func (e ScanError) Error() string {
	return fmt.Sprintf("%s %s/%s: %v", e.Resource, e.AccountID, e.Region, e.Err)
}

// Collector scans accounts/regions with bounded global and per-account concurrency.
// The unit of concurrency is a target: one (account, region) pair, or one
// account's global (IAM) scan. A target runs its resource types in sequence.
type Collector struct {
	Creds              *awscreds.Factory
	DefaultRegions     []string // used when an account row has no regions
	Concurrency        int      // max parallel targets
	AccountConcurrency int      // max parallel targets per account; 0 => Concurrency

	now func() time.Time // for tests; nil => time.Now
}

// target is one (account, region) pair being scanned. region is globalRegion
// for an account's global scan, and ec2 is nil there.
type target struct {
	acct      store.Account
	region    string
	cfg       aws.Config
	ec2       *ec2.Client
	scannedAt time.Time // start of the current resource type's scan
}

// scanner reads one resource type of a target. commit stores the result and
// runs with the Inventory lock held.
type scanner struct {
	resource string
	scan     func(context.Context, target) (commit func(), err error)
}

// into adapts a typed scan func that appends to dst.
func into[T any](resource string, dst *[]T, scan func(context.Context, target) ([]T, error)) scanner {
	return scanner{resource, func(ctx context.Context, t target) (func(), error) {
		got, err := scan(ctx, t)
		if err != nil {
			return nil, err
		}
		return func() { *dst = append(*dst, got...) }, nil
	}}
}

// Collect fetches every resource type for the accounts. One bad account
// (revoked role, SCP deny, opt-in region disabled) is reported, not fatal.
// Cancelling ctx stops queued and unstarted scans; they are reported with
// ctx.Err().
func (c *Collector) Collect(ctx context.Context, accounts []store.Account) Inventory {
	var inv Inventory
	// New resource types: add a field to Inventory and a line here.
	regional := []scanner{
		into("vpc", &inv.VPCs, c.vpcsIn),
		into("subnet", &inv.Subnets, c.subnetsIn),
		into("routetable", &inv.RouteTables, c.routeTablesIn),
		into("natgateway", &inv.NATGateways, c.natGatewaysIn),
		into("internetgateway", &inv.InternetGateways, c.internetGatewaysIn),
		into("egressonlyinternetgateway", &inv.EgressOnlyInternetGateways, c.egressOnlyInternetGatewaysIn),
		into("transitgateway", &inv.TransitGateways, c.transitGatewaysIn),
		into("ec2", &inv.Instances, c.instancesIn),
		into("ebs", &inv.Volumes, c.volumesIn),
		into("eks", &inv.Clusters, c.clustersIn),
	}
	global := []scanner{
		into("iam", &inv.IAM, c.iamIn),
	}

	var (
		l  = c.newLimiter()
		mu sync.Mutex // guards inv
		wg sync.WaitGroup
	)
	for _, acct := range accounts {
		for _, region := range c.regionsFor(acct) {
			wg.Go(func() { c.scanTarget(ctx, l, &mu, &inv, acct, region, regional) })
		}
		wg.Go(func() { c.scanTarget(ctx, l, &mu, &inv, acct, globalRegion, global) })
	}
	wg.Wait()
	return inv
}

// scanTarget holds one limiter slot for the whole target, so its resource
// types run back-to-back instead of interleaving with other targets.
func (c *Collector) scanTarget(
	ctx context.Context, l *limiter, mu *sync.Mutex, inv *Inventory,
	acct store.Account, region string, scans []scanner,
) {
	fail := func(resource string, err error) {
		mu.Lock()
		defer mu.Unlock()
		inv.Errors = append(inv.Errors, ScanError{resource, acct.AccountID, region, err}) // keep going
	}

	release, err := l.acquire(ctx, acct.AccountID)
	if err != nil {
		for _, s := range scans {
			fail(s.resource, err)
		}
		return
	}
	defer release()

	t := target{acct: acct, region: region}
	if region == globalRegion {
		t.cfg = c.Creds.ConfigFor(acct, c.globalEndpointRegion(acct))
	} else {
		t.cfg = c.Creds.ConfigFor(acct, region)
		t.ec2 = ec2.NewFromConfig(t.cfg)
	}
	for _, s := range scans {
		if err := ctx.Err(); err != nil { // don't start the rest after cancel
			fail(s.resource, err)
			continue
		}
		t.scannedAt = c.clock()
		commit, err := s.scan(ctx, t)
		if err != nil {
			fail(s.resource, err)
			continue
		}
		mu.Lock()
		commit()
		mu.Unlock()
	}
}

func (c *Collector) regionsFor(acct store.Account) []string {
	if len(acct.Regions) == 0 {
		return c.DefaultRegions
	}
	return acct.Regions
}

// globalEndpointRegion picks the region used to reach a global service. Any
// region in the account's partition resolves to the global IAM endpoint.
func (c *Collector) globalEndpointRegion(acct store.Account) string {
	if r := c.regionsFor(acct); len(r) > 0 {
		return r[0]
	}
	return "us-east-1"
}

func (c *Collector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

const globalRegion = "global"

// limiter holds the global semaphore and one semaphore per account.
type limiter struct {
	global    chan struct{}
	acctLimit int

	mu   sync.Mutex
	acct map[string]chan struct{}
}

func (c *Collector) newLimiter() *limiter {
	limit := max(c.Concurrency, 1)
	acctLimit := limit
	if c.AccountConcurrency > 0 {
		acctLimit = min(c.AccountConcurrency, limit)
	}
	return &limiter{
		global:    make(chan struct{}, limit),
		acctLimit: acctLimit,
		acct:      map[string]chan struct{}{},
	}
}

// API throttling is per account, so cap each account on its own too.
func (l *limiter) forAccount(id string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.acct[id]
	if !ok {
		s = make(chan struct{}, l.acctLimit)
		l.acct[id] = s
	}
	return s
}

// acquire takes the account slot before the global one, so a goroutine
// waiting on a busy account doesn't hold a global slot other accounts could use.
func (l *limiter) acquire(ctx context.Context, accountID string) (release func(), err error) {
	acctSem := l.forAccount(accountID)
	if err := acquire(ctx, acctSem); err != nil {
		return nil, err
	}
	if err := acquire(ctx, l.global); err != nil {
		<-acctSem
		return nil, err
	}
	return func() { <-l.global; <-acctSem }, nil
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
