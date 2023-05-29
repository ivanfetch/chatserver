package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	log "github.com/sirupsen/logrus"
)

type connection struct {
	netConn  net.Conn // TCP connection
	nickname string
}

func newConnection(netConn net.Conn) *connection {
	return &connection{
		netConn: netConn,
	}
}

func (c connection) Close() {
	debugLog.Printf("closing connection %s", c.UniqueID())
	c.netConn.Close()
}

func (c connection) UniqueID() string {
	if c.nickname != "" {
		return fmt.Sprintf("%s-%s", c.nickname, c.netConn.RemoteAddr().String())
	}
	return c.netConn.RemoteAddr().String()
}

func (c connection) Nickname() string {
	if c.nickname != "" {
		return c.nickname
	}
	return c.netConn.RemoteAddr().String()
}

type message struct {
	connection *connection // who originated the message
	text       string
}

// String formats the message sender and text.
func (m message) String() string {
	return fmt.Sprintf("%s: %s\n", m.connection.Nickname(), m.text)
}

var debugLog *log.Logger = log.New()

// startConnectionAndMessageManager runs a goroutine that tracks connections to the chat
// server, and processes messages sent by clients.
func startConnectionAndMessageManager(addConnCh, removeConnCh chan *connection, addMessageCh chan message) {
	var connections []*connection
	// TODO: Add context to respond to signals and cleanup connections?
	go func() {
		debugLog.Println("starting connection manager")
		for {
			select {
			case newConn := <-addConnCh:
				debugLog.Printf("adding connection from %s", newConn.UniqueID())
				connections = append(connections, newConn)
			case removeConn := <-removeConnCh:
				debugLog.Printf("removing connection %s", removeConn.UniqueID)
				newConnections := make([]*connection, len(connections)-1)
				newI := 0
				for _, conn := range connections {
					if conn.UniqueID() != removeConn.UniqueID() {
						newConnections[newI] = conn
						newI++
					}
				}
				connections = newConnections
			case newMessage := <-addMessageCh:
				debugLog.Printf("processing new message from %s: %s", newMessage.connection.Nickname(), newMessage.text)
				go broadcast(newMessage, connections, removeConnCh)
			default:
				// do nothing
			}
		}
	}()
}

// processInput scans a chat connection for text and hands chat commands or
// messages.
func processInput(con *connection, addMessageCh chan message, removeConnCh chan *connection) {
	log.Println("saying hello to our connection")
	fmt.Fprintln(con.netConn, `Well hello there!

Anything you type to me will be displayed in the output of this chat-server.
`)
	scanner := bufio.NewScanner(con.netConn)
	for scanner.Scan() {
		line := scanner.Text()
		debugLog.Printf("received from %s: %s", con.UniqueID(), line)
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
			text:       line,
			connection: con,
		}
	}
	err := scanner.Err()
	if err != nil {
		debugLog.Println("while reading from %s: %v", con.UniqueID(), err)
	}
}

func broadcast(msg message, allConnections []*connection, removeConnCh chan *connection) {
	debugLog.Printf("broadcasting to %d connections: %s", len(allConnections), msg)
	for _, con := range allConnections {
		if con.UniqueID() == msg.connection.UniqueID() {
			_, err := fmt.Fprintf(con.netConn, "> %s\n", msg.text)
			if err != nil {
				debugLog.Printf("error writing to %v: %v", con.netConn, err)
				removeConnCh <- con
			}
			continue
		}
		_, err := fmt.Fprintf(con.netConn, msg.String())
		if err != nil {
			debugLog.Printf("error writing to %v: %v", con.netConn, err)
			removeConnCh <- con
		}
	}
}

func processCommands(input string, con *connection) (clientIsLeaving bool) {
	fields := strings.Fields(input)
	switch strings.ToLower(fields[0][1:]) { // first word minus its first character(/)
	case "quit", "exit", "leave":
		fmt.Fprintf(con.netConn, "You're leaving? Ok - have a nice day. :)\n")
		debugLog.Printf("client %s has signed off", con.UniqueID())
		return true
	case "nickname", "nick":
		if len(fields) > 1 && fields[1] != "" {
			debugLog.Printf("changing nickname from %q to %q", con.Nickname(), fields[1])
			con.nickname = fields[1]
		}
	case "help":
		fmt.Fprintf(con.netConn, `Available commands are:
/quit|leave|exit - Sign off and disconnect from chat.
/nick|nickname - Set your nickname, to be displayed with your messages.
`)
	default:
		fmt.Fprintf(con.netConn, "%q is an invalid command, please use /help for a list of commands.\n", fields[0])
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
	addConnCh := make(chan *connection)
	removeConnCh := make(chan *connection)
	addMessageCh := make(chan message)
	startConnectionAndMessageManager(addConnCh, removeConnCh, addMessageCh)
	debugLog.Println("waiting for new connections")
	for {
		netConn, err := l.Accept()
		if err != nil {
			debugLog.Printf("while accepting a connection: %v", err)
			continue
		}
		conn := newConnection(netConn)
		addConnCh <- conn
		go processInput(conn, addMessageCh, removeConnCh)
	}
}
