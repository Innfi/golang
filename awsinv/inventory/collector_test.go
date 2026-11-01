package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// fakeAWS serves STS + EC2 + IAM on one endpoint. Account 222222222222's role
// is "broken". onCall, if set, runs inside each Describe*/ListUsers call.
func fakeAWS(t *testing.T, assumeCalls *atomic.Int32, onCall func(acct string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				onCall(acct)
			}
			fmt.Fprintf(w, `<DescribeVpcsResponse><vpcSet><item>
				<vpcId>vpc-%s-%s</vpcId><cidrBlock>10.0.0.0/16</cidrBlock><state>available</state>
				<isDefault>false</isDefault><tagSet><item><key>Name</key><value>main</value></item></tagSet>
				</item></vpcSet></DescribeVpcsResponse>`, acct, region)
		case "DescribeSubnets":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct)
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
				onCall(acct)
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
				onCall(acct)
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
				onCall(acct)
			}
			fmt.Fprintf(w, `<DescribeInternetGatewaysResponse><internetGatewaySet><item>
				<internetGatewayId>igw-%s-%s</internetGatewayId><ownerId>%s</ownerId>
				<attachmentSet><item><vpcId>vpc-%s-%s</vpcId><state>available</state></item></attachmentSet>
				<tagSet><item><key>Name</key><value>edge</value></item></tagSet>
				</item></internetGatewaySet></DescribeInternetGatewaysResponse>`, acct, region, acct, acct, region)
		case "DescribeEgressOnlyInternetGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct)
			}
			fmt.Fprintf(w, `<DescribeEgressOnlyInternetGatewaysResponse><egressOnlyInternetGatewaySet><item>
				<egressOnlyInternetGatewayId>eigw-%s-%s</egressOnlyInternetGatewayId>
				<attachmentSet><item><vpcId>vpc-%s-%s</vpcId><state>attached</state></item></attachmentSet>
				<tagSet><item><key>Name</key><value>v6</value></item></tagSet>
				</item></egressOnlyInternetGatewaySet></DescribeEgressOnlyInternetGatewaysResponse>`, acct, region, acct, region)
		case "DescribeTransitGateways":
			acct, region := caller(r)
			if onCall != nil {
				onCall(acct)
			}
			// Owned by another account: shared through RAM.
			fmt.Fprintf(w, `<DescribeTransitGatewaysResponse><transitGatewaySet><item>
				<transitGatewayId>tgw-%s-%s</transitGatewayId><transitGatewayArn>arn:tgw-%s-%s</transitGatewayArn>
				<ownerId>999999999999</ownerId><description>hub</description><state>available</state>
				<options><amazonSideAsn>64512</amazonSideAsn></options>
				<tagSet><item><key>Name</key><value>core</value></item></tagSet>
				</item></transitGatewaySet></DescribeTransitGatewaysResponse>`, acct, region, acct, region)
		case "ListUsers":
			acct, _ := caller(r)
			if onCall != nil {
				onCall(acct)
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

// caller reads the SigV4 header: Credential=AK<acct>/<date>/<region>/<svc>/aws4_request
func caller(r *http.Request) (acct, region string) {
	cred := strings.Split(strings.Split(r.Header.Get("Authorization"), "Credential=")[1], "/")
	return strings.TrimPrefix(cred[0], "AK"), cred[2]
}

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
	for _, target := range []struct{ acct, region string }{
		{"111111111111", "ap-northeast-2"}, {"111111111111", "us-east-1"}, {"333333333333", "ap-northeast-2"},
	} {
		id := target.acct + "-" + target.region
		wantSubnet := Subnet{
			AccountID: target.acct, Region: target.region, SubnetID: "subnet-" + id, VpcID: "vpc-" + id,
			CIDR: "10.0.1.0/24", AvailabilityZone: target.region + "a", AvailableIPs: 251, MapPublicIP: true,
			State: "available", Name: "public", Tags: map[string]string{"Name": "public"},
		}
		if got := gotSubnet["subnet-"+id]; !reflect.DeepEqual(got, wantSubnet) {
			t.Errorf("bad subnet mapping:\n got %+v\nwant %+v", got, wantSubnet)
		}
		wantRT := RouteTable{
			AccountID: target.acct, Region: target.region, RouteTableID: "rtb-" + id, VpcID: "vpc-" + id,
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
			AccountID: target.acct, Region: target.region, NATGatewayID: "nat-" + id, VpcID: "vpc-" + id, SubnetID: "subnet-" + id,
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
			AccountID: target.acct, Region: target.region, InternetGatewayID: "igw-" + id, OwnerID: target.acct,
			Attachments: []GatewayAttachment{{VpcID: "vpc-" + id, State: "available"}},
			Name:        "edge", Tags: map[string]string{"Name": "edge"},
		}
		if got := gotIGW["igw-"+id]; !reflect.DeepEqual(got, wantIGW) {
			t.Errorf("bad internet gateway mapping:\n got %+v\nwant %+v", got, wantIGW)
		}
		wantEIGW := EgressOnlyInternetGateway{
			AccountID: target.acct, Region: target.region, EgressOnlyInternetGatewayID: "eigw-" + id,
			Attachments: []GatewayAttachment{{VpcID: "vpc-" + id, State: "attached"}},
			Name:        "v6", Tags: map[string]string{"Name": "v6"},
		}
		if got := gotEIGW["eigw-"+id]; !reflect.DeepEqual(got, wantEIGW) {
			t.Errorf("bad egress-only internet gateway mapping:\n got %+v\nwant %+v", got, wantEIGW)
		}
		wantTGW := TransitGateway{
			AccountID: target.acct, Region: target.region, TransitGatewayID: "tgw-" + id, ARN: "arn:tgw-" + id,
			OwnerID: "999999999999", AmazonSideASN: 64512, Description: "hub", State: "available",
			Name: "core", Tags: map[string]string{"Name": "core"},
		}
		if got := gotTGW["tgw-"+id]; !reflect.DeepEqual(got, wantTGW) {
			t.Errorf("bad transit gateway mapping:\n got %+v\nwant %+v", got, wantTGW)
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
		if p.Kind != want.kind || p.AccountID != want.acct || p.CreateDate.IsZero() {
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
	if len(inv.Errors) != 8 || !failed["vpc/ap-northeast-2"] || !failed["subnet/ap-northeast-2"] ||
		!failed["routetable/ap-northeast-2"] || !failed["natgateway/ap-northeast-2"] ||
		!failed["internetgateway/ap-northeast-2"] || !failed["egressonlyinternetgateway/ap-northeast-2"] ||
		!failed["transitgateway/ap-northeast-2"] || !failed["iam/global"] {
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
		len(inv.InternetGateways) != 0 || len(inv.EgressOnlyInternetGateways) != 0 || len(inv.TransitGateways) != 0 || len(inv.IAM) != 0 {
		t.Errorf("want nothing after cancel, got %+v", inv)
	}
	// 3 scans (2 + 1 regions) for each of the 7 regional types + 2 IAM scans (one per account).
	if len(inv.Errors) != 23 {
		t.Fatalf("want 23 scan errors, got %v", inv.Errors)
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
	)
	srv := fakeAWS(t, &assumeCalls, func(acct string) {
		mu.Lock()
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
		len(inv.InternetGateways) != 8 || len(inv.EgressOnlyInternetGateways) != 8 || len(inv.TransitGateways) != 8 || len(inv.IAM) != 4 {
		t.Fatalf("want 8 of each regional type, 4 principals, no errors; got %d VPCs, %d subnets, %d route tables, %d NAT, %d internet, %d egress-only, %d transit, %d principals, errs %v",
			len(inv.VPCs), len(inv.Subnets), len(inv.RouteTables), len(inv.NATGateways), len(inv.InternetGateways),
			len(inv.EgressOnlyInternetGateways), len(inv.TransitGateways), len(inv.IAM), inv.Errors)
	}
	// The cap holds across resource types, not just within VPC scans.
	for acct, n := range peak {
		if n != 1 {
			t.Errorf("account %s peak concurrency = %d, want 1", acct, n)
		}
	}
}
