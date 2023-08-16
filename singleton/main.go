package main

import (
	"fmt"
	"sync"
)

var mutexHandle = &sync.Mutex{}

type Singleton struct {
	counter int
}

func (instance *Singleton) incr() {
	instance.counter += 1
}

func (instance *Singleton) unwrap() int {
	return instance.counter
}

var instance *Singleton

func getInstance() *Singleton {
	if instance != nil {
		return instance
	}

	mutexHandle.Lock()
	defer mutexHandle.Unlock()

	if instance == nil {
		fmt.Printf("getInstance] creating\n")
		instance = &Singleton{
			counter: 0,
		}
	}

	return instance
}

func main() {
	mainInstance := getInstance()

	mainInstance.incr()

	subInstance := getInstance()
	subInstance.incr()

	reader := getInstance()

	fmt.Printf("reader: %d\n", reader.unwrap())
}
