package inventory

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

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
			tags := tagMap(v.Tags)
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

func tagMap(in []types.Tag) map[string]string {
	tags := make(map[string]string, len(in))
	for _, t := range in {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return tags
}
