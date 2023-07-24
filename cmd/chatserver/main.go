package main

import (
	"fmt"
	"os"

	chat "github.com/ivanfetch/chatserver"
)

func main() {
	err := chat.CreateAndRun()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
