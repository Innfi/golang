package main

import (
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func HandlePresignUrlWorking() {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(os.Getenv("REGION")),
	})
	if err != nil {
		log.Fatal("NewSession()")
	}

	objectKey := "desc.png"
	aclString := "public-read"

	s3Client := s3.New(sess)

	req, _ := s3Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(os.Getenv("BUCKET")),
		Key:    aws.String(objectKey),
	})

	q := req.HTTPRequest.URL.Query()
	q.Add("x-amx-acl", aclString)

	req.HTTPRequest.URL.RawQuery = q.Encode()

	url, err := req.Presign(300 * time.Second)
	if err != nil {
		log.Fatal("req.Presign()")
	}

	log.Println("presignURL: ", url)
}
