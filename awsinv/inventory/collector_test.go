package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/store"
)

// fakeAWS serves STS + EC2 + IAM + EKS on one endpoint. Account 222222222222's role
// is "broken". onCall, if set, runs inside each Describe*/ListUsers call with the
// caller account and target (its region, or "global" for IAM). For EKS it runs
// on ListClusters only, so each resource type counts as one call.
func fakeAWS(t *testing.T, assumeCalls *atomic.Int32, onCall func(acct, target string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/clusters") {
			fakeEKS(t, w, r, onCall)
			return
		}
		_ = r.ParseForm()
		switch r.Form.Get("Action") {
		case "AssumeRole":
			role := r.Form.Get("RoleArn")
			if strings.Contains(role, "222222222222") {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<ErrorResponse><Error><Code>AccessDenied</Code><Message>not trusted</Message></Error></ErrorResponse>`)
				return
			}
			assumeCalls.Add(1) // successful ones only: failures aren't cached, so retries vary
			// Encode the account into the access key so later calls know who's calling.
			acct := strings.Split(role, ":")[4]
			fmt.Fprintf(w, `<AssumeRoleResponse><AssumeRoleResult><Credentials>
				<AccessKeyId>AK%s</AccessKeyId><SecretAccessKey>s</SecretAccessKey>
				<SessionToken>t</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration>
				</Credentials></AssumeRoleResult></AssumeRoleResponse>`, acct)
		case "DescribeVpcs":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeVpcsResponse><vpcSet><item>
				<vpcId>vpc-%s-%s</vpcId><cidrBlock>10.0.0.0/16</cidrBlock><state>available</state>
				<isDefault>false</isDefault><tagSet><item><key>Name</key><value>main</value></item></tagSet>
				</item></vpcSet></DescribeVpcsResponse>`, acct, region)
		case "DescribeSubnets":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeSubnetsResponse><subnetSet><item>
				<subnetId>subnet-%s-%s</subnetId><vpcId>vpc-%s-%s</vpcId><cidrBlock>10.0.1.0/24</cidrBlock>
				<availabilityZone>%sa</availabilityZone><availableIpAddressCount>251</availableIpAddressCount>
				<mapPublicIpOnLaunch>true</mapPublicIpOnLaunch><defaultForAz>false</defaultForAz><state>available</state>
				<tagSet><item><key>Name</key><value>public</value></item></tagSet>
				</item></subnetSet></DescribeSubnetsResponse>`, acct, region, acct, region, region)
		case "DescribeRouteTables":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeRouteTablesResponse><routeTableSet><item>
				<routeTableId>rtb-%s-%s</routeTableId><vpcId>vpc-%s-%s</vpcId>
				<associationSet>
					<item><routeTableAssociationId>rtbassoc-main</routeTableAssociationId><main>true</main><associationState><state>associated</state></associationState></item>
					<item><routeTableAssociationId>rtbassoc-subnet</routeTableAssociationId><main>false</main><subnetId>subnet-%s-%s</subnetId><associationState><state>associated</state></associationState></item>
					<item><routeTableAssociationId>rtbassoc-edge</routeTableAssociationId><main>false</main><gatewayId>igw-1</gatewayId><associationState><state>failed</state></associationState></item>
				</associationSet>
				<routeSet>
					<item><destinationCidrBlock>10.0.0.0/16</destinationCidrBlock><gatewayId>local</gatewayId><state>active</state><origin>CreateRouteTable</origin></item>
					<item><destinationCidrBlock>0.0.0.0/0</destinationCidrBlock><natGatewayId>nat-1</natGatewayId><state>blackhole</state><origin>CreateRoute</origin></item>
					<item><destinationPrefixListId>pl-1</destinationPrefixListId><instanceId>i-1</instanceId><networkInterfaceId>eni-1</networkInterfaceId><state>active</state><origin>CreateRoute</origin></item>
				</routeSet>
				<tagSet><item><key>Name</key><value>private</value></item></tagSet>
				</item></routeTableSet></DescribeRouteTablesResponse>`, acct, region, acct, region, acct, region)
		case "DescribeNatGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeNatGatewaysResponse><natGatewaySet><item>
				<natGatewayId>nat-%s-%s</natGatewayId><vpcId>vpc-%s-%s</vpcId><subnetId>subnet-%s-%s</subnetId>
				<connectivityType>public</connectivityType><availabilityMode>zonal</availabilityMode><state>available</state>
				<natGatewayAddressSet><item>
					<allocationId>eipalloc-1</allocationId><networkInterfaceId>eni-1</networkInterfaceId>
					<privateIp>10.0.1.5</privateIp><publicIp>203.0.113.5</publicIp>
					<availabilityZone>%sa</availabilityZone><isPrimary>true</isPrimary><status>succeeded</status>
				</item></natGatewayAddressSet>
				<tagSet><item><key>Name</key><value>egress</value></item></tagSet>
				</item></natGatewaySet></DescribeNatGatewaysResponse>`, acct, region, acct, region, acct, region, region)
		case "DescribeInternetGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeInternetGatewaysResponse><internetGatewaySet><item>
				<internetGatewayId>igw-%s-%s</internetGatewayId><ownerId>%s</ownerId>
				<attachmentSet><item><vpcId>vpc-%s-%s</vpcId><state>available</state></item></attachmentSet>
				<tagSet><item><key>Name</key><value>edge</value></item></tagSet>
				</item></internetGatewaySet></DescribeInternetGatewaysResponse>`, acct, region, acct, acct, region)
		case "DescribeEgressOnlyInternetGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeEgressOnlyInternetGatewaysResponse><egressOnlyInternetGatewaySet><item>
				<egressOnlyInternetGatewayId>eigw-%s-%s</egressOnlyInternetGatewayId>
				<attachmentSet><item><vpcId>vpc-%s-%s</vpcId><state>attached</state></item></attachmentSet>
				<tagSet><item><key>Name</key><value>v6</value></item></tagSet>
				</item></egressOnlyInternetGatewaySet></DescribeEgressOnlyInternetGatewaysResponse>`, acct, region, acct, region)
		case "DescribeTransitGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			// Owned by another account: shared through RAM.
			fmt.Fprintf(w, `<DescribeTransitGatewaysResponse><transitGatewaySet><item>
				<transitGatewayId>tgw-%s-%s</transitGatewayId><transitGatewayArn>arn:tgw-%s-%s</transitGatewayArn>
				<ownerId>999999999999</ownerId><description>hub</description><state>available</state>
				<options><amazonSideAsn>64512</amazonSideAsn></options>
				<tagSet><item><key>Name</key><value>core</value></item></tagSet>
				</item></transitGatewaySet></DescribeTransitGatewaysResponse>`, acct, region, acct, region)
		case "DescribeInstances":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeInstancesResponse><reservationSet><item>
				<reservationId>r-1</reservationId><ownerId>%s</ownerId>
				<instancesSet><item>
					<instanceId>i-%s-%s</instanceId><imageId>ami-1</imageId>
					<instanceState><code>16</code><name>running</name></instanceState>
					<instanceType>t3.micro</instanceType><keyName>ops</keyName><launchTime>2024-01-02T03:04:05Z</launchTime>
					<placement><availabilityZone>%sa</availabilityZone></placement>
					<privateIpAddress>10.0.1.10</privateIpAddress><ipAddress>203.0.113.10</ipAddress>
					<subnetId>subnet-%s-%s</subnetId><vpcId>vpc-%s-%s</vpcId>
					<architecture>arm64</architecture><instanceLifecycle>spot</instanceLifecycle><platformDetails>Linux/UNIX</platformDetails>
					<iamInstanceProfile><arn>arn:aws:iam::%s:instance-profile/app</arn><id>AIPA1</id></iamInstanceProfile>
					<groupSet><item><groupId>sg-1</groupId><groupName>web</groupName></item></groupSet>
					<blockDeviceMapping>
						<item><deviceName>/dev/xvda</deviceName><ebs><volumeId>vol-%s-%s</volumeId><status>attached</status><deleteOnTermination>true</deleteOnTermination></ebs></item>
					</blockDeviceMapping>
					<tagSet><item><key>Name</key><value>app</value></item></tagSet>
				</item></instancesSet>
				</item></reservationSet></DescribeInstancesResponse>`,
				acct, acct, region, region, acct, region, acct, region, acct, acct, region)
		case "DescribeVolumes":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct, region)
			}
			fmt.Fprintf(w, `<DescribeVolumesResponse><volumeSet><item>
				<volumeId>vol-%s-%s</volumeId><volumeType>gp3</volumeType><size>8</size><iops>3000</iops><throughput>125</throughput>
				<status>in-use</status><availabilityZone>%sa</availabilityZone>
				<encrypted>true</encrypted><kmsKeyId>arn:kms-1</kmsKeyId><snapshotId>snap-1</snapshotId>
				<multiAttachEnabled>false</multiAttachEnabled><createTime>2024-01-02T03:04:00Z</createTime>
				<attachmentSet><item><instanceId>i-%s-%s</instanceId><device>/dev/xvda</device><status>attached</status><deleteOnTermination>true</deleteOnTermination></item></attachmentSet>
				<tagSet><item><key>Name</key><value>root</value></item></tagSet>
				</item></volumeSet></DescribeVolumesResponse>`, acct, region, region, acct, region)
		case "ListUsers":
			acct, _ := caller(r)
			if onCall != nil {
				onCall(acct, globalRegion)
			}
			fmt.Fprintf(w, `<ListUsersResponse><ListUsersResult><Users><member>
				<UserName>alice</UserName><UserId>AIDA%s</UserId><Path>/</Path>
				<Arn>arn:aws:iam::%s:user/alice</Arn><CreateDate>2020-01-01T00:00:00Z</CreateDate>
				</member></Users><IsTruncated>false</IsTruncated></ListUsersResult></ListUsersResponse>`, acct, acct)
		case "ListRoles":
			acct, _ := caller(r)
			fmt.Fprintf(w, `<ListRolesResponse><ListRolesResult><Roles><member>
				<RoleName>Inv</RoleName><RoleId>AROA%s</RoleId><Path>/</Path>
				<Arn>arn:aws:iam::%s:role/Inv</Arn><CreateDate>2020-01-01T00:00:00Z</CreateDate>
				</member></Roles><IsTruncated>false</IsTruncated></ListRolesResult></ListRolesResponse>`, acct, acct)
		default:
			t.Errorf("unexpected action %q", r.Form.Get("Action"))
		}
	}))
}

// fakeEKS serves the EKS REST-JSON API: one cluster per target with two add-ons
// listed, one of which ("gone") was deleted before it could be described.
func fakeEKS(t *testing.T, w http.ResponseWriter, r *http.Request, onCall func(acct, target string)) {
	acct, region := caller(r)
	id := acct + "-" + region
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/clusters":
		if onCall != nil {
			onCall(acct, region)
		}
		fmt.Fprintf(w, `{"clusters":["k-%s"]}`, id)
	case "/clusters/k-" + id:
		fmt.Fprintf(w, `{"cluster":{
			"name":"k-%[1]s","arn":"arn:eks-%[1]s","version":"1.31","platformVersion":"eks.12","status":"ACTIVE",
			"endpoint":"https://k-%[1]s.eks","roleArn":"arn:aws:iam::%[2]s:role/eks","createdAt":1704164645,
			"resourcesVpcConfig":{"vpcId":"vpc-%[1]s","subnetIds":["subnet-%[1]s"],"securityGroupIds":["sg-1"],
				"clusterSecurityGroupId":"sg-eks","endpointPublicAccess":true,"endpointPrivateAccess":false,
				"publicAccessCidrs":["0.0.0.0/0"]},
			"accessConfig":{"authenticationMode":"API_AND_CONFIG_MAP"},
			"tags":{"team":"platform"}}}`, id, acct)
	case "/clusters/k-" + id + "/addons":
		fmt.Fprint(w, `{"addons":["vpc-cni","gone"]}`)
	case "/clusters/k-" + id + "/addons/vpc-cni":
		fmt.Fprintf(w, `{"addon":{
			"addonName":"vpc-cni","clusterName":"k-%[1]s","addonArn":"arn:addon-%[1]s","addonVersion":"v1.19.0-eksbuild.1",
			"status":"DEGRADED","serviceAccountRoleArn":"arn:aws:iam::%[2]s:role/cni","owner":"aws","publisher":"eks",
			"createdAt":1704164700,"modifiedAt":1704164800,
			"health":{"issues":[{"code":"InsufficientNumberOfReplicas","message":"1/2 ready","resourceIds":["aws-node"]}]}}}`, id, acct)
	case "/clusters/k-" + id + "/addons/gone":
		w.Header().Set("X-Amzn-Errortype", "ResourceNotFoundException")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"No addon: gone found in cluster"}`)
	default:
		t.Errorf("unexpected EKS path %q", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// caller reads the SigV4 header: Credential=AK<acct>/<date>/<region>/<svc>/aws4_request
func caller(r *http.Request) (acct, region string) {
	cred := strings.Split(strings.Split(r.Header.Get("Authorization"), "Credential=")[1], "/")
	return strings.TrimPrefix(cred[0], "AK"), cred[2]
}

// scanTime is the fixed clock newTestCollector uses for ScannedAt.
var scanTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func newTestCollector(srvURL string) *Collector {
	base := aws.Config{
		Region:       "ap-northeast-2",
		Credentials:  credentials.NewStaticCredentialsProvider("hub", "hub", ""),
		BaseEndpoint: aws.String(srvURL),
	}
	return &Collector{
		Creds:          awscreds.NewFactory(base, "test"),
		DefaultRegions: []string{"ap-northeast-2"},
		Concurrency:    4,
		now:            func() time.Time { return scanTime },
	}
}

func TestHasMainAssociation(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
	}{
		{"associated", true},
		{"associating", false},
		{"disassociating", false},
		{"disassociated", false},
		{"failed", false},
		{"", false},
	} {
		if got := hasMainAssociation([]Association{{IsMain: true, State: tc.state}}); got != tc.want {
			t.Errorf("state %q: got %v, want %v", tc.state, got, tc.want)
		}
	}
	if hasMainAssociation([]Association{{SubnetID: "subnet-1", State: "associated"}}) {
		t.Error("subnet association counted as main")
	}
}

func TestCollect_MultiAccount(t *testing.T) {
	var assumeCalls atomic.Int32
	srv := fakeAWS(t, &assumeCalls, nil)
	defer srv.Close()

	c := newTestCollector(srv.URL)
	accounts := []store.Account{
		{AccountID: "111111111111", RoleARN: "arn:aws:iam::111111111111:role/Inv", Regions: []string{"ap-northeast-2", "us-east-1"}},
		{AccountID: "222222222222", RoleARN: "arn:aws:iam::222222222222:role/Inv"},
		{AccountID: "333333333333", RoleARN: "arn:aws:iam::333333333333:role/Inv", ExternalID: "ext-1"},
	}

	inv := c.Collect(context.Background(), accounts)

	gotVPC := map[string]bool{}
	for _, v := range inv.VPCs {
		gotVPC[v.VpcID] = true
		if v.Name != "main" || v.CIDR != "10.0.0.0/16" {
			t.Errorf("bad VPC mapping: %+v", v)
		}
	}
	for _, want := range []string{
		"vpc-111111111111-ap-northeast-2", "vpc-111111111111-us-east-1", "vpc-333333333333-ap-northeast-2",
	} {
		if !gotVPC[want] {
			t.Errorf("missing %s (got %v)", want, gotVPC)
		}
	}

	gotSubnet := map[string]Subnet{}
	for _, s := range inv.Subnets {
		gotSubnet[s.SubnetID] = s
	}
	gotRT := map[string]RouteTable{}
	for _, rt := range inv.RouteTables {
		gotRT[rt.RouteTableID] = rt
	}
	if len(inv.Subnets) != 3 || len(inv.RouteTables) != 3 {
		t.Errorf("want 3 subnets, 3 route tables, got %d, %d", len(inv.Subnets), len(inv.RouteTables))
	}
	gotNAT := map[string]NATGateway{}
	for _, g := range inv.NATGateways {
		gotNAT[g.NATGatewayID] = g
	}
	gotIGW := map[string]InternetGateway{}
	for _, g := range inv.InternetGateways {
		gotIGW[g.InternetGatewayID] = g
	}
	gotEIGW := map[string]EgressOnlyInternetGateway{}
	for _, g := range inv.EgressOnlyInternetGateways {
		gotEIGW[g.EgressOnlyInternetGatewayID] = g
	}
	gotTGW := map[string]TransitGateway{}
	for _, g := range inv.TransitGateways {
		gotTGW[g.TransitGatewayID] = g
	}
	if len(inv.NATGateways) != 3 || len(inv.InternetGateways) != 3 || len(inv.EgressOnlyInternetGateways) != 3 || len(inv.TransitGateways) != 3 {
		t.Errorf("want 3 of each gateway, got %d NAT, %d internet, %d egress-only, %d transit",
			len(inv.NATGateways), len(inv.InternetGateways), len(inv.EgressOnlyInternetGateways), len(inv.TransitGateways))
	}
	gotInst := map[string]Instance{}
	for _, i := range inv.Instances {
		gotInst[i.InstanceID] = i
	}
	gotVol := map[string]Volume{}
	for _, v := range inv.Volumes {
		gotVol[v.VolumeID] = v
	}
	gotCluster := map[string]Cluster{}
	for _, k := range inv.Clusters {
		gotCluster[k.Name] = k
	}
	if len(inv.Instances) != 3 || len(inv.Volumes) != 3 || len(inv.Clusters) != 3 {
		t.Errorf("want 3 instances, 3 volumes, 3 clusters, got %d, %d, %d", len(inv.Instances), len(inv.Volumes), len(inv.Clusters))
	}
	for _, target := range []struct{ acct, region string }{
		{"111111111111", "ap-northeast-2"}, {"111111111111", "us-east-1"}, {"333333333333", "ap-northeast-2"},
	} {
		id := target.acct + "-" + target.region
		wantSubnet := Subnet{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, SubnetID: "subnet-" + id, VpcID: "vpc-" + id,
			CIDR: "10.0.1.0/24", AvailabilityZone: target.region + "a", AvailableIPs: 251, MapPublicIP: true,
			State: "available", Name: "public", Tags: map[string]string{"Name": "public"},
		}
		if got := gotSubnet["subnet-"+id]; !reflect.DeepEqual(got, wantSubnet) {
			t.Errorf("bad subnet mapping:\n got %+v\nwant %+v", got, wantSubnet)
		}
		wantRT := RouteTable{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, RouteTableID: "rtb-" + id, VpcID: "vpc-" + id,
			IsMain: true,
			Associations: []Association{
				{AssociationID: "rtbassoc-main", IsMain: true, State: "associated"},
				{AssociationID: "rtbassoc-subnet", SubnetID: "subnet-" + id, State: "associated"},
				{AssociationID: "rtbassoc-edge", GatewayID: "igw-1", State: "failed"},
			},
			Routes: []Route{
				{"10.0.0.0/16", "local", "active", "CreateRouteTable"},
				{"0.0.0.0/0", "nat-1", "blackhole", "CreateRoute"},
				{"pl-1", "i-1", "active", "CreateRoute"},
			},
			Name: "private", Tags: map[string]string{"Name": "private"},
		}
		if got := gotRT["rtb-"+id]; !reflect.DeepEqual(got, wantRT) {
			t.Errorf("bad route table mapping:\n got %+v\nwant %+v", got, wantRT)
		}
		wantNAT := NATGateway{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, NATGatewayID: "nat-" + id, VpcID: "vpc-" + id, SubnetID: "subnet-" + id,
			ConnectivityType: "public", AvailabilityMode: "zonal", State: "available",
			Addresses: []NATGatewayAddress{{
				AllocationID: "eipalloc-1", NetworkInterfaceID: "eni-1", PrivateIP: "10.0.1.5", PublicIP: "203.0.113.5",
				AvailabilityZone: target.region + "a", IsPrimary: true, Status: "succeeded",
			}},
			Name: "egress", Tags: map[string]string{"Name": "egress"},
		}
		if got := gotNAT["nat-"+id]; !reflect.DeepEqual(got, wantNAT) {
			t.Errorf("bad NAT gateway mapping:\n got %+v\nwant %+v", got, wantNAT)
		}
		wantIGW := InternetGateway{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, InternetGatewayID: "igw-" + id, OwnerID: target.acct,
			Attachments: []GatewayAttachment{{VpcID: "vpc-" + id, State: "available"}},
			Name:        "edge", Tags: map[string]string{"Name": "edge"},
		}
		if got := gotIGW["igw-"+id]; !reflect.DeepEqual(got, wantIGW) {
			t.Errorf("bad internet gateway mapping:\n got %+v\nwant %+v", got, wantIGW)
		}
		wantEIGW := EgressOnlyInternetGateway{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, EgressOnlyInternetGatewayID: "eigw-" + id,
			Attachments: []GatewayAttachment{{VpcID: "vpc-" + id, State: "attached"}},
			Name:        "v6", Tags: map[string]string{"Name": "v6"},
		}
		if got := gotEIGW["eigw-"+id]; !reflect.DeepEqual(got, wantEIGW) {
			t.Errorf("bad egress-only internet gateway mapping:\n got %+v\nwant %+v", got, wantEIGW)
		}
		wantTGW := TransitGateway{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, TransitGatewayID: "tgw-" + id, ARN: "arn:tgw-" + id,
			OwnerID: "999999999999", AmazonSideASN: 64512, Description: "hub", State: "available",
			Name: "core", Tags: map[string]string{"Name": "core"},
		}
		if got := gotTGW["tgw-"+id]; !reflect.DeepEqual(got, wantTGW) {
			t.Errorf("bad transit gateway mapping:\n got %+v\nwant %+v", got, wantTGW)
		}
		wantInst := Instance{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, InstanceID: "i-" + id, InstanceType: "t3.micro",
			State: "running", ImageID: "ami-1", VpcID: "vpc-" + id, SubnetID: "subnet-" + id,
			AvailabilityZone: target.region + "a", PrivateIP: "10.0.1.10", PublicIP: "203.0.113.10", KeyName: "ops",
			PlatformDetails: "Linux/UNIX", Architecture: "arm64", Lifecycle: "spot",
			IAMInstanceProfile: "arn:aws:iam::" + target.acct + ":instance-profile/app",
			SecurityGroupIDs:   []string{"sg-1"},
			BlockDevices:       []BlockDevice{{DeviceName: "/dev/xvda", VolumeID: "vol-" + id, Status: "attached", DeleteOnTermination: true}},
			LaunchTime:         time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			Name:               "app", Tags: map[string]string{"Name": "app"},
		}
		if got := gotInst["i-"+id]; !reflect.DeepEqual(got, wantInst) {
			t.Errorf("bad instance mapping:\n got %+v\nwant %+v", got, wantInst)
		}
		wantVol := Volume{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, VolumeID: "vol-" + id, VolumeType: "gp3",
			SizeGiB: 8, IOPS: 3000, ThroughputMiBps: 125, State: "in-use", AvailabilityZone: target.region + "a",
			Encrypted: true, KMSKeyID: "arn:kms-1", SnapshotID: "snap-1",
			Attachments: []VolumeAttachment{{InstanceID: "i-" + id, Device: "/dev/xvda", State: "attached", DeleteOnTermination: true}},
			CreateTime:  time.Date(2024, 1, 2, 3, 4, 0, 0, time.UTC),
			Name:        "root", Tags: map[string]string{"Name": "root"},
		}
		if got := gotVol["vol-"+id]; !reflect.DeepEqual(got, wantVol) {
			t.Errorf("bad volume mapping:\n got %+v\nwant %+v", got, wantVol)
		}
		wantCluster := Cluster{
			AccountID: target.acct, Region: target.region, ScannedAt: scanTime, Name: "k-" + id, ARN: "arn:eks-" + id,
			Version: "1.31", PlatformVersion: "eks.12", Status: "ACTIVE", Endpoint: "https://k-" + id + ".eks",
			RoleARN: "arn:aws:iam::" + target.acct + ":role/eks", VpcID: "vpc-" + id,
			SubnetIDs: []string{"subnet-" + id}, SecurityGroupIDs: []string{"sg-1"}, ClusterSecurityGroupID: "sg-eks",
			EndpointPublicAccess: true, PublicAccessCIDRs: []string{"0.0.0.0/0"}, AuthenticationMode: "API_AND_CONFIG_MAP",
			Addons: []Addon{{ // "gone" was deleted after listing: skipped, not an error
				Name: "vpc-cni", ARN: "arn:addon-" + id, Version: "v1.19.0-eksbuild.1", Status: "DEGRADED",
				ServiceAccountRoleARN: "arn:aws:iam::" + target.acct + ":role/cni", Owner: "aws", Publisher: "eks",
				HealthIssues: []AddonIssue{{Code: "InsufficientNumberOfReplicas", Message: "1/2 ready", ResourceIDs: []string{"aws-node"}}},
				CreatedAt:    time.Date(2024, 1, 2, 3, 5, 0, 0, time.UTC),
				ModifiedAt:   time.Date(2024, 1, 2, 3, 6, 40, 0, time.UTC),
				Tags:         map[string]string{},
			}},
			CreatedAt: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			Tags:      map[string]string{"team": "platform"},
		}
		// JSON epoch timestamps decode in local time; compare in UTC.
		gotK := gotCluster["k-"+id]
		gotK.CreatedAt, gotK.Addons = gotK.CreatedAt.UTC(), slices.Clone(gotK.Addons)
		for i := range gotK.Addons {
			gotK.Addons[i].CreatedAt, gotK.Addons[i].ModifiedAt = gotK.Addons[i].CreatedAt.UTC(), gotK.Addons[i].ModifiedAt.UTC()
		}
		if !reflect.DeepEqual(gotK, wantCluster) {
			t.Errorf("bad cluster mapping:\n got %+v\nwant %+v", gotK, wantCluster)
		}
	}

	// IAM is global: account 111 has two regions but is scanned once.
	gotIAM := map[string]IAMPrincipal{}
	for _, p := range inv.IAM {
		gotIAM[p.ARN] = p
	}
	if len(inv.IAM) != 4 {
		t.Errorf("want 4 principals (user+role for 2 accounts), got %v", inv.IAM)
	}
	for _, want := range []struct{ arn, kind, acct string }{
		{"arn:aws:iam::111111111111:user/alice", "user", "111111111111"},
		{"arn:aws:iam::111111111111:role/Inv", "role", "111111111111"},
		{"arn:aws:iam::333333333333:user/alice", "user", "333333333333"},
		{"arn:aws:iam::333333333333:role/Inv", "role", "333333333333"},
	} {
		p, ok := gotIAM[want.arn]
		if !ok {
			t.Errorf("missing %s", want.arn)
			continue
		}
		if p.Kind != want.kind || p.AccountID != want.acct || p.CreateDate.IsZero() || !p.ScannedAt.Equal(scanTime) {
			t.Errorf("bad IAM mapping: %+v", p)
		}
	}

	failed := map[string]bool{}
	for _, e := range inv.Errors {
		if e.AccountID != "222222222222" {
			t.Errorf("unexpected failure: %v", e)
		}
		failed[e.Resource+"/"+e.Region] = true
	}
	if len(inv.Errors) != 11 || !failed["vpc/ap-northeast-2"] || !failed["subnet/ap-northeast-2"] ||
		!failed["routetable/ap-northeast-2"] || !failed["natgateway/ap-northeast-2"] ||
		!failed["internetgateway/ap-northeast-2"] || !failed["egressonlyinternetgateway/ap-northeast-2"] ||
		!failed["transitgateway/ap-northeast-2"] || !failed["ec2/ap-northeast-2"] || !failed["ebs/ap-northeast-2"] ||
		!failed["eks/ap-northeast-2"] || !failed["iam/global"] {
		t.Fatalf("want a failure per resource type for 222222222222, got %v", inv.Errors)
	}
	// Credentials are cached per account across regions and resource types.
	if n := assumeCalls.Load(); n != 2 {
		t.Errorf("successful AssumeRole calls = %d, want 2 (one per healthy account)", n)
	}
}

func TestCollect_Cancelled(t *testing.T) {
	var assumeCalls atomic.Int32
	srv := fakeAWS(t, &assumeCalls, nil)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	accounts := []store.Account{
		{AccountID: "111111111111", RoleARN: "arn:aws:iam::111111111111:role/Inv", Regions: []string{"ap-northeast-2", "us-east-1"}},
		{AccountID: "333333333333", RoleARN: "arn:aws:iam::333333333333:role/Inv"},
	}
	inv := newTestCollector(srv.URL).Collect(ctx, accounts)

	if len(inv.VPCs) != 0 || len(inv.Subnets) != 0 || len(inv.RouteTables) != 0 || len(inv.NATGateways) != 0 ||
		len(inv.InternetGateways) != 0 || len(inv.EgressOnlyInternetGateways) != 0 || len(inv.TransitGateways) != 0 ||
		len(inv.Instances) != 0 || len(inv.Volumes) != 0 || len(inv.Clusters) != 0 || len(inv.IAM) != 0 {
		t.Errorf("want nothing after cancel, got %+v", inv)
	}
	// 3 scans (2 + 1 regions) for each of the 10 regional types + 2 IAM scans (one per account).
	if len(inv.Errors) != 32 {
		t.Fatalf("want 32 scan errors, got %v", inv.Errors)
	}
	for _, e := range inv.Errors {
		if !errors.Is(e.Err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", e)
		}
	}
	if n := assumeCalls.Load(); n != 0 {
		t.Errorf("AssumeRole calls = %d, want 0 (nothing should start)", n)
	}
}

func TestCollect_AccountConcurrency(t *testing.T) {
	var (
		assumeCalls atomic.Int32
		mu          sync.Mutex
		inflight    = map[string]int{}
		peak        = map[string]int{}
		calls       = map[string][]string{} // account -> target of each call, in order
	)
	srv := fakeAWS(t, &assumeCalls, func(acct, target string) {
		mu.Lock()
		calls[acct] = append(calls[acct], target)
		inflight[acct]++
		peak[acct] = max(peak[acct], inflight[acct])
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inflight[acct]--
		mu.Unlock()
	})
	defer srv.Close()

	c := newTestCollector(srv.URL)
	c.AccountConcurrency = 1
	regions := []string{"ap-northeast-2", "us-east-1", "us-west-2", "eu-west-1"}
	accounts := []store.Account{
		{AccountID: "111111111111", RoleARN: "arn:aws:iam::111111111111:role/Inv", Regions: regions},
		{AccountID: "333333333333", RoleARN: "arn:aws:iam::333333333333:role/Inv", Regions: regions},
	}

	inv := c.Collect(context.Background(), accounts)
	if len(inv.Errors) != 0 || len(inv.VPCs) != 8 || len(inv.Subnets) != 8 || len(inv.RouteTables) != 8 || len(inv.NATGateways) != 8 ||
		len(inv.InternetGateways) != 8 || len(inv.EgressOnlyInternetGateways) != 8 || len(inv.TransitGateways) != 8 ||
		len(inv.Instances) != 8 || len(inv.Volumes) != 8 || len(inv.Clusters) != 8 || len(inv.IAM) != 4 {
		t.Fatalf("want 8 of each regional type, 4 principals, no errors; got %d VPCs, %d subnets, %d route tables, %d NAT, %d internet, %d egress-only, %d transit, %d instances, %d volumes, %d clusters, %d principals, errs %v",
			len(inv.VPCs), len(inv.Subnets), len(inv.RouteTables), len(inv.NATGateways), len(inv.InternetGateways),
			len(inv.EgressOnlyInternetGateways), len(inv.TransitGateways), len(inv.Instances), len(inv.Volumes), len(inv.Clusters), len(inv.IAM), inv.Errors)
	}
	// The cap holds across resource types, not just within VPC scans.
	for acct, n := range peak {
		if n != 1 {
			t.Errorf("account %s peak concurrency = %d, want 1", acct, n)
		}
	}
	// A target runs all its types back-to-back: once the account moves on from a
	// target, that target is never called again.
	for acct, seq := range calls {
		done := map[string]bool{}
		for i, target := range seq {
			if done[target] {
				t.Fatalf("account %s: target %s resumed after others ran: %v", acct, target, seq)
			}
			if i+1 < len(seq) && seq[i+1] != target {
				done[target] = true
			}
		}
		if n := len(seq); n != 4*10+1 {
			t.Errorf("account %s: %d calls, want %d (10 types x 4 regions + IAM)", acct, n, 4*10+1)
		}
	}
}
