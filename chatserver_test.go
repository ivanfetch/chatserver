package chatserver_test

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ivanfetch/chatserver"
)

// readStringWithTimeout reads the supplied bufio.Reader and returns the
// resulting string, or timedOut set to true if the Reader returned an
// "deadline exceeded" error. The deadline; timeout is managed outside of this
// function, via previously calling SetReadDeadline() on a net.Conn type,
// then creating a bufio.Reader from that net.Conn.
func readStringWithTimeout(r *bufio.Reader) (result string, timedOut bool, err error) {
	result, err = r.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) { // underlying net.Conn deadline reached, via SetReadDeadline()
			return result, true, nil
		}
		return "", false, err
	}
	return result, false, nil
}

func TestChatSession(t *testing.T) {
	t.Parallel()
	// Define a substring that is expected in each line of chat-server output.
	expectedClient1Output := []string{
		"hello there",
		"\n",
		"Anything you type will be sent to all other users",
		"enter /help for a list",
		"known as \"ivan1\"",
		"known as \"ivan2\"",
		"> first message",
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	t.Log("starting chat server")
	server, err := chatserver.NewServer()
	if err != nil {
		t.Fatal(err)
	}
	server.Run()
	client1, err := net.Dial("tcp", server.GetListenAddress())
	if err != nil {
		t.Fatalf("client 1 cannot connect to chat server: %v", err)
	}
	client1.SetReadDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintln(client1, "/nick ivan1\n/nick ivan2\nfirst message")
	t.Log("starting to read from client 1")
	client1Reader := bufio.NewReader(client1)
	for _, want := range expectedClient1Output {
		got, timedOut, err := readStringWithTimeout(client1Reader)
		if err != nil {
			t.Fatalf("error while reading from client 1: %v", err)
		}
		if timedOut {
			t.Fatalf("timed out while reading from client 1, want substring %q", want)
		}
		if !strings.Contains(got, want) {
			t.Logf("got %q, want substring %q, while reading chat output from client 1", got, want)
		} else {
			t.Logf("matched substring %q from %q", want, got)
		}
	}
	t.Log("stopping chat server")
	server.InitiateShutdown(true)
	want := "the chat server is shutting down"
	got, timedOut, err := readStringWithTimeout(client1Reader)
	if err != nil {
		t.Fatalf("error while reading from client 1: %v", err)
	}
	if timedOut {
		t.Fatalf("timed out while reading from client 1, want substring %q", want)
	}
	if !strings.Contains(got, want) {
		t.Logf("got %q, want substring %q, while reading chat output from client 1", got, want)
	} else {
		t.Logf("matched substring %q from %q", want, got)
	}
	go server.WaitForExit() // Causes server.HasExited() == true
	for !server.HasExited() {
		select {
		case <-timeout.C:
			t.Fatal("tesst timed out")
		default:
			if server.HasExited() {
				t.Log("chat server has exited")
			}
		}
	}
}
