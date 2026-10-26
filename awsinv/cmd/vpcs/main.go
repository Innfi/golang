package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/inventory"
	"github.com/innfi/awsinv/store"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

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
	base, err := config.LoadDefaultConfig(ctx, config.WithRegion("ap-northeast-2"))
	if err != nil {
		log.Fatal(err)
	}

	c := &inventory.Collector{
		Creds:          awscreds.NewFactory(base, "inventory-vpc"),
		DefaultRegions: []string{"ap-northeast-2"},
		Concurrency:    8,
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
