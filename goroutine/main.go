package main

import (
	"fmt"
	"math/rand"
	"runtime"
)

func main() {
	var result int64
	numbers := generateList(1e7)

	numCpu := runtime.NumCPU()
	fmt.Printf("cpu count: %d\n", numCpu)

	totalNumbers := len(numbers)
	blockRange := totalNumbers / numCpu

	c := make(chan int)

	for g := 0; g < numCpu; g++ {
		start := g * blockRange
		end := start + blockRange
		if g == numCpu-1 {
			end = totalNumbers
		}

		go add(numbers[start:end], c)
	}

	for j := 0; j < numCpu; j++ {
		result += int64(<-c)
	}

	fmt.Printf("result: %d\n", result)
}

func generateList(len int) []int {
	numbers := make([]int, len)
	for i := 0; i < len; i++ {
		numbers[i] = rand.Intn(len)
	}

	return numbers
}

func add(numbers []int, c chan int) {
	var v int
	for _, n := range numbers {
		v += n
	}

	c <- v
}
