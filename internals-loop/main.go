package main

import "fmt"

func LoopingReference() {
	var out []*int

	for i := 0; i < 3; i++ {
		// so the address of the loop variable i never changes...
		out = append(out, &i)
	}

	fmt.Println("value: ", *out[0], *out[1], *out[2])
	fmt.Println("addr: ", out[0], out[1], out[2])
}

// TODO: goroutine, defer, recover

func main() {
	fmt.Println("start from here")

	LoopingReference()
}
