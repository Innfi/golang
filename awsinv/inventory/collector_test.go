package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
// is "broken". onCall, if set, runs inside each DescribeVpcs/ListUsers call.
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
	if len(inv.Errors) != 2 || !failed["vpc/ap-northeast-2"] || !failed["iam/global"] {
		t.Fatalf("want vpc + iam failure for 222222222222, got %v", inv.Errors)
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

	if len(inv.VPCs) != 0 || len(inv.IAM) != 0 {
		t.Errorf("want nothing after cancel, got %+v", inv)
	}
	// 3 VPC scans (2 + 1 regions) + 2 IAM scans (one per account).
	if len(inv.Errors) != 5 {
		t.Fatalf("want 5 scan errors, got %v", inv.Errors)
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
	if len(inv.Errors) != 0 || len(inv.VPCs) != 8 || len(inv.IAM) != 4 {
		t.Fatalf("want 8 VPCs, 4 principals, no errors; got %d VPCs, %d principals, errs %v",
			len(inv.VPCs), len(inv.IAM), inv.Errors)
	}
	// The cap holds across resource types, not just within VPC scans.
	for acct, n := range peak {
		if n != 1 {
			t.Errorf("account %s peak concurrency = %d, want 1", acct, n)
		}
	}
}
