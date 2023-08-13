package main

import (
	"fmt"

	"local-module.com/src"

	"local-module.com/tests"
)

func main() {
	fmt.Printf("%s\n", src.InitialFunction())
	fmt.Printf("%s\n", tests.InitialTests())
}
