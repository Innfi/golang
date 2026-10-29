package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	go cancelOnSignal(cancel)

	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	accounts, err := (&store.SQLStore{DB: db}).ListActiveAccounts(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if *only != "" {
		if accounts, err = filterAccounts(accounts, strings.Split(*only, ",")); err != nil {
			log.Fatal(err)
		}
	}

	// Hub identity: whatever the default chain resolves (instance role, SSO profile...).
	// It only needs sts:AssumeRole on the target role ARNs.
	base, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("ap-northeast-2"),
		// Adaptive mode backs off client-side when AWS starts throttling.
		// Each client gets its own bucket, i.e. one per (account, region, service).
		config.WithRetryer(func() aws.Retryer { return retry.NewAdaptiveMode() }),
	)
	if err != nil {
		log.Fatal(err)
	}

	c := &inventory.Collector{
		Creds:              awscreds.NewFactory(base, "awsinv"),
		DefaultRegions:     []string{"ap-northeast-2"},
		Concurrency:        8,
		AccountConcurrency: 2,
	}

	inv := c.Collect(ctx, accounts)
	for _, e := range inv.Errors {
		log.Printf("scan failed: %v", e)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(inv)
	log.Printf("accounts=%d vpcs=%d iam=%d failures=%d",
		len(accounts), len(inv.VPCs), len(inv.IAM), len(inv.Errors))
}

// filterAccounts keeps the requested accounts, failing on IDs that aren't
// active in the store so a typo doesn't silently scan nothing.
func filterAccounts(accounts []store.Account, ids []string) ([]store.Account, error) {
	var out []store.Account
	for _, id := range ids {
		id = strings.TrimSpace(id)
		i := slices.IndexFunc(accounts, func(a store.Account) bool { return a.AccountID == id })
		if i < 0 {
			return nil, fmt.Errorf("account %q not found or not enabled", id)
		}
		out = append(out, accounts[i])
	}
	return out, nil
}

// cancelOnSignal cancels the scan on SIGINT/SIGTERM so in-flight calls abort
// and partial results still get printed. A second signal exits immediately.
func cancelOnSignal(cancel context.CancelFunc) {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	log.Printf("received %v, cancelling scan (signal again to force exit)", s)
	cancel()
	<-sig
	log.Print("forced exit")
	os.Exit(1)
}
