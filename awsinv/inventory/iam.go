package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/innfi/awsinv/store"
)

// IAMPrincipal is a flattened IAM user or role. IAM is global, so each
// account is scanned once and failures report Region "global".
type IAMPrincipal struct {
	AccountID  string
	Kind       string // "user" or "role"
	Name       string
	ID         string // AIDA... / AROA...
	ARN        string
	Path       string
	CreateDate time.Time
}

func (c *Collector) iamIn(ctx context.Context, acct store.Account, _ string) ([]IAMPrincipal, error) {
	// Any region in the account's partition resolves to the global IAM endpoint.
	region := "us-east-1"
	if r := c.regionsFor(acct); len(r) > 0 {
		region = r[0]
	}
	client := iam.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []IAMPrincipal
	users := iam.NewListUsersPaginator(client, &iam.ListUsersInput{})
	for users.HasMorePages() {
		page, err := users.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range page.Users {
			out = append(out, IAMPrincipal{
				AccountID:  acct.AccountID,
				Kind:       "user",
				Name:       aws.ToString(u.UserName),
				ID:         aws.ToString(u.UserId),
				ARN:        aws.ToString(u.Arn),
				Path:       aws.ToString(u.Path),
				CreateDate: aws.ToTime(u.CreateDate),
			})
		}
	}

	roles := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for roles.HasMorePages() {
		page, err := roles.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Roles {
			out = append(out, IAMPrincipal{
				AccountID:  acct.AccountID,
				Kind:       "role",
				Name:       aws.ToString(r.RoleName),
				ID:         aws.ToString(r.RoleId),
				ARN:        aws.ToString(r.Arn),
				Path:       aws.ToString(r.Path),
				CreateDate: aws.ToTime(r.CreateDate),
			})
		}
	}
	return out, nil
}
