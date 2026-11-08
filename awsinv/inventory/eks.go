package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// Cluster is a flattened EKS cluster with its add-ons.
type Cluster struct {
	AccountID              string
	Region                 string
	Name                   string
	ARN                    string
	Version                string // Kubernetes version, e.g. "1.31"
	PlatformVersion        string // e.g. "eks.12"
	Status                 string
	Endpoint               string
	RoleARN                string
	VpcID                  string
	SubnetIDs              []string
	SecurityGroupIDs       []string // additional, user-supplied groups
	ClusterSecurityGroupID string   // the group EKS creates
	EndpointPublicAccess   bool
	EndpointPrivateAccess  bool
	PublicAccessCIDRs      []string
	AuthenticationMode     string // "CONFIG_MAP", "API" or "API_AND_CONFIG_MAP"
	Addons                 []Addon
	CreatedAt              time.Time
	Tags                   map[string]string
	ScannedAt              time.Time // see Inventory
}

// Addon is one EKS add-on installed on a cluster.
type Addon struct {
	Name                  string
	ARN                   string
	Version               string
	Status                string
	ServiceAccountRoleARN string // IRSA role, empty if none
	Owner                 string // "aws" or "aws-marketplace"
	Publisher             string
	HealthIssues          []AddonIssue
	CreatedAt             time.Time
	ModifiedAt            time.Time
	Tags                  map[string]string
}

// AddonIssue is one health problem EKS reports for an add-on.
type AddonIssue struct {
	Code        string
	Message     string
	ResourceIDs []string
}

func (c *Collector) clustersIn(ctx context.Context, t target) ([]Cluster, error) {
	client := eks.NewFromConfig(t.cfg)

	var out []Cluster
	p := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, name := range page.Clusters {
			cl, err := c.cluster(ctx, client, t, name)
			if isNotFound(err) { // deleted since ListClusters
				continue
			}
			if err != nil {
				return nil, err
			}
			out = append(out, cl)
		}
	}
	return out, nil
}

func (c *Collector) cluster(ctx context.Context, client *eks.Client, t target, name string) (Cluster, error) {
	res, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
	if err != nil {
		return Cluster{}, err
	}
	k := res.Cluster
	if k == nil {
		return Cluster{}, &types.ResourceNotFoundException{}
	}
	cl := Cluster{
		AccountID:       t.acct.AccountID,
		Region:          t.region,
		ScannedAt:       t.scannedAt,
		Name:            aws.ToString(k.Name),
		ARN:             aws.ToString(k.Arn),
		Version:         aws.ToString(k.Version),
		PlatformVersion: aws.ToString(k.PlatformVersion),
		Status:          string(k.Status),
		Endpoint:        aws.ToString(k.Endpoint),
		RoleARN:         aws.ToString(k.RoleArn),
		CreatedAt:       aws.ToTime(k.CreatedAt),
		Tags:            nonNilTags(k.Tags),
	}
	if v := k.ResourcesVpcConfig; v != nil {
		cl.VpcID = aws.ToString(v.VpcId)
		cl.SubnetIDs = v.SubnetIds
		cl.SecurityGroupIDs = v.SecurityGroupIds
		cl.ClusterSecurityGroupID = aws.ToString(v.ClusterSecurityGroupId)
		cl.EndpointPublicAccess = v.EndpointPublicAccess
		cl.EndpointPrivateAccess = v.EndpointPrivateAccess
		cl.PublicAccessCIDRs = v.PublicAccessCidrs
	}
	if k.AccessConfig != nil {
		cl.AuthenticationMode = string(k.AccessConfig.AuthenticationMode)
	}

	ap := eks.NewListAddonsPaginator(client, &eks.ListAddonsInput{ClusterName: aws.String(name)})
	for ap.HasMorePages() {
		page, err := ap.NextPage(ctx)
		if err != nil {
			return Cluster{}, err
		}
		for _, addon := range page.Addons {
			a, err := describeAddon(ctx, client, name, addon)
			if isNotFound(err) { // removed since ListAddons
				continue
			}
			if err != nil {
				return Cluster{}, err
			}
			cl.Addons = append(cl.Addons, a)
		}
	}
	return cl, nil
}

func describeAddon(ctx context.Context, client *eks.Client, cluster, name string) (Addon, error) {
	res, err := client.DescribeAddon(ctx, &eks.DescribeAddonInput{
		ClusterName: aws.String(cluster),
		AddonName:   aws.String(name),
	})
	if err != nil {
		return Addon{}, err
	}
	a := res.Addon
	if a == nil {
		return Addon{}, &types.ResourceNotFoundException{}
	}
	out := Addon{
		Name:                  aws.ToString(a.AddonName),
		ARN:                   aws.ToString(a.AddonArn),
		Version:               aws.ToString(a.AddonVersion),
		Status:                string(a.Status),
		ServiceAccountRoleARN: aws.ToString(a.ServiceAccountRoleArn),
		Owner:                 aws.ToString(a.Owner),
		Publisher:             aws.ToString(a.Publisher),
		CreatedAt:             aws.ToTime(a.CreatedAt),
		ModifiedAt:            aws.ToTime(a.ModifiedAt),
		Tags:                  nonNilTags(a.Tags),
	}
	if a.Health != nil {
		for _, i := range a.Health.Issues {
			out.HealthIssues = append(out.HealthIssues, AddonIssue{
				Code:        string(i.Code),
				Message:     aws.ToString(i.Message),
				ResourceIDs: i.ResourceIds,
			})
		}
	}
	return out, nil
}

func isNotFound(err error) bool {
	var nf *types.ResourceNotFoundException
	return errors.As(err, &nf)
}

// nonNilTags matches tagMap: always a map, even when AWS sends none.
func nonNilTags(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}
