package chat_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	chat "github.com/ivanfetch/chatserver"
)

type chatClient struct {
	name          string        // differentiate connections for test feedback
	netConn       *net.Conn     // TCP connection
	netConnReader *bufio.Reader // to read strings from netConn
}

func newChatClient(name, connectAddress string) (*chatClient, error) {
	netConn, err := net.Dial("tcp", connectAddress)
	if err != nil {
		return nil, fmt.Errorf("%s %v", name, err)
	}
	// This timeout is used by the client readStringWithTimeout method.
	err = netConn.SetReadDeadline(time.Now().Add(getOSSpecificNetworkReadTimeout()))
	if err != nil {
		return nil, fmt.Errorf("%s %v", name, err)
	}
	netConnReader := bufio.NewReader(netConn)
	return &chatClient{
		name:          name,
		netConn:       &netConn,
		netConnReader: netConnReader,
	}, nil
}

func (c *chatClient) matchSubstringsOrFailTestWithTimeout(t *testing.T, expectSubstrs ...string) {
	for _, expectSubstr := range expectSubstrs {
		result, err := c.readStringWithTimeout(t, expectSubstr)
		if err != nil {
			t.Fatalf("%s %v", c.name, err)
		}
		if !strings.Contains(result, expectSubstr) {
			t.Fatalf("client %s expected substring %q was not found in %q", c.name, expectSubstr, result)
		}
		t.Logf("client %s matched substring %q in %q", c.name, expectSubstr, result)
	}
}

// readStringWithTimeout reads the supplied bufio.Reader and returns the
// resulting string, or calls t.Fatal() if the Reader returned an
// "deadline exceeded" error. The deadline; timeout is managed outside of this
// function, via previously calling SetReadDeadline() on a net.Conn type,
// then creating a bufio.Reader from that net.Conn.
// The `expected` parameter is only used to provide context in the error message
// when failing the test.
func (c *chatClient) readStringWithTimeout(t *testing.T, expected string) (result string, err error) {
	result, err = c.netConnReader.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) { // underlying net.Conn deadline reached, via SetReadDeadline()
			t.Fatalf("client %s exceeded timeout while waiting to read expected string %q", c.name, expected)
		}
		return "", fmt.Errorf("client %s %v", c.name, err)
	}
	return result, nil
}

// getOSSpecificPerTestTimeout returns how long a single test should be given
// to complete, depending on which operating system is executing the test.
func getOSSpecificPerTestTimeout() time.Duration {
	switch runtime.GOOS {
	case "windows":
		// WIndows Github runners take 3-5X longer to execute `go test`
		return 60 * time.Second
	default:
		return 5 * time.Second
	}
}

// getOSSpecificNetworkReadTimeout returns how long a network read operation
// should be given to complete, depending on which operating system is executing the test.
func getOSSpecificNetworkReadTimeout() time.Duration {
	switch runtime.GOOS {
	case "windows":
		// WIndows Github runners take 3-5X longer to execute `go test`
		return 15 * time.Second
	default:
		return 3 * time.Second
	}
}

func TestChatSession(t *testing.T) {
	t.Parallel()
	timeout := time.NewTimer(getOSSpecificPerTestTimeout())
	defer timeout.Stop()
	t.Log("starting chat server")
	var err error
	var server *chat.Server
	if testing.Verbose() {
		server, err = chat.NewServer(chat.WithDebugLogging(), chat.WithListenAddress(":8888"))
	} else {
		server, err = chat.NewServer(chat.WithLogWriter(io.Discard), chat.WithListenAddress(":8888")) // discard non-debug status output
	}
	if err != nil {
		t.Fatal(err)
	}
	err = server.ListenAndServe()
	if err != nil {
		t.Fatal(err)
	}
	client1, err := newChatClient("1", server.GetListenAddress())
	if err != nil {
		t.Error(err)
	}
	t.Log("starting to read from client 1")
	client1.matchSubstringsOrFailTestWithTimeout(t,
		"hello there",
		"\n",
		"Anything you type will be sent to all other users",
		"enter /help for a list",
		"has joined the chat",
	)

	client2, err := newChatClient("2", server.GetListenAddress())
	if err != nil {
		t.Error(err)
	}
	t.Log("starting to read from client 2")
	client2.matchSubstringsOrFailTestWithTimeout(t,
		"hello there",
		"\n",
		"Anything you type will be sent to all other users",
		"enter /help for a list",
		"has joined the chat",
	)

	client1.matchSubstringsOrFailTestWithTimeout(t, "has joined the chat") // client2 joining

	fmt.Fprintln(*client1.netConn, "/nick client1")
	client1.matchSubstringsOrFailTestWithTimeout(t, "now known as \"client1\"")
	client2.matchSubstringsOrFailTestWithTimeout(t, "now known as \"client1\"")

	fmt.Fprintln(*client2.netConn, "/nick client2")
	client1.matchSubstringsOrFailTestWithTimeout(t, "now known as \"client2\"")
	client2.matchSubstringsOrFailTestWithTimeout(t, "now known as \"client2\"")

	fmt.Fprintln(*client1.netConn, "first message")
	client1.matchSubstringsOrFailTestWithTimeout(t, "> first message")
	client2.matchSubstringsOrFailTestWithTimeout(t, "client1> first message")

	fmt.Fprintln(*client2.netConn, "/quit")
	client1.matchSubstringsOrFailTestWithTimeout(t, "client2 has left the chat")

	fmt.Fprintln(*client1.netConn, "/help")
	client1.matchSubstringsOrFailTestWithTimeout(t,
		"Available commands are",
		"Sign off and disconnect",
		"Set your nickname",
	)

	t.Log("stopping chat server")
	server.InitiateShutdown()
	client1.matchSubstringsOrFailTestWithTimeout(t, "the chat server is shutting down")
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

func ExampleNewServer() {
	server, err := chat.NewServer(chat.WithDebugLogging(), chat.WithListenAddress(":9999"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("This server has debugging enabled and is listening on %s", server.GetListenAddress())
	// Output:
	// This server has debugging enabled and is listening on :9999
}
