package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/aws/smithy-go/logging"
)

func HandlePresignUrlNotWorking() {
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(os.Getenv("REGION")),
		config.WithLogger(logging.NewStandardLogger(os.Stdout)),
		config.WithClientLogMode(aws.LogRequestWithBody|aws.LogResponse),
	)
	if err != nil {
		log.Fatal("LoadDefaultConfig()")
	}

	objectKey := "desc.png"

	client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(client)

	presignResult, err := presignClient.PresignPutObject(
		context.TODO(),
		&s3.PutObjectInput{
			Bucket: aws.String(os.Getenv("BUCKET_NAME")),
			Key:    aws.String(objectKey),
			ACL:    types.ObjectCannedACLPublicRead, //it won't passed to s3
		},
		func(opts *s3.PresignOptions) {
			opts.Expires = time.Duration(60 * time.Second)
		},
	)

	if err != nil {
		log.Fatal("PresignPutObject")
	}

	uploadFileToS3(presignResult.URL, objectKey)
}

func uploadFileToS3(url string, objectKey string) {
	uploadFile, err := os.Open(fmt.Sprintf("./%s", objectKey))
	if err != nil {
		log.Fatal("os.Open()")
	}
	defer uploadFile.Close()

	info, err := uploadFile.Stat()
	if err != nil {
		log.Fatal("Stat()")
	}

	putRequest, err := http.NewRequest("PUT", url, uploadFile)
	if err != nil {
		log.Fatal("http.NewRequest")
	}

	putRequest.ContentLength = info.Size()
	// putRequest.Header.Add("x-amx-acl", "public-read") //not working either

	response, err := http.DefaultClient.Do(putRequest)
	if err != nil {
		log.Fatal("http.Defaultclient.Do()")
	}

	bytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal("io.ReadAll()")
	}

	log.Println("resonse body: ", string(bytes))
}
