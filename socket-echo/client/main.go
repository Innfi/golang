package main

import (
	"fmt"
	"net"
	"time"
)

// func main() {
// 	id := uuid.New()
//
// 	conn, err := net.Dial("tcp", "localhost:8080")
// 	if err != nil {
// 		fmt.Println(err)
// 		return
// 	}
// 	defer conn.Close()
//
// 	go handleResponse(conn)
//
// 	index := 0
// 	for {
// 		input := fmt.Sprintf("%s] message %d\n", id.String(), index)
// 		conn.Write([]byte(input))
//
// 		index += 1
//
// 		time.Sleep(1 * time.Second)
// 	}
// }
//
// func handleResponse(conn net.Conn) {
// 	fmt.Printf("handleResponse] started\n")
//
// 	reader := bufio.NewReader(conn)
//
// 	for {
// 		message, err := reader.ReadString('\n')
// 		if err != nil {
// 			break
// 		}
//
// 		fmt.Printf("message from the server: %s\n", message)
// 	}
// }

func main() {
	conn, err := net.Dial("tcp", "localhost:9092")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer conn.Close()

	// reader := bufio.NewReader(os.Stdin)
	go handleResponse(conn)

	binInput := toByteArray()

	for {
		// stdinBuf, readErr := reader.ReadString('\n')
		// if readErr != nil {
		// 	fmt.Printf("err: %s\n", readErr)
		// 	break
		// }
		// if len(stdinBuf) <= 2 {
		// 	break
		// }

		conn.Write(binInput)

		time.Sleep(1 * time.Second)
	}
}

func handleResponse(conn net.Conn) {
	fmt.Printf("handleResponse] started\n")
	respBuf := make([]byte, 100)

	for {
		readLen, respErr := conn.Read(respBuf)
		if respErr != nil {
			fmt.Printf("respErr: %s\n", respErr)
			break
		}

		fmt.Printf("response: \n")
		for index, elem := range respBuf {
			if index >= readLen {
				break
			}

			fmt.Printf("%02x ", elem)
		}
		fmt.Printf("\n")
	}
}

func toByteArray() []byte {
	binInput := []byte{
		0x00, 0x00, 0x00, 0x23,
		0x00, 0x12,
		// 0x67, 0x4a,
		0x00, 0x04,
		0x4f, 0x74, 0xd2, 0x8b,
		0x00, 0x09,
		0x6b, 0x61, 0x66, 0x6b, 0x61, 0x2d, 0x63, 0x6c, 0x69,
		0x00,
		0x0a,
		0x6b, 0x61, 0x66, 0x6b, 0x61, 0x2d, 0x63, 0x6c, 0x69,
		0x04,
		0x30, 0x2e, 0x31, 0x00,
	}

	return binInput
}
