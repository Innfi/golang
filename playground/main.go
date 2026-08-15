package main

import (
	"log"

	config "playground/config"
)

func main() {
	log.Println("main")

	reader := config.NewConfigReader()

	configSet := reader.Read()

	log.Printf("configSet: %v\n", configSet)
}
