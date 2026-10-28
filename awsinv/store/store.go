// Package store loads the AWS accounts we're allowed to inventory.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Account is one target AWS account and the role we assume into it.
type Account struct {
	AccountID  string   // 12-digit AWS account ID
	Name       string   // human label (customer / env)
	RoleARN    string   // e.g. arn:aws:iam::123456789012:role/InventoryReadOnly
	ExternalID string   // optional; required by most MSP cross-account trust policies
	Regions    []string // regions to scan; empty => caller decides (e.g. all enabled regions)
}

// AccountStore is the seam between "where accounts live" and "how we scan them".
// Swap in a fake for tests.
type AccountStore interface {
	ListActiveAccounts(ctx context.Context) ([]Account, error)
}

// SQLStore reads accounts from a table like:
//
//	CREATE TABLE aws_accounts (
//	    account_id  CHAR(12) PRIMARY KEY,
//	    name        TEXT NOT NULL,
//	    role_arn    TEXT NOT NULL,
//	    external_id TEXT,
//	    regions     TEXT,            -- comma separated: 'ap-northeast-2,us-east-1'
//	    enabled     BOOLEAN NOT NULL DEFAULT TRUE
//	);
type SQLStore struct {
	DB *sql.DB
}

var _ AccountStore = (*SQLStore)(nil)

// ListActiveAccounts returns enabled accounts ordered by account ID.
func (s *SQLStore) ListActiveAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT account_id, name, role_arn, COALESCE(external_id, ''), COALESCE(regions, '')
		FROM aws_accounts
		WHERE enabled = TRUE
		ORDER BY account_id`)
	if err != nil {
		return nil, fmt.Errorf("query aws_accounts: %w", err)
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		var a Account
		var regions string
		if err := rows.Scan(&a.AccountID, &a.Name, &a.RoleARN, &a.ExternalID, &regions); err != nil {
			return nil, fmt.Errorf("scan aws_accounts: %w", err)
		}
		a.Regions = splitCSV(regions)
		out = append(out, a)
	}
	return out, rows.Err()
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
