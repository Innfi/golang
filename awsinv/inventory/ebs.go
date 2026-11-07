package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// Volume is a flattened EBS volume. One with no Attachments is unattached
// (State "available").
type Volume struct {
	AccountID        string
	Region           string
	VolumeID         string
	VolumeType       string // gp3, io2, ...
	SizeGiB          int32
	IOPS             int32 // 0 when the type has no provisioned IOPS
	ThroughputMiBps  int32 // gp3 only
	State            string
	AvailabilityZone string
	Encrypted        bool
	KMSKeyID         string
	SnapshotID       string // source snapshot, empty if created blank
	MultiAttach      bool
	Attachments      []VolumeAttachment
	CreateTime       time.Time
	Name             string
	Tags             map[string]string
	ScannedAt        time.Time // see Inventory
}

// VolumeAttachment links an EBS volume to an instance.
type VolumeAttachment struct {
	InstanceID          string
	Device              string
	State               string
	DeleteOnTermination bool
}

func (c *Collector) volumesIn(ctx context.Context, t target) ([]Volume, error) {
	acct, region, client := t.acct, t.region, t.ec2

	var out []Volume
	p := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Volumes {
			tags := tagMap(v.Tags)
			vol := Volume{
				AccountID:        acct.AccountID,
				Region:           region,
				ScannedAt:        t.scannedAt,
				VolumeID:         aws.ToString(v.VolumeId),
				VolumeType:       string(v.VolumeType),
				SizeGiB:          aws.ToInt32(v.Size),
				IOPS:             aws.ToInt32(v.Iops),
				ThroughputMiBps:  aws.ToInt32(v.Throughput),
				State:            string(v.State),
				AvailabilityZone: aws.ToString(v.AvailabilityZone),
				Encrypted:        aws.ToBool(v.Encrypted),
				KMSKeyID:         aws.ToString(v.KmsKeyId),
				SnapshotID:       aws.ToString(v.SnapshotId),
				MultiAttach:      aws.ToBool(v.MultiAttachEnabled),
				CreateTime:       aws.ToTime(v.CreateTime),
				Name:             tags["Name"],
				Tags:             tags,
			}
			for _, a := range v.Attachments {
				vol.Attachments = append(vol.Attachments, VolumeAttachment{
					InstanceID:          aws.ToString(a.InstanceId),
					Device:              aws.ToString(a.Device),
					State:               string(a.State),
					DeleteOnTermination: aws.ToBool(a.DeleteOnTermination),
				})
			}
			out = append(out, vol)
		}
	}
	return out, nil
}
