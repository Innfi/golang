package inventory

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/innfi/awsinv/store"
)

// Subnet is a flattened EC2 subnet.
type Subnet struct {
	AccountID        string
	Region           string
	SubnetID         string
	VpcID            string
	CIDR             string
	AvailabilityZone string
	AvailableIPs     int32
	MapPublicIP      bool
	IsDefault        bool
	State            string
	Name             string
	Tags             map[string]string
}

func (c *Collector) subnetsIn(ctx context.Context, acct store.Account, region string) ([]Subnet, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []Subnet
	p := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range page.Subnets {
			tags := tagMap(s.Tags)
			out = append(out, Subnet{
				AccountID:        acct.AccountID,
				Region:           region,
				SubnetID:         aws.ToString(s.SubnetId),
				VpcID:            aws.ToString(s.VpcId),
				CIDR:             aws.ToString(s.CidrBlock),
				AvailabilityZone: aws.ToString(s.AvailabilityZone),
				AvailableIPs:     aws.ToInt32(s.AvailableIpAddressCount),
				MapPublicIP:      aws.ToBool(s.MapPublicIpOnLaunch),
				IsDefault:        aws.ToBool(s.DefaultForAz),
				State:            string(s.State),
				Name:             tags["Name"],
				Tags:             tags,
			})
		}
	}
	return out, nil
}
