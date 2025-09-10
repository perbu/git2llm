package main

import (
	"fmt"
	"os"
)

// main is the entry point of the application
func main() {
	fmt.Println("Hello, World!")

	if len(os.Args) > 1 {
		fmt.Printf("Arguments: %v\n", os.Args[1:])
	}
}

// helper function for testing
func helper(input string) string {
	return fmt.Sprintf("processed: %s", input)
}
