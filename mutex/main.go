package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	shared_state := make(map[int]int)
	mutex := &sync.Mutex{}

	var reader uint64 = 0
	var writer uint64 = 0

	for i := 0; i < 100; i++ {
		go func() {
			total := 0
			for {
				seed := rand.Intn(5)
				mutex.Lock()
				total += shared_state[seed]
				mutex.Unlock()
				atomic.AddUint64(&reader, 1)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	for j := 0; j < 10; j++ {
		go func() {
			for {
				key := rand.Intn(5)
				val := rand.Intn(100)
				mutex.Lock()
				shared_state[key] = val
				mutex.Unlock()
				atomic.AddUint64(&writer, 1)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	time.Sleep(time.Second)

	read_result := atomic.LoadUint64(&reader)
	write_result := atomic.LoadUint64(&writer)
	fmt.Printf("read: %d, write: %d\n", read_result, write_result)

	if !mutex.TryLock() {
		fmt.Println("lock failed")
		return
	}
	fmt.Println("shared_state: ", shared_state)
	mutex.Unlock()
}
