package chat

import (
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
)

// This test is in the chat package so it can inspect private members of the
func TestNewServerFromArgs(t *testing.T) {
	t.Parallel()
	server, err := NewServerFromArgs([]string{"-d", "-l", ":1234"})
	if err != nil {
		t.Fatal(err)
	}
	got := server.log.GetLevel()
	want := logrus.DebugLevel
	if want != got {
		t.Fatalf("want log level %v, got %v", want, got)
	}

	wantListenAddress := ":1234"
	if wantListenAddress != server.listenAddress {
		t.Fatalf("want server listen address %q, got %q", wantListenAddress, server.listenAddress)
	}
}

func TestNewServerFromArgsWithInvalidListenAddress(t *testing.T) {
	t.Parallel()
	_, err := NewServerFromArgs([]string{"-l", "hostnamewithnoport"})
	if err == nil {
		t.Fatal("did not receive an expected error")
	}
}

func ExampleNewServerFromArgs() {
	server, err := NewServerFromArgs([]string{"--debug-logging", "--listen-address", ":9999"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("This server has debugging enabled and is listening on %s", server.GetListenAddress())
	// Output:
	// This server has debugging enabled and is listening on :9999
}
