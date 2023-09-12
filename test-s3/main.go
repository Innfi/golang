package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go/aws"
)

func main() {
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(os.Getenv("REGION")),
	)
	if err != nil {
		log.Fatal("failed to load config")
	}

	s3Client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(s3Client)

	presignResult, err := presignClient.PresignPutObject(
		context.TODO(),
		&s3.PutObjectInput{
			Bucket: aws.String(os.Getenv("BUCKET")),
			Key:    aws.String("test_key"),
		},
		func(opts *s3.PresignOptions) {
			opts.Expires = time.Duration(60 * time.Second)
		},
	)

	if err != nil {
		log.Fatal("failed to presign for PutObject")
	}

	//TODO: upload object via presign url

	log.Println("url: ", presignResult.URL)
}
