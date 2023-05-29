package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	log "github.com/sirupsen/logrus"
)

type message struct {
	sender, text string
}

// String formats the message sender and text.
func (m message) String() string {
	return fmt.Sprintf("%s: %s\n", m.sender, m.text)
}

var debugLog *log.Logger = log.New()

// startConnectionAndMessageManager runs a goroutine that tracks connections to the chat
// server, and processes messages sent by clients.
func startConnectionAndMessageManager(addConnCh, removeConnCh chan net.Conn, addMessageCh chan message) {
	var connections []net.Conn
	// TODO: Add context to respond to signals and cleanup connections?
	go func() {
		debugLog.Println("starting connection manager")
		for {
			select {
			case newConn := <-addConnCh:
				debugLog.Printf("adding connection from %s", newConn.RemoteAddr())
				connections = append(connections, newConn)
			case removeConn := <-removeConnCh:
				debugLog.Printf("removing connection %s", removeConn.RemoteAddr())
				newConnections := make([]net.Conn, len(connections)-1)
				newI := 0
				for _, conn := range connections {
					if conn != removeConn {
						newConnections[newI] = conn
						newI++
					}
				}
				connections = newConnections
			case newMessage := <-addMessageCh:
				debugLog.Printf("processing new message from %s: %s", newMessage.sender, newMessage.text)
				go broadcast(newMessage, connections, removeConnCh)
			default:
				// do nothing
			}
		}
	}()
}

// processInput scans a chat connection for text and hands chat commands or
// messages.
func processInput(con net.Conn, addMessageCh chan message, removeConnCh chan net.Conn) {
	log.Println("saying hello to our connection")
	fmt.Fprintln(con, `Well hello there!

Anything you type to me will be displayed in the output of this chat-server.
`)
	scanner := bufio.NewScanner(con)
	for scanner.Scan() {
		line := scanner.Text()
		debugLog.Printf("received from %s: %s", con.RemoteAddr(), line)
		if strings.HasPrefix(line, "/") {
			exiting := processCommands(line, con)
			if exiting {
				con.Close()
				removeConnCh <- con
				return
			}
			continue
		}
		addMessageCh <- message{
			text:   line,
			sender: con.RemoteAddr().String(),
		}
	}
	err := scanner.Err()
	if err != nil {
		debugLog.Println("while reading from %s: %v", con.RemoteAddr(), err)
	}
}

func broadcast(msg message, allConnections []net.Conn, removeConnCh chan net.Conn) {
	debugLog.Printf("broadcasting to %d connections: %s", len(allConnections), msg)
	for _, con := range allConnections {
		if con.RemoteAddr().String() == msg.sender {
			_, err := fmt.Fprintf(con, "> %s\n", msg.text)
			if err != nil {
				debugLog.Printf("error writing to %v: %v", con, err)
				removeConnCh <- con
			}
			continue
		}
		_, err := fmt.Fprintf(con, msg.String())
		if err != nil {
			debugLog.Printf("error writing to %v: %v", con, err)
			removeConnCh <- con
		}
	}
}

func processCommands(input string, con net.Conn) (clientIsLeaving bool) {
	fields := strings.Fields(input)
	switch strings.ToLower(fields[0][1:]) { // first word minus its first character(/)
	case "quit", "exit", "leave":
		fmt.Fprintf(con, "You're leaving? Ok - have a nice day. :)\n")
		debugLog.Printf("client %s has signed off", con.RemoteAddr())
		return true
	case "help":
		fmt.Fprintf(con, `Available commands are:
/quit|leave|exit - Sign off and disconnect from chat.
/nick|nickname - Set your nickname, to be displayed with your messages.
`)
	default:
		fmt.Fprintf(con, "%q is an invalid command, please use /help for a list of commands.\n", fields[0])
	}
	return false
}

func main() {
	debugLog.SetFormatter(&log.TextFormatter{
		PadLevelText: true,
	})

	const listenAddress = ":8080"
	l, err := net.Listen("tcp", listenAddress)
	if err != nil {
		debugLog.Fatalf("cannot listen: %v", err)
	}
	debugLog.Printf("listening for connections on %s", listenAddress)
	addConnCh := make(chan net.Conn)
	removeConnCh := make(chan net.Conn)
	addMessageCh := make(chan message)
	startConnectionAndMessageManager(addConnCh, removeConnCh, addMessageCh)
	debugLog.Println("waiting for new connections")
	for {
		con, err := l.Accept()
		if err != nil {
			debugLog.Printf("while accepting a connection: %v", err)
			continue
		}
		addConnCh <- con
		go processInput(con, addMessageCh, removeConnCh)
	}
}
