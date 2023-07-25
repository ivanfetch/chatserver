package main

import (
	"os"

	chat "github.com/ivanfetch/chatserver"
)

func main() {
	os.Exit(chat.RunCLI())
}
