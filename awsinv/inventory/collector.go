// Package inventory fetches resources across accounts/regions.
package inventory

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/store"
)

// Inventory is everything one Collect run found.
type Inventory struct {
	VPCs        []VPC
	Subnets     []Subnet
	RouteTables []RouteTable
	IAM         []IAMPrincipal
	Errors      []ScanError `json:"-"`
}

// ScanError records a per-target failure without aborting the whole run.
type ScanError struct {
	Resource  string // "vpc", "subnet", "routetable", "iam", ...
	AccountID string
	Region    string
	Err       error
}

func (e ScanError) Error() string {
	return fmt.Sprintf("%s %s/%s: %v", e.Resource, e.AccountID, e.Region, e.Err)
}

// Collector scans accounts/regions with bounded global and per-account concurrency.
type Collector struct {
	Creds              *awscreds.Factory
	DefaultRegions     []string // used when an account row has no regions
	Concurrency        int      // max parallel scans
	AccountConcurrency int      // max parallel scans per account; 0 => Concurrency
}

// Collect fetches every resource type for the accounts. All types share one
// set of limits, so AccountConcurrency caps an account across VPC+IAM+...
// One bad account (revoked role, SCP deny, opt-in region disabled) is
// reported, not fatal. Cancelling ctx stops queued scans; they are reported
// with ctx.Err().
func (c *Collector) Collect(ctx context.Context, accounts []store.Account) Inventory {
	l := c.newLimiter()
	var (
		inv                                  Inventory
		wg                                   sync.WaitGroup
		vpcErrs, subnetErrs, rtErrs, iamErrs []ScanError
	)
	// New resource types: add a field to Inventory and a line here.
	wg.Go(func() { inv.VPCs, vpcErrs = collect(ctx, l, "vpc", accounts, c.regionsFor, c.vpcsIn) })
	wg.Go(func() { inv.Subnets, subnetErrs = collect(ctx, l, "subnet", accounts, c.regionsFor, c.subnetsIn) })
	wg.Go(func() {
		inv.RouteTables, rtErrs = collect(ctx, l, "routetable", accounts, c.regionsFor, c.routeTablesIn)
	})
	wg.Go(func() { inv.IAM, iamErrs = collect(ctx, l, "iam", accounts, global, c.iamIn) })
	wg.Wait()
	inv.Errors = slices.Concat(vpcErrs, subnetErrs, rtErrs, iamErrs)
	return inv
}

func (c *Collector) regionsFor(acct store.Account) []string {
	if len(acct.Regions) == 0 {
		return c.DefaultRegions
	}
	return acct.Regions
}

// global targets for resources that are scanned once per account.
func global(store.Account) []string { return []string{globalRegion} }

const globalRegion = "global"

// collect runs scan once per (account, target) pair, where targets lists the
// regions (or "global") to scan for an account.
func collect[T any](
	ctx context.Context,
	l *limiter,
	resource string,
	accounts []store.Account,
	targets func(store.Account) []string,
	scan func(context.Context, store.Account, string) ([]T, error),
) ([]T, []ScanError) {
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		items []T
		errs  []ScanError
	)
	for _, acct := range accounts {
		for _, region := range targets(acct) {
			wg.Go(func() {
				got, err := scanLimited(ctx, l, acct.AccountID, func(ctx context.Context) ([]T, error) {
					return scan(ctx, acct, region)
				})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, ScanError{resource, acct.AccountID, region, err}) // keep going
					return
				}
				items = append(items, got...)
			})
		}
	}
	wg.Wait()
	return items, errs
}

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

// scanLimited takes the account slot before the global one, so a goroutine
// waiting on a busy account doesn't hold a global slot other accounts could use.
func scanLimited[T any](ctx context.Context, l *limiter, accountID string, scan func(context.Context) ([]T, error)) ([]T, error) {
	acctSem := l.forAccount(accountID)
	if err := acquire(ctx, acctSem); err != nil {
		return nil, err
	}
	defer func() { <-acctSem }()
	if err := acquire(ctx, l.global); err != nil {
		return nil, err
	}
	defer func() { <-l.global }()
	return scan(ctx)
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
