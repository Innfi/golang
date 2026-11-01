package inventory

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/innfi/awsinv/store"
)

// NATGateway is a flattened NAT gateway. AWS keeps listing deleted ones for a
// while, so check State before treating one as in service.
type NATGateway struct {
	AccountID        string
	Region           string
	NATGatewayID     string
	VpcID            string
	SubnetID         string // empty for a regional NAT gateway
	ConnectivityType string // "public" or "private"
	AvailabilityMode string // "zonal" or "regional"
	State            string
	Addresses        []NATGatewayAddress
	Name             string
	Tags             map[string]string
}

// NATGatewayAddress is one IP address of a NAT gateway.
type NATGatewayAddress struct {
	AllocationID       string // empty for a private NAT gateway
	NetworkInterfaceID string
	PrivateIP          string
	PublicIP           string // empty for a private NAT gateway
	AvailabilityZone   string // set for a regional NAT gateway
	IsPrimary          bool
	Status             string
}

// InternetGateway is a flattened internet gateway. It has no state of its own;
// one with no Attachments is detached.
type InternetGateway struct {
	AccountID         string
	Region            string
	InternetGatewayID string
	OwnerID           string
	Attachments       []GatewayAttachment
	Name              string
	Tags              map[string]string
}

// EgressOnlyInternetGateway is a flattened egress-only internet gateway.
type EgressOnlyInternetGateway struct {
	AccountID                   string
	Region                      string
	EgressOnlyInternetGatewayID string
	Attachments                 []GatewayAttachment
	Name                        string
	Tags                        map[string]string
}

// GatewayAttachment links an internet or egress-only internet gateway to a VPC.
type GatewayAttachment struct {
	VpcID string
	State string
}

// TransitGateway is a flattened transit gateway. A gateway shared through RAM
// is listed by its owner and by each account it is shared with: AccountID is
// the account scanned, OwnerID the one that owns it. AWS keeps listing deleted
// ones for a while, so check State before treating one as in service.
type TransitGateway struct {
	AccountID        string
	Region           string
	TransitGatewayID string
	ARN              string
	OwnerID          string
	AmazonSideASN    int64
	Description      string
	State            string
	Name             string
	Tags             map[string]string
}

func (c *Collector) natGatewaysIn(ctx context.Context, acct store.Account, region string) ([]NATGateway, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []NATGateway
	p := ec2.NewDescribeNatGatewaysPaginator(client, &ec2.DescribeNatGatewaysInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range page.NatGateways {
			tags := tagMap(n.Tags)
			gw := NATGateway{
				AccountID:        acct.AccountID,
				Region:           region,
				NATGatewayID:     aws.ToString(n.NatGatewayId),
				VpcID:            aws.ToString(n.VpcId),
				SubnetID:         aws.ToString(n.SubnetId),
				ConnectivityType: string(n.ConnectivityType),
				AvailabilityMode: string(n.AvailabilityMode),
				State:            string(n.State),
				Name:             tags["Name"],
				Tags:             tags,
			}
			for _, a := range n.NatGatewayAddresses {
				gw.Addresses = append(gw.Addresses, NATGatewayAddress{
					AllocationID:       aws.ToString(a.AllocationId),
					NetworkInterfaceID: aws.ToString(a.NetworkInterfaceId),
					PrivateIP:          aws.ToString(a.PrivateIp),
					PublicIP:           aws.ToString(a.PublicIp),
					AvailabilityZone:   aws.ToString(a.AvailabilityZone),
					IsPrimary:          aws.ToBool(a.IsPrimary),
					Status:             string(a.Status),
				})
			}
			out = append(out, gw)
		}
	}
	return out, nil
}

func (c *Collector) internetGatewaysIn(ctx context.Context, acct store.Account, region string) ([]InternetGateway, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []InternetGateway
	p := ec2.NewDescribeInternetGatewaysPaginator(client, &ec2.DescribeInternetGatewaysInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range page.InternetGateways {
			tags := tagMap(g.Tags)
			out = append(out, InternetGateway{
				AccountID:         acct.AccountID,
				Region:            region,
				InternetGatewayID: aws.ToString(g.InternetGatewayId),
				OwnerID:           aws.ToString(g.OwnerId),
				Attachments:       flattenAttachments(g.Attachments),
				Name:              tags["Name"],
				Tags:              tags,
			})
		}
	}
	return out, nil
}

func (c *Collector) egressOnlyInternetGatewaysIn(ctx context.Context, acct store.Account, region string) ([]EgressOnlyInternetGateway, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []EgressOnlyInternetGateway
	p := ec2.NewDescribeEgressOnlyInternetGatewaysPaginator(client, &ec2.DescribeEgressOnlyInternetGatewaysInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range page.EgressOnlyInternetGateways {
			tags := tagMap(g.Tags)
			out = append(out, EgressOnlyInternetGateway{
				AccountID:                   acct.AccountID,
				Region:                      region,
				EgressOnlyInternetGatewayID: aws.ToString(g.EgressOnlyInternetGatewayId),
				Attachments:                 flattenAttachments(g.Attachments),
				Name:                        tags["Name"],
				Tags:                        tags,
			})
		}
	}
	return out, nil
}

func (c *Collector) transitGatewaysIn(ctx context.Context, acct store.Account, region string) ([]TransitGateway, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []TransitGateway
	p := ec2.NewDescribeTransitGatewaysPaginator(client, &ec2.DescribeTransitGatewaysInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range page.TransitGateways {
			tags := tagMap(g.Tags)
			gw := TransitGateway{
				AccountID:        acct.AccountID,
				Region:           region,
				TransitGatewayID: aws.ToString(g.TransitGatewayId),
				ARN:              aws.ToString(g.TransitGatewayArn),
				OwnerID:          aws.ToString(g.OwnerId),
				Description:      aws.ToString(g.Description),
				State:            string(g.State),
				Name:             tags["Name"],
				Tags:             tags,
			}
			if g.Options != nil {
				gw.AmazonSideASN = aws.ToInt64(g.Options.AmazonSideAsn)
			}
			out = append(out, gw)
		}
	}
	return out, nil
}

func flattenAttachments(in []types.InternetGatewayAttachment) []GatewayAttachment {
	var out []GatewayAttachment
	for _, a := range in {
		out = append(out, GatewayAttachment{VpcID: aws.ToString(a.VpcId), State: string(a.State)})
	}
	return out
}
