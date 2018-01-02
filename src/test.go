package main

import (
	"fmt"
)

func test() {
	a := 1
	defer fmt.Println(a)

	a = 10
}
func main() {
	test()
	
}