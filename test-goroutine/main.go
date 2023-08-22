package main

import (
	"fmt"
	"sync"
	"time"
)

func say(wait *sync.WaitGroup, s string) {
	defer wait.Done()
	for i := 0; i < 5; i++ {
		time.Sleep(time.Millisecond * 100)
		// fmt.Println(s, "***", i)
		fmt.Println(s)
	}
}

func main() {
	var wait sync.WaitGroup
	wait.Add(4) //less than number of goroutines result in early exit

	go say(&wait, "async 1")
	go say(&wait, "async 2")
	go say(&wait, "async 3")

	go func(msg string) {
		defer wait.Done()
		fmt.Println(msg)
	}("anonymous func with goroutine")

	wait.Wait()
}
