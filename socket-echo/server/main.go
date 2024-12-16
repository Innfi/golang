package main

import (
	"bufio"
	"fmt"
	"net"
)

type Context struct {
	conn net.Conn
	ch   chan []byte
}

var (
	contexts   = make(map[*Context]bool)
	register   = make(chan *Context)
	unregister = make(chan *Context)
)

func main() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer listener.Close()

	go handleMessage()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err.Error())
			continue
		}

		context := &Context{conn: conn, ch: make(chan []byte)}
		register <- context
		go handleContext(context)
	}
}

func handleMessage() {
	for {
		select {
		case context := <-register:
			fmt.Println("received register by channel")
			contexts[context] = true
		case context := <-unregister:
			fmt.Println("received unregister by channel")
			if _, ok := contexts[context]; ok {
				delete(contexts, context)
				close(context.ch)
			}
		}
	}
}

func handleContext(context *Context) {
	defer func() {
		unregister <- context
		context.conn.Close()
	}()

	reader := bufio.NewReader(context.conn)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		fmt.Printf("message: %s\n", message)

		context.conn.Write([]byte(message))
	}
}
