package main

import (
	"fmt"
	"os"

	"github.com/ivanfetch/chatserver"
)

func main() {
	err := chatserver.CreateAndRun()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
