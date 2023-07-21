package chatserver_test

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/ivanfetch/chatserver"
)

func TestChatSession(t *testing.T) {
	t.Parallel()
	finishByTime := time.After(5 * time.Second)
	done := make(chan bool)
	go func() {
		t.Log("starting chat server")
		server, err := chatserver.RunWithoutWaitingForExit()
		if err != nil {
			t.Fatal(err)
		}
		client1, err := net.Dial("tcp", server.GetListenAddress())
		if err != nil {
			t.Fatalf("client 1 cannot connect to chat server: %v", err)
		}
		client1Reader := bufio.NewReader(client1)
		client1Str, err := client1Reader.ReadString('\n')
		if err != nil {
			t.Fatalf("client 1 cannot read a string from its chat-server connection: %v", err)
		}
		t.Logf("client 1 read from chat server: %s\n", client1Str)
		time.Sleep(15 * time.Second) // don't shutdown the server while we're using it
		t.Log("stopping chat server")
		server.InitiateShutdown()
		t.Log("Waiting for shutdown\n")
		server.WaitForExit()
		return
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatalf("Test didn't finish in time %s", timeoutDuration.String())
	case <-done:
	}
}
