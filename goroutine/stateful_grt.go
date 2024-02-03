package main

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"
)

type reader struct {
	key        int
	outChannel chan int
}

type writer struct {
	key        int
	value      int
	outChannel chan bool
}

func TestRunner() {
	var readCounter uint64 = 0
	var writeCounter uint64 = 0

	readChannel := make(chan *reader)
	writeChannel := make(chan *writer)

	go func() {
		privateState := make(map[int]int)
		for {
			select {
			case read := <-readChannel:
				read.outChannel <- privateState[read.key]
			case write := <-writeChannel:
				privateState[write.key] = write.value
				write.outChannel <- true
			}
		}
	}()
	fmt.Println("state handler called")

	for i := 0; i < 100; i++ {
		go func() {
			for {
				read := &reader{
					key:        rand.Intn(5),
					outChannel: make(chan int),
				}
				readChannel <- read
				<-read.outChannel

				atomic.AddUint64(&readCounter, 1)
				time.Sleep(time.Millisecond)
			}
		}()
	}
	fmt.Println("reader called")

	for j := 0; j < 10; j++ {
		go func() {
			for {
				write := &writer{
					key:        rand.Intn(5),
					value:      rand.Intn(10),
					outChannel: make(chan bool),
				}
				writeChannel <- write
				<-write.outChannel

				atomic.AddUint64(&writeCounter, 1)
				time.Sleep((time.Millisecond))
			}
		}()
	}
	fmt.Println("writer called")

	time.Sleep(time.Second)
	resultRead := atomic.LoadUint64(&readCounter)
	resultWrite := atomic.LoadUint64(&writeCounter)
	fmt.Printf("read: %d, write: %d\n", resultRead, resultWrite)
}
