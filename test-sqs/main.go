package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

func main() {
	queueUrl := "http://your-queue-url.region.amazonaws.com"
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := sqs.New(sess)

	sendResult, err := svc.SendMessage(&sqs.SendMessageInput{
		QueueUrl:     aws.String(queueUrl),
		DelaySeconds: aws.Int64(30),
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"MessageType": {
				DataType:    aws.String("String"),
				StringValue: aws.String("greeting"),
			},
		},
		MessageBody: aws.String("queue message body"),
	})

	if err != nil {
		fmt.Println("SendMessageErr: ", err)
		return
	}

	fmt.Println("sendResult.MessageId: ", *(sendResult.MessageId))
	fmt.Println("sendResult.SequenceNumber: ", *(sendResult.SequenceNumber))
}
