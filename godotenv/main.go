package main

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type EnvStorage struct {
	TestValue1 string
	TestValue2 int64
}

func InitEnvStorage() *EnvStorage {
	err := godotenv.Load("./.env")
	if err != nil {
		log.Fatal("failed to read env")
	}

	return &EnvStorage{
		TestValue1: os.Getenv("TEST_VALUE1"),
		TestValue2: toIntParam(os.Getenv("TEST_VALUE2")),
	}
}

func toIntParam(input string) int64 {
	intParam, err := strconv.ParseInt(input, 10, 32)
	if err != nil {
		log.Fatal("failed to parse int value1")
	}

	return intParam
}

func main() {
	storage := InitEnvStorage()

	log.Default().Println("testvalue1: ", storage.TestValue1)
	log.Default().Println("testvalue2: ", storage.TestValue2)
}
