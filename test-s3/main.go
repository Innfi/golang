package main

import (
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func main() {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("ap-northeast-2"),
	})

	if err != nil {
		log.Fatal("failed to create aws session")
	}

	s3Service := s3.New(sess)

	request, objErr := s3Service.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String("innfisBucket"),
		Key:    aws.String("myKey"),
	})
	if objErr != nil {
		log.Fatal("failed to create object: ", objErr)
	}

	presignUrl, presignErr := request.Presign(1 * time.Minute)
	if presignErr != nil {
		log.Fatal("object presign err: ", presignErr)
	}

	log.Println("presignUrl: ", presignUrl)
}
