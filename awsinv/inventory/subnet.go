package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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
	ScannedAt        time.Time // see Inventory
}

func (c *Collector) subnetsIn(ctx context.Context, t target) ([]Subnet, error) {
	acct, region, client := t.acct, t.region, t.ec2

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
				ScannedAt:        t.scannedAt,
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
