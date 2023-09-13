package main

import (
	"context"
	"io"
	"log"
	"net/http"
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

	log.Println("url: ", presignResult.URL)

	filePath := "./test.png"
	uploadFile, err := os.Open(filePath)
	if err != nil {
		log.Fatal("failed to open the file to upload")
	}
	defer uploadFile.Close()

	info, err := uploadFile.Stat()
	if err != nil {
		log.Fatal("failed to stat file")
	}

	putResponse, err := PutFile(presignResult.URL, info.Size(), uploadFile)
	if err != nil {
		log.Fatal("failed to upload file")
	}

	log.Println("response code: ", putResponse.StatusCode)
}

func PutFile(url string, contentLength int64, body io.Reader) (resp *http.Response, err error) {
	putRequest, err := http.NewRequest("PUT", url, body)
	if err != nil {
		return nil, err
	}
	putRequest.ContentLength = contentLength
	return http.DefaultClient.Do(putRequest)
}
