package chat_test

// Note: The go test -testwork flag preserves the TestScript temporary directory.

import (
	"os"
	"testing"

	chat "github.com/ivanfetch/chatserver"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"chatserver": chat.RunCLIWithoutWaitingForExit,
	}))
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
	})
}
