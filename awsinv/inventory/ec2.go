package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// Instance is a flattened EC2 instance. AWS keeps listing terminated ones for
// about an hour, so check State before treating one as running.
type Instance struct {
	AccountID          string
	Region             string
	InstanceID         string
	InstanceType       string
	State              string
	ImageID            string
	VpcID              string // empty for EC2-Classic leftovers
	SubnetID           string
	AvailabilityZone   string
	PrivateIP          string
	PublicIP           string
	KeyName            string
	PlatformDetails    string // e.g. "Linux/UNIX", "Windows"
	Architecture       string
	Lifecycle          string // empty for on-demand, else "spot" or "scheduled"
	IAMInstanceProfile string // ARN
	SecurityGroupIDs   []string
	BlockDevices       []BlockDevice
	LaunchTime         time.Time
	Name               string
	Tags               map[string]string
	ScannedAt          time.Time // see Inventory
}

// BlockDevice is one EBS volume attached to an instance.
type BlockDevice struct {
	DeviceName          string
	VolumeID            string
	Status              string
	DeleteOnTermination bool
}

func (c *Collector) instancesIn(ctx context.Context, t target) ([]Instance, error) {
	acct, region, client := t.acct, t.region, t.ec2

	var out []Instance
	p := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				tags := tagMap(i.Tags)
				inst := Instance{
					AccountID:       acct.AccountID,
					Region:          region,
					ScannedAt:       t.scannedAt,
					InstanceID:      aws.ToString(i.InstanceId),
					InstanceType:    string(i.InstanceType),
					ImageID:         aws.ToString(i.ImageId),
					VpcID:           aws.ToString(i.VpcId),
					SubnetID:        aws.ToString(i.SubnetId),
					PrivateIP:       aws.ToString(i.PrivateIpAddress),
					PublicIP:        aws.ToString(i.PublicIpAddress),
					KeyName:         aws.ToString(i.KeyName),
					PlatformDetails: aws.ToString(i.PlatformDetails),
					Architecture:    string(i.Architecture),
					Lifecycle:       string(i.InstanceLifecycle),
					LaunchTime:      aws.ToTime(i.LaunchTime),
					Name:            tags["Name"],
					Tags:            tags,
				}
				if i.State != nil {
					inst.State = string(i.State.Name)
				}
				if i.Placement != nil {
					inst.AvailabilityZone = aws.ToString(i.Placement.AvailabilityZone)
				}
				if i.IamInstanceProfile != nil {
					inst.IAMInstanceProfile = aws.ToString(i.IamInstanceProfile.Arn)
				}
				for _, g := range i.SecurityGroups {
					inst.SecurityGroupIDs = append(inst.SecurityGroupIDs, aws.ToString(g.GroupId))
				}
				for _, b := range i.BlockDeviceMappings {
					if b.Ebs == nil {
						continue
					}
					inst.BlockDevices = append(inst.BlockDevices, BlockDevice{
						DeviceName:          aws.ToString(b.DeviceName),
						VolumeID:            aws.ToString(b.Ebs.VolumeId),
						Status:              string(b.Ebs.Status),
						DeleteOnTermination: aws.ToBool(b.Ebs.DeleteOnTermination),
					})
				}
				out = append(out, inst)
			}
		}
	}
	return out, nil
}
