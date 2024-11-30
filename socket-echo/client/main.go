package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer conn.Close()

	for i := 1; i < 10; i++ {
		_, err = conn.Write([]byte(fmt.Sprintf("hello server%d\n", i)))
		if err != nil {
			fmt.Println(err)
			return
		}

		time.Sleep(time.Second * 3)
	}
}
