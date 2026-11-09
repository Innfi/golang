package inventory

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// Alarm is a flattened CloudWatch alarm, metric or composite. Tags are not
// collected: they need one ListTagsForResource call per alarm.
type Alarm struct {
	AccountID      string
	Region         string
	Name           string
	ARN            string
	Type           string // "metric" or "composite"
	Description    string
	State          string // "OK", "ALARM" or "INSUFFICIENT_DATA"
	StateReason    string
	StateUpdatedAt time.Time
	UpdatedAt      time.Time // last configuration change
	ActionsEnabled bool
	AlarmActions   []string // SNS topics, Auto Scaling policies, ...
	OKActions      []string
	ScannedAt      time.Time // see Inventory
	// Metric alarms only. Namespace and MetricName are empty for an alarm on a
	// metric math expression.
	Namespace          string
	MetricName         string
	Dimensions         map[string]string
	Statistic          string // "Average", ..., or an extended one like "p99"
	PeriodSeconds      int32
	EvaluationPeriods  int32
	ComparisonOperator string
	Threshold          float64
	TreatMissingData   string
	// Composite alarms only.
	AlarmRule string
}

func (c *Collector) alarmsIn(ctx context.Context, t target) ([]Alarm, error) {
	client := cloudwatch.NewFromConfig(t.cfg)

	var out []Alarm
	p := cloudwatch.NewDescribeAlarmsPaginator(client, &cloudwatch.DescribeAlarmsInput{
		AlarmTypes: []types.AlarmType{types.AlarmTypeMetricAlarm, types.AlarmTypeCompositeAlarm},
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range page.MetricAlarms {
			al := Alarm{
				AccountID:          t.acct.AccountID,
				Region:             t.region,
				ScannedAt:          t.scannedAt,
				Name:               aws.ToString(a.AlarmName),
				ARN:                aws.ToString(a.AlarmArn),
				Type:               "metric",
				Description:        aws.ToString(a.AlarmDescription),
				State:              string(a.StateValue),
				StateReason:        aws.ToString(a.StateReason),
				StateUpdatedAt:     aws.ToTime(a.StateUpdatedTimestamp),
				ActionsEnabled:     aws.ToBool(a.ActionsEnabled),
				AlarmActions:       a.AlarmActions,
				OKActions:          a.OKActions,
				Namespace:          aws.ToString(a.Namespace),
				MetricName:         aws.ToString(a.MetricName),
				Statistic:          string(a.Statistic),
				PeriodSeconds:      aws.ToInt32(a.Period),
				EvaluationPeriods:  aws.ToInt32(a.EvaluationPeriods),
				ComparisonOperator: string(a.ComparisonOperator),
				Threshold:          aws.ToFloat64(a.Threshold),
				TreatMissingData:   aws.ToString(a.TreatMissingData),
				UpdatedAt:          aws.ToTime(a.AlarmConfigurationUpdatedTimestamp),
			}
			if al.Statistic == "" {
				al.Statistic = aws.ToString(a.ExtendedStatistic)
			}
			if len(a.Dimensions) > 0 {
				al.Dimensions = make(map[string]string, len(a.Dimensions))
				for _, d := range a.Dimensions {
					al.Dimensions[aws.ToString(d.Name)] = aws.ToString(d.Value)
				}
			}
			out = append(out, al)
		}
		for _, a := range page.CompositeAlarms {
			out = append(out, Alarm{
				AccountID:      t.acct.AccountID,
				Region:         t.region,
				ScannedAt:      t.scannedAt,
				Name:           aws.ToString(a.AlarmName),
				ARN:            aws.ToString(a.AlarmArn),
				Type:           "composite",
				Description:    aws.ToString(a.AlarmDescription),
				State:          string(a.StateValue),
				StateReason:    aws.ToString(a.StateReason),
				StateUpdatedAt: aws.ToTime(a.StateUpdatedTimestamp),
				ActionsEnabled: aws.ToBool(a.ActionsEnabled),
				AlarmActions:   a.AlarmActions,
				OKActions:      a.OKActions,
				AlarmRule:      aws.ToString(a.AlarmRule),
				UpdatedAt:      aws.ToTime(a.AlarmConfigurationUpdatedTimestamp),
			})
		}
	}
	return out, nil
}
