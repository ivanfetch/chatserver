package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// connection holds information about a connection to the chat server.
type connection struct {
	netConn  net.Conn // TCP connection
	nickname string   // optional nickname, accompanying chat messages
}

// newConnection accepts a net.Conn and returns a type connection.
func newConnection(netConn net.Conn) *connection {
	return &connection{
		netConn: netConn,
	}
}

// Close wraps the Close method of the member net.Conn type.
func (c connection) Close() {
	debugLog.Printf("closing connection %s", c.UniqueID())
	c.netConn.Close()
}

// uniqueID returns a unique identifier for the chat connection,
// differentiating connections by using the source IP address and port, and
// an optional nickname.
func (c connection) UniqueID() string {
	if c.nickname != "" {
		return fmt.Sprintf("%s-%s", c.nickname, c.netConn.RemoteAddr().String())
	}
	return c.netConn.RemoteAddr().String()
}

// Nickname returns the nickname for the chat connection if it has been set,
// otherwise the TCP IP address and port are returned.
func (c connection) Nickname() string {
	if c.nickname != "" {
		return c.nickname
	}
	return c.netConn.RemoteAddr().String()
}

// message represents a message sent by a chat connection.
type message struct {
	connection *connection // who/what originated the message
	text       string
}

// String formats the message sender and text.
func (m message) String() string {
	return fmt.Sprintf("%s: %s\n", m.connection.Nickname(), m.text)
}

var debugLog *log.Logger = log.New()

// startConnectionAndMessageManager runs a goroutine that tracks connections to the chat
// server, and processes messages submitted by connections.
func startConnectionAndMessageManager(stopCh chan os.Signal, addConnCh, removeConnCh chan *connection, addMessageCh chan message) {
	var currentConnections []*connection
	var cleaningUp bool // Indicates goroutines are in the process of cleaning up, to exit
	go func() {
		debugLog.Println("starting connection manager")
		for {
			select {
			case newConn := <-addConnCh:
				debugLog.Printf("adding connection from %s", newConn.UniqueID())
				currentConnections = append(currentConnections, newConn)
			case removeConn := <-removeConnCh:
				debugLog.Printf("removing connection %s", removeConn.UniqueID)
				newConnections := make([]*connection, len(currentConnections)-1)
				newI := 0
				for _, conn := range currentConnections {
					if conn.UniqueID() != removeConn.UniqueID() {
						newConnections[newI] = conn
						newI++
					}
				}
				currentConnections = newConnections
			case newMessage := <-addMessageCh:
				debugLog.Printf("processing new message from %s: %s", newMessage.connection.Nickname(), newMessage.text)
				cleaningUp = true
				go broadcast(newMessage, currentConnections, removeConnCh, false)
			case stopSig := <-stopCh:
				debugLog.Printf("received signal %v, cleaning up and exiting", stopSig)
				go broadcast(message{
					text:       "You are being disconnected because the chat-server is exiting. So long...",
					connection: nil, // no connection because this is a system message
				}, currentConnections, removeConnCh, true)
			default:
				// Avoid blocking thecontaining loop
			}
			if cleaningUp && len(currentConnections) == 0 {
				break
			}
		}
		debugLog.Println("connection manager is finished")
		if cleaningUp {
			debugLog.Printf("cleanup complete")
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
				removeConnCh <- con
				con.Close()
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

func broadcast(msg message, allConnections []*connection, removeConnCh chan *connection, disconnect bool) {
	if disconnect {
		debugLog.Printf("broadcasting to, then disconnecting, %d connections: %s", len(allConnections), msg)
	} else {
		debugLog.Printf("broadcasting to %d connections: %s", len(allConnections), msg)
	}
	for _, con := range allConnections {
		var sender string
		if msg.connection == nil {
			sender = "system:"
		} else if msg.connection != nil && con.UniqueID() == msg.connection.UniqueID() {
			sender = ">" // this recipient is the message-sender
		} else {
			sender = fmt.Sprintf("%s:", msg.connection.nickname)
		}
		_, err := fmt.Fprintf(con.netConn, "%s %s\n", sender, msg.text)
		if err != nil {
			debugLog.Printf("error writing to %v: %v", con.netConn, err)
			removeConnCh <- con
			return
		}
		if disconnect {
			debugLog.Printf("disconnecting connection %s", con.UniqueID())
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

// createSignalHandler returns a channel that will be closed when SIGTERM
// and SIGINT signals are received.
// This channel can then be used to trigger cleanup and exit.
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
	stopCh := make(chan os.Signal, 2)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)
	addConnCh := make(chan *connection)
	removeConnCh := make(chan *connection)
	addMessageCh := make(chan message)
	startConnectionAndMessageManager(stopCh, addConnCh, removeConnCh, addMessageCh)
	debugLog.Println("waiting for new connections")
	for {
		netConn, err := l.Accept()
		if err != nil {
			debugLog.Printf("while accepting a connection: %v", err)
			continue
		}
		debugLog.Printf("accepted connection from %s", netConn.RemoteAddr())
		conn := newConnection(netConn)
		addConnCh <- conn
		go processInput(conn, addMessageCh, removeConnCh)
	}
}
