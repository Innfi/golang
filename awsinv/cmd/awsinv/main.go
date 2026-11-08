package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver for database/sql

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/inventory"
	"github.com/innfi/awsinv/store"
)

func main() {
	only := flag.String("account", "", "comma-separated account IDs to scan (default: all active accounts)")
	socket := flag.String("serve", "", "run as a long-lived server on this unix socket path instead of scanning once")
	stdio := flag.Bool("stdio", false, "run as a long-lived server reading requests from stdin and writing responses to stdout")
	flag.Parse()

	if *socket != "" && *stdio {
		log.Fatal("-serve and -stdio are mutually exclusive")
	}
	if (*socket != "" || *stdio) && *only != "" {
		log.Fatal("-account cannot be combined with -serve or -stdio; pass accounts per request")
	}
	switch {
	case *socket != "":
		runServer(*socket)
		return
	case *stdio:
		runStdio()
		return
	}
	runOnce(*only)
}

// runOnce scans once, prints the inventory as JSON to stdout and exits.
func runOnce(only string) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	go cancelOnSignal(cancel)

	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var ids []string
	if only != "" {
		ids = strings.Split(only, ",")
	}
	accounts, err := loadAccounts(ctx, &store.SQLStore{DB: db}, ids)
	if err != nil {
		log.Fatal(err)
	}

	c, err := newCollector(ctx)
	if err != nil {
		log.Fatal(err)
	}

	inv := c.Collect(ctx, accounts)
	for _, e := range inv.Errors {
		log.Printf("scan failed: %v", e)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(inv)
	logSummary(len(accounts), inv)
}

const scanTimeout = 5 * time.Minute

// loadAccounts returns active accounts, narrowed to ids when non-empty.
func loadAccounts(ctx context.Context, s store.AccountStore, ids []string) ([]store.Account, error) {
	accounts, err := s.ListActiveAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return accounts, nil
	}
	return filterAccounts(accounts, ids)
}

func newCollector(ctx context.Context) (*inventory.Collector, error) {
	// Hub identity: whatever the default chain resolves (instance role, SSO profile...).
	// It only needs sts:AssumeRole on the target role ARNs.
	base, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("ap-northeast-2"),
		// Adaptive mode backs off client-side when AWS starts throttling.
		// Each client gets its own bucket, i.e. one per (account, region, service).
		config.WithRetryer(func() aws.Retryer { return retry.NewAdaptiveMode() }),
	)
	if err != nil {
		return nil, err
	}
	return &inventory.Collector{
		Creds:              awscreds.NewFactory(base, "awsinv"),
		DefaultRegions:     []string{"ap-northeast-2"},
		Concurrency:        8,
		AccountConcurrency: 2,
	}, nil
}

func logSummary(accounts int, inv inventory.Inventory) {
	log.Printf("accounts=%d vpcs=%d subnets=%d routetables=%d natgateways=%d internetgateways=%d egressonlyinternetgateways=%d transitgateways=%d instances=%d volumes=%d clusters=%d iam=%d failures=%d",
		accounts, len(inv.VPCs), len(inv.Subnets), len(inv.RouteTables), len(inv.NATGateways), len(inv.InternetGateways),
		len(inv.EgressOnlyInternetGateways), len(inv.TransitGateways), len(inv.Instances), len(inv.Volumes), len(inv.Clusters), len(inv.IAM), len(inv.Errors))
}

var errUnknownAccount = errors.New("account not found or not enabled")

// filterAccounts keeps the requested accounts, failing on IDs that aren't
// active in the store so a typo doesn't silently scan nothing.
func filterAccounts(accounts []store.Account, ids []string) ([]store.Account, error) {
	var out []store.Account
	for _, id := range ids {
		id = strings.TrimSpace(id)
		i := slices.IndexFunc(accounts, func(a store.Account) bool { return a.AccountID == id })
		if i < 0 {
			return nil, fmt.Errorf("account %q: %w", id, errUnknownAccount)
		}
		out = append(out, accounts[i])
	}
	return out, nil
}

// cancelOnSignal cancels ctx on SIGINT/SIGTERM so in-flight calls abort and
// partial results still get delivered. A second signal exits immediately.
func cancelOnSignal(cancel context.CancelFunc) {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	log.Printf("received %v, shutting down (signal again to force exit)", s)
	cancel()
	<-sig
	log.Print("forced exit")
	os.Exit(1)
}
