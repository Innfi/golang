package inventory

import (
	"cmp"
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/innfi/awsinv/store"
)

// RouteTable is a flattened VPC route table.
type RouteTable struct {
	AccountID    string
	Region       string
	RouteTableID string
	VpcID        string
	IsMain       bool     // the VPC's main table; applies to subnets with no explicit association
	SubnetIDs    []string // explicit associations only
	GatewayIDs   []string // edge associations (ingress routing)
	Routes       []Route
	Name         string
	Tags         map[string]string
}

// Route is one entry of a route table.
type Route struct {
	Destination string // IPv4 CIDR, IPv6 CIDR or prefix list ID
	Target      string // gateway, NAT, instance, ... ID; "local" for the VPC's own range
	State       string // "active" or "blackhole"
	Origin      string
}

func (c *Collector) routeTablesIn(ctx context.Context, acct store.Account, region string) ([]RouteTable, error) {
	client := ec2.NewFromConfig(c.Creds.ConfigFor(acct, region))

	var out []RouteTable
	p := ec2.NewDescribeRouteTablesPaginator(client, &ec2.DescribeRouteTablesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, rt := range page.RouteTables {
			tags := tagMap(rt.Tags)
			table := RouteTable{
				AccountID:    acct.AccountID,
				Region:       region,
				RouteTableID: aws.ToString(rt.RouteTableId),
				VpcID:        aws.ToString(rt.VpcId),
				Name:         tags["Name"],
				Tags:         tags,
			}
			for _, a := range rt.Associations {
				table.IsMain = table.IsMain || aws.ToBool(a.Main)
				if a.SubnetId != nil {
					table.SubnetIDs = append(table.SubnetIDs, *a.SubnetId)
				}
				if a.GatewayId != nil {
					table.GatewayIDs = append(table.GatewayIDs, *a.GatewayId)
				}
			}
			for _, r := range rt.Routes {
				table.Routes = append(table.Routes, flattenRoute(r))
			}
			out = append(out, table)
		}
	}
	return out, nil
}

// A route has one destination and one target, each in its own SDK field.
func flattenRoute(r types.Route) Route {
	return Route{
		Destination: cmp.Or(
			aws.ToString(r.DestinationCidrBlock),
			aws.ToString(r.DestinationIpv6CidrBlock),
			aws.ToString(r.DestinationPrefixListId),
		),
		Target: cmp.Or(
			aws.ToString(r.GatewayId),
			aws.ToString(r.NatGatewayId),
			aws.ToString(r.TransitGatewayId),
			aws.ToString(r.VpcPeeringConnectionId),
			aws.ToString(r.EgressOnlyInternetGatewayId),
			aws.ToString(r.InstanceId), // before the ENI: instance routes set both
			aws.ToString(r.NetworkInterfaceId),
			aws.ToString(r.LocalGatewayId),
			aws.ToString(r.CarrierGatewayId),
			aws.ToString(r.CoreNetworkArn),
			aws.ToString(r.OdbNetworkArn),
			aws.ToString(r.IpAddress),
		),
		State:  string(r.State),
		Origin: string(r.Origin),
	}
}
