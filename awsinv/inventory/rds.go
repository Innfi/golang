package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// DBInstance is a flattened RDS DB instance. The RDS API also returns Neptune
// and DocumentDB instances; Engine tells them apart. An Aurora instance has
// ClusterID set and its storage fields are empty: they live on the DBCluster.
type DBInstance struct {
	AccountID          string
	Region             string
	DBInstanceID       string
	ARN                string
	ResourceID         string // dbi-..., stable across renames
	Class              string // e.g. "db.r6g.large"
	Engine             string
	EngineVersion      string
	Status             string
	ClusterID          string // empty unless part of a DB cluster
	EndpointAddress    string // empty while creating
	EndpointPort       int32
	AvailabilityZone   string
	SecondaryAZ        string // standby AZ for Multi-AZ
	MultiAZ            bool
	PubliclyAccessible bool
	VpcID              string
	SubnetGroup        string
	SubnetIDs          []string
	SecurityGroupIDs   []string
	StorageType        string
	AllocatedGiB       int32
	MaxAllocatedGiB    int32 // storage autoscaling ceiling, 0 if off
	IOPS               int32
	StorageEncrypted   bool
	KMSKeyID           string
	BackupRetention    int32 // days; 0 disables automated backups
	DeletionProtection bool
	ReplicaSourceID    string // set on a read replica
	CreateTime         time.Time
	Tags               map[string]string
	ScannedAt          time.Time // see Inventory
}

// DBCluster is a flattened RDS DB cluster (Aurora or Multi-AZ DB cluster).
type DBCluster struct {
	AccountID          string
	Region             string
	DBClusterID        string
	ARN                string
	ResourceID         string // cluster-..., stable across renames
	Engine             string
	EngineVersion      string
	EngineMode         string // "provisioned", "serverless", ...
	Status             string
	Endpoint           string // writer
	ReaderEndpoint     string
	Port               int32
	MultiAZ            bool
	AvailabilityZones  []string
	SubnetGroup        string
	SecurityGroupIDs   []string
	Members            []DBClusterMember
	StorageType        string
	StorageEncrypted   bool
	KMSKeyID           string
	BackupRetention    int32 // days
	DeletionProtection bool
	GlobalClusterID    string
	ServerlessV2MinACU float64 // 0 when not Serverless v2
	ServerlessV2MaxACU float64
	CreateTime         time.Time
	Tags               map[string]string
	ScannedAt          time.Time // see Inventory
}

// DBClusterMember is one DB instance of a cluster.
type DBClusterMember struct {
	DBInstanceID string
	IsWriter     bool
}

func (c *Collector) dbInstancesIn(ctx context.Context, t target) ([]DBInstance, error) {
	client := rds.NewFromConfig(t.cfg)

	var out []DBInstance
	p := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range page.DBInstances {
			inst := DBInstance{
				AccountID:          t.acct.AccountID,
				Region:             t.region,
				ScannedAt:          t.scannedAt,
				DBInstanceID:       aws.ToString(d.DBInstanceIdentifier),
				ARN:                aws.ToString(d.DBInstanceArn),
				ResourceID:         aws.ToString(d.DbiResourceId),
				Class:              aws.ToString(d.DBInstanceClass),
				Engine:             aws.ToString(d.Engine),
				EngineVersion:      aws.ToString(d.EngineVersion),
				Status:             aws.ToString(d.DBInstanceStatus),
				ClusterID:          aws.ToString(d.DBClusterIdentifier),
				AvailabilityZone:   aws.ToString(d.AvailabilityZone),
				SecondaryAZ:        aws.ToString(d.SecondaryAvailabilityZone),
				MultiAZ:            aws.ToBool(d.MultiAZ),
				PubliclyAccessible: aws.ToBool(d.PubliclyAccessible),
				StorageType:        aws.ToString(d.StorageType),
				AllocatedGiB:       aws.ToInt32(d.AllocatedStorage),
				MaxAllocatedGiB:    aws.ToInt32(d.MaxAllocatedStorage),
				IOPS:               aws.ToInt32(d.Iops),
				StorageEncrypted:   aws.ToBool(d.StorageEncrypted),
				KMSKeyID:           aws.ToString(d.KmsKeyId),
				BackupRetention:    aws.ToInt32(d.BackupRetentionPeriod),
				DeletionProtection: aws.ToBool(d.DeletionProtection),
				ReplicaSourceID:    aws.ToString(d.ReadReplicaSourceDBInstanceIdentifier),
				CreateTime:         aws.ToTime(d.InstanceCreateTime),
				Tags:               rdsTags(d.TagList),
			}
			if d.Endpoint != nil {
				inst.EndpointAddress = aws.ToString(d.Endpoint.Address)
				inst.EndpointPort = aws.ToInt32(d.Endpoint.Port)
			}
			if g := d.DBSubnetGroup; g != nil {
				inst.VpcID = aws.ToString(g.VpcId)
				inst.SubnetGroup = aws.ToString(g.DBSubnetGroupName)
				for _, s := range g.Subnets {
					inst.SubnetIDs = append(inst.SubnetIDs, aws.ToString(s.SubnetIdentifier))
				}
			}
			inst.SecurityGroupIDs = vpcSecurityGroupIDs(d.VpcSecurityGroups)
			out = append(out, inst)
		}
	}
	return out, nil
}

func (c *Collector) dbClustersIn(ctx context.Context, t target) ([]DBCluster, error) {
	client := rds.NewFromConfig(t.cfg)

	var out []DBCluster
	p := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range page.DBClusters {
			cl := DBCluster{
				AccountID:          t.acct.AccountID,
				Region:             t.region,
				ScannedAt:          t.scannedAt,
				DBClusterID:        aws.ToString(d.DBClusterIdentifier),
				ARN:                aws.ToString(d.DBClusterArn),
				ResourceID:         aws.ToString(d.DbClusterResourceId),
				Engine:             aws.ToString(d.Engine),
				EngineVersion:      aws.ToString(d.EngineVersion),
				EngineMode:         aws.ToString(d.EngineMode),
				Status:             aws.ToString(d.Status),
				Endpoint:           aws.ToString(d.Endpoint),
				ReaderEndpoint:     aws.ToString(d.ReaderEndpoint),
				Port:               aws.ToInt32(d.Port),
				MultiAZ:            aws.ToBool(d.MultiAZ),
				AvailabilityZones:  d.AvailabilityZones,
				SubnetGroup:        aws.ToString(d.DBSubnetGroup),
				SecurityGroupIDs:   vpcSecurityGroupIDs(d.VpcSecurityGroups),
				StorageType:        aws.ToString(d.StorageType),
				StorageEncrypted:   aws.ToBool(d.StorageEncrypted),
				KMSKeyID:           aws.ToString(d.KmsKeyId),
				BackupRetention:    aws.ToInt32(d.BackupRetentionPeriod),
				DeletionProtection: aws.ToBool(d.DeletionProtection),
				GlobalClusterID:    aws.ToString(d.GlobalClusterIdentifier),
				CreateTime:         aws.ToTime(d.ClusterCreateTime),
				Tags:               rdsTags(d.TagList),
			}
			if s := d.ServerlessV2ScalingConfiguration; s != nil {
				cl.ServerlessV2MinACU = aws.ToFloat64(s.MinCapacity)
				cl.ServerlessV2MaxACU = aws.ToFloat64(s.MaxCapacity)
			}
			for _, m := range d.DBClusterMembers {
				cl.Members = append(cl.Members, DBClusterMember{
					DBInstanceID: aws.ToString(m.DBInstanceIdentifier),
					IsWriter:     aws.ToBool(m.IsClusterWriter),
				})
			}
			out = append(out, cl)
		}
	}
	return out, nil
}

func vpcSecurityGroupIDs(in []types.VpcSecurityGroupMembership) []string {
	var out []string
	for _, g := range in {
		out = append(out, aws.ToString(g.VpcSecurityGroupId))
	}
	return out
}

func rdsTags(in []types.Tag) map[string]string {
	tags := make(map[string]string, len(in))
	for _, t := range in {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return tags
}
