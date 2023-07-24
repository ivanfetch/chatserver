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
// resulting string, or calls t.Fatal() if the Reader returned an
// "deadline exceeded" error. The deadline; timeout is managed outside of this
// function, via previously calling SetReadDeadline() on a net.Conn type,
// then creating a bufio.Reader from that net.Conn.
func readStringWithTimeout(t *testing.T, r *bufio.Reader) (result string, err error) {
	result, err = r.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) { // underlying net.Conn deadline reached, via SetReadDeadline()
			t.Fatal("exceeded timeout while waiting to read")
		}
		return "", err
	}
	return result, nil
}

func matchSubstringsOrFailTestWithTimeout(t *testing.T, r *bufio.Reader, expectSubstrs ...string) {
	for _, expectSubstr := range expectSubstrs {
		result, err := readStringWithTimeout(t, r)
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(result, expectSubstr) {
			t.Fatalf("expected substring %q not found in %q", expectSubstr, result)
		}
		t.Logf("matched substring %q in %q", expectSubstr, result)
	}
}

func TestChatSession(t *testing.T) {
	t.Parallel()
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
	t.Log("starting to read from client 1")
	client1Reader := bufio.NewReader(client1)
	matchSubstringsOrFailTestWithTimeout(t, client1Reader,
		"hello there",
		"\n",
		"Anything you type will be sent to all other users",
		"enter /help for a list",
		"has joined the chat",
	)
	if err != nil {
		t.Fatalf("error while reading from client 1: %v", err)
	}

	client2, err := net.Dial("tcp", server.GetListenAddress())
	if err != nil {
		t.Fatalf("client 2 cannot connect to chat server: %v", err)
	}
	client2.SetReadDeadline(time.Now().Add(3 * time.Second))
	t.Log("starting to read from client 2")
	client2Reader := bufio.NewReader(client2)
	matchSubstringsOrFailTestWithTimeout(t, client2Reader,
		"hello there",
		"\n",
		"Anything you type will be sent to all other users",
		"enter /help for a list",
		"has joined the chat",
	)
	if err != nil {
		t.Fatalf("error while reading from client 2: %v", err)
	}

	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "has joined the chat") // client2 joining

	fmt.Fprintln(client1, "/nick client1")
	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "now known as \"client1\"")
	matchSubstringsOrFailTestWithTimeout(t, client2Reader, "now known as \"client1\"")

	fmt.Fprintln(client2, "/nick client2")
	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "now known as \"client2\"")
	matchSubstringsOrFailTestWithTimeout(t, client2Reader, "now known as \"client2\"")

	fmt.Fprintln(client1, "first message")
	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "> first message")
	matchSubstringsOrFailTestWithTimeout(t, client2Reader, "client1> first message")

	fmt.Fprintln(client2, "/quit")
	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "client2 has left the chat")

	fmt.Fprintln(client1, "/help")
	matchSubstringsOrFailTestWithTimeout(t, client1Reader,
		"Available commands are",
		"Sign off and disconnect",
		"Set your nickname",
	)

	t.Log("stopping chat server")
	server.InitiateShutdown(true)
	matchSubstringsOrFailTestWithTimeout(t, client1Reader, "the chat server is shutting down")
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
