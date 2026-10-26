package inventory

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/innfi/awsinv/awscreds"
	"github.com/innfi/awsinv/store"
)

// Fake STS + EC2 on one endpoint. Account 222222222222's role is "broken".
func TestCollectVPCs_MultiAccount(t *testing.T) {
	var assumeCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("Action") {
		case "AssumeRole":
			assumeCalls.Add(1)
			role := r.Form.Get("RoleArn")
			if strings.Contains(role, "222222222222") {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<ErrorResponse><Error><Code>AccessDenied</Code><Message>not trusted</Message></Error></ErrorResponse>`)
				return
			}
			// Encode the account into the access key so EC2 knows who's calling.
			acct := strings.Split(role, ":")[4]
			fmt.Fprintf(w, `<AssumeRoleResponse><AssumeRoleResult><Credentials>
				<AccessKeyId>AK%s</AccessKeyId><SecretAccessKey>s</SecretAccessKey>
				<SessionToken>t</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration>
				</Credentials></AssumeRoleResult></AssumeRoleResponse>`, acct)
		case "DescribeVpcs":
			// SigV4 header: Credential=AK<acct>/<date>/<region>/ec2/aws4_request
			auth := r.Header.Get("Authorization")
			cred := strings.Split(strings.Split(auth, "Credential=")[1], "/")
			acct, region := strings.TrimPrefix(cred[0], "AK"), cred[2]
			fmt.Fprintf(w, `<DescribeVpcsResponse><vpcSet><item>
				<vpcId>vpc-%s-%s</vpcId><cidrBlock>10.0.0.0/16</cidrBlock><state>available</state>
				<isDefault>false</isDefault><tagSet><item><key>Name</key><value>main</value></item></tagSet>
				</item></vpcSet></DescribeVpcsResponse>`, acct, region)
		default:
			t.Errorf("unexpected action %q", r.Form.Get("Action"))
		}
	}))
	defer srv.Close()

	base := aws.Config{
		Region:       "ap-northeast-2",
		Credentials:  credentials.NewStaticCredentialsProvider("hub", "hub", ""),
		BaseEndpoint: aws.String(srv.URL),
	}
	c := &Collector{
		Creds:          awscreds.NewFactory(base, "test"),
		DefaultRegions: []string{"ap-northeast-2"},
		Concurrency:    4,
	}
	accounts := []store.Account{
		{AccountID: "111111111111", RoleARN: "arn:aws:iam::111111111111:role/Inv", Regions: []string{"ap-northeast-2", "us-east-1"}},
		{AccountID: "222222222222", RoleARN: "arn:aws:iam::222222222222:role/Inv"},
		{AccountID: "333333333333", RoleARN: "arn:aws:iam::333333333333:role/Inv", ExternalID: "ext-1"},
	}

	vpcs, errs := c.CollectVPCs(context.Background(), accounts)

	got := map[string]bool{}
	for _, v := range vpcs {
		got[v.VpcID] = true
		if v.Name != "main" || v.CIDR != "10.0.0.0/16" {
			t.Errorf("bad mapping: %+v", v)
		}
	}
	for _, want := range []string{
		"vpc-111111111111-ap-northeast-2", "vpc-111111111111-us-east-1", "vpc-333333333333-ap-northeast-2",
	} {
		if !got[want] {
			t.Errorf("missing %s (got %v)", want, got)
		}
	}
	if len(errs) != 1 || errs[0].AccountID != "222222222222" {
		t.Fatalf("want exactly one failure for 222222222222, got %v", errs)
	}
	// Account 111 scans two regions but should assume its role only once.
	if n := assumeCalls.Load(); n != 3 {
		t.Errorf("AssumeRole calls = %d, want 3 (one per account)", n)
	}
}
