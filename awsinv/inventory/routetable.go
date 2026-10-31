package inventory

import (
	"cmp"
	"context"
	"slices"

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
	IsMain       bool          // the VPC's main table; applies to subnets with no explicit association
	Associations []Association // every state; check State before treating one as in effect
	Routes       []Route
	Name         string
	Tags         map[string]string
}

// Association links a route table to the main slot, a subnet or a gateway.
type Association struct {
	AssociationID string
	SubnetID      string // explicit subnet association
	GatewayID     string // edge association (ingress routing)
	IsMain        bool
	State         string // "associating", "associated", "disassociating", "disassociated" or "failed"
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
				table.Associations = append(table.Associations, flattenAssociation(a))
			}
			table.IsMain = hasMainAssociation(table.Associations)
			for _, r := range rt.Routes {
				table.Routes = append(table.Routes, flattenRoute(r))
			}
			out = append(out, table)
		}
	}
	return out, nil
}

func flattenAssociation(a types.RouteTableAssociation) Association {
	out := Association{
		AssociationID: aws.ToString(a.RouteTableAssociationId),
		SubnetID:      aws.ToString(a.SubnetId),
		GatewayID:     aws.ToString(a.GatewayId),
		IsMain:        aws.ToBool(a.Main),
	}
	if a.AssociationState != nil {
		out.State = string(a.AssociationState.State)
	}
	return out
}

// A main association on its way in or out does not make the table main.
func hasMainAssociation(assocs []Association) bool {
	return slices.ContainsFunc(assocs, func(a Association) bool {
		return a.IsMain && a.State == string(types.RouteTableAssociationStateCodeAssociated)
	})
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
