package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:6379")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer conn.Close()

	reader := bufio.NewReader(os.Stdin)
	go handleResponse(conn)

	for {
		stdinBuf, readErr := reader.ReadString('\n')
		if readErr != nil {
			fmt.Printf("err: %s\n", readErr)
			break
		}
		if len(stdinBuf) <= 2 {
			break
		}

		conn.Write([]byte(stdinBuf))
	}
}

func handleResponse(conn net.Conn) {
	fmt.Printf("handleResponse] started\n")
	respBuf := make([]byte, 100)

	for {
		_, respErr := conn.Read(respBuf)
		if respErr != nil {
			fmt.Printf("respErr: %s\n", respErr)
			break
		}

		fmt.Printf("handleResponse] message from the server: %s\n", string(respBuf))
	}
}
