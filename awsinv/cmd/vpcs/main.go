package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/inventory"
	"github.com/innfi/awsinv/store"
)

func main() {
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

	// Hub identity: whatever the default chain resolves (instance role, SSO profile...).
	// It only needs sts:AssumeRole on the target role ARNs.
	base, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("ap-northeast-2"),
		// Adaptive mode backs off client-side when AWS starts throttling.
		// Each client gets its own bucket, i.e. one per (account, region).
		config.WithRetryer(func() aws.Retryer { return retry.NewAdaptiveMode() }),
	)
	if err != nil {
		log.Fatal(err)
	}

	c := &inventory.Collector{
		Creds:              awscreds.NewFactory(base, "inventory-vpc"),
		DefaultRegions:     []string{"ap-northeast-2"},
		Concurrency:        8,
		AccountConcurrency: 2,
	}

	vpcs, scanErrs := c.CollectVPCs(ctx, accounts)
	for _, e := range scanErrs {
		log.Printf("scan failed: %v", e)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(vpcs)
	log.Printf("accounts=%d vpcs=%d failures=%d", len(accounts), len(vpcs), len(scanErrs))
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
