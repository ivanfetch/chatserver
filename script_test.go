package chat_test

// Note: The go test -testwork flag preserves the TestScript temporary directory.

import (
	"os"
	"testing"

	chat "github.com/ivanfetch/chatserver"
	"github.com/rogpeppe/go-internal/testscript"
)

var testScriptSetup func(*testscript.Env) error = func(e *testscript.Env) error {
	// default to use a random chat server TCP listener port.
	e.Vars = append(e.Vars, "CHATSERVER_LISTEN_ADDRESS=:0")
	return nil
}

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"chatserver": chat.RunCLI,
	}))
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Setup: testScriptSetup,
		Dir:   "testdata/script",
	})
}
