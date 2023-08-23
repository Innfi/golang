package main

import (
	"fmt"
	"sync"
	"time"
)

type ChannelPayload struct {
	StrValue1     string
	StrValue2     string
	IntValue      uint32
	IntArrayValue []int
}

func say(wait *sync.WaitGroup, c chan ChannelPayload, s string) {
	payload := ChannelPayload{
		StrValue1:     "string 1",
		StrValue2:     "string 2",
		IntValue:      33,
		IntArrayValue: []int{9, 1, 2, 3},
	}
	fmt.Printf("intArrayValue: %d\n", payload.IntArrayValue[0])
	c <- payload

	defer wait.Done()
	for i := 0; i < 5; i++ {
		time.Sleep(time.Millisecond * 100)
		fmt.Println(s)
	}
}

func main() {
	c := make(chan ChannelPayload)
	var wait sync.WaitGroup
	wait.Add(2) //less than number of goroutines result in early exit

	go say(&wait, c, "async 1")
	// go say(&wait, c, "async 2")
	// go say(&wait, c, "async 3")

	go func(msg string) {
		defer wait.Done()
		fmt.Println(msg)
	}("anonymous func with goroutine")

	resultPayload := <-c

	fmt.Println(resultPayload.StrValue1)
	fmt.Println(resultPayload.StrValue2)
	fmt.Println(resultPayload.IntValue)

	for index := range resultPayload.IntArrayValue {
		fmt.Println(resultPayload.IntArrayValue[index])
	}

	wait.Wait()
}
