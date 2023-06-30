package main

// This multi-user chat server helps me learn concurrency.
// This is a learning project, please don't count on it improving, or even
// working entirely well.
// You can use nc or telnet to connect to localhost port 8080,
// and chat with this server.

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"
)

// connection holds information about a connection to the chat server.
type connection struct {
	netConn  net.Conn // TCP connection
	isClosed bool     // if netConn has been closed by us
	nickname string   // optional nickname, accompanying chat messages
}

// newConnection accepts a net.Conn and returns a type connection.
func newConnection(netConn net.Conn) *connection {
	return &connection{
		netConn: netConn,
	}
}

// Close wraps the Close method of the member net.Conn type to register that
// this connection has been closed.
func (c *connection) Close() {
	debugLog.Printf("closing connection %s", c.UniqueID())
	c.isClosed = true
	c.netConn.Close()
}

// Write wraps netCOnn.Write, only writing if isClosed == false
func (c *connection) Write(b []byte) (int, error) {
	if c.isClosed == true {
		debugLog.Printf("not writing to this closed connection %s: %s\n", c.UniqueID(), string(b))
		return 0, nil
	}
	return c.netConn.Write(b)
}

// Read wraps netCOnn.Read, only reading if isClosed == false
func (c *connection) Read(b []byte) (int, error) {
	if c.isClosed == true {
		debugLog.Printf("not reading from this closed connection %s\n", c.UniqueID())
		return 0, nil
	}
	return c.netConn.Read(b)
}

// uniqueID returns a unique identifier for the chat connection,
// differentiating connections by using the source IP address and port, and
// an optional nickname.
func (c connection) UniqueID() string {
	var remoteAddr string
	if c.netConn == nil {
		remoteAddr = "never connected"
	} else {
		remoteAddr = c.netConn.RemoteAddr().String()
	}
	if c.nickname != "" {
		return fmt.Sprintf("%s-%s", c.nickname, remoteAddr)
	}
	return remoteAddr
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

// startConnectionAccepter runs a goroutine that accepts connections to the
// chat server, and adds them to the connection-manager. A goroutine is also
// spawned to process any input from the connection.
func startConnectionAccepter(listener net.Listener, exitWG *sync.WaitGroup, stopCh <-chan struct{}, addConnCh, removeConnCh chan *connection, addMessageCh chan message) {
	go func() {
		defer exitWG.Done()
		debugLog.Println("starting connection accepter")
		for {
			select {
			case <-stopCh:
				debugLog.Println("no longer accepting connections")
				return
			default:
				netConn, err := listener.Accept()
				if err != nil {
					debugLog.Printf("while accepting a connection: %v", err)
					continue
				}
				debugLog.Printf("accepted connection from %s", netConn.RemoteAddr())
				conn := newConnection(netConn)
				addConnCh <- conn
				exitWG.Add(1)
				go processInput(conn, exitWG, addMessageCh, removeConnCh)
			}
		}
	}()
}

// startConnectionAndMessageManager runs a goroutine that tracks connections to the chat
// server, and broadcasts chat messages to all connections.
func startConnectionAndMessageManager(listener net.Listener, exitWG *sync.WaitGroup, stopCh <-chan struct{}, addConnCh, removeConnCh chan *connection, addMessageCh chan message) {
	var currentConnections []*connection
	var alreadyCleaningUp bool // Indicates goroutines are in the process of cleaning up, to exit
	go func() {
		defer exitWG.Done()
		debugLog.Println("starting connection manager")
		for {
			select {
			case newConn := <-addConnCh:
				debugLog.Printf("adding connection from %s", newConn.UniqueID())
				currentConnections = append(currentConnections, newConn)
			case removeConn := <-removeConnCh:
				if removeConn != nil {
					debugLog.Printf("closing and removing connection %s", removeConn.UniqueID())
					newConnections := make([]*connection, len(currentConnections)-1)
					newI := 0
					for _, conn := range currentConnections {
						if conn.UniqueID() != removeConn.UniqueID() {
							newConnections[newI] = conn
							newI++
						}
					}
					currentConnections = newConnections
					removeConn.Close()
				}
			case newMessage := <-addMessageCh:
				debugLog.Printf("broadcasting a new message from %s: %s", newMessage.connection.Nickname(), newMessage.text)
				exitWG.Add(1)
				go broadcast(newMessage, currentConnections, exitWG, removeConnCh, false)
			case <-stopCh:
				if !alreadyCleaningUp {
					debugLog.Printf("the connection manager is starting clean up")
					alreadyCleaningUp = true
					listener.Close()
					go broadcast(message{
						text:       "You are being disconnected because the chat-server is exiting. So long...",
						connection: &connection{nickname: "system"},
					}, currentConnections, exitWG, removeConnCh, true)
				}
			default:
				// Avoid blocking thecontaining loop
			}
			if alreadyCleaningUp && len(currentConnections) == 0 {
				break
			}
		}
		if alreadyCleaningUp {
			debugLog.Printf("the connection manager has finished cleaning up")
		}
		debugLog.Println("connection manager exiting")
	}()
}

// processInput scans a chat connection for text, and hands chat commands or
// messages.
func processInput(con *connection, exitWG *sync.WaitGroup, addMessageCh chan message, removeConnCh chan *connection) {
	defer exitWG.Done()
	debugLog.Printf("saying hello then reading input from connection %s", con.UniqueID())
	fmt.Fprintln(con, `Well hello there!

Anything you type will be sent to all other users of this chat server.
A line that begins with a slash (/) is considered a command - enter /help for a list of valid commands.
`)
	scanner := bufio.NewScanner(con)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		debugLog.Printf("received from %s: %s", con.UniqueID(), line)
		if strings.HasPrefix(line, "/") {
			exiting := processCommands(line, con)
			if exiting {
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
	if !con.isClosed && err != nil {
		debugLog.Printf("while reading from %s: %v", con.UniqueID(), err)
	}
	debugLog.Printf("input processing is exiting for connection %s", con.UniqueID())
}

// broadcast sends the specified message to all chat-server connections. If
// finalBroadcast is true then all connections will be disconnected and
// removed after the message has been sent to them.
func broadcast(msg message, allConnections []*connection, exitWG *sync.WaitGroup, removeConnCh chan *connection, finalBroadcast bool) {
	defer exitWG.Done()
	if finalBroadcast {
		debugLog.Printf("broadcasting to, then disconnecting, %d connections: %s", len(allConnections), msg)
	} else {
		debugLog.Printf("broadcasting to %d connections: %s", len(allConnections), msg)
	}
	for _, con := range allConnections {
		var sender string
		if msg.connection != nil && con.UniqueID() == msg.connection.UniqueID() {
			sender = ">" // this recipient is the message-sender
		} else {
			sender = fmt.Sprintf("%s:", msg.connection.nickname)
		}
		_, err := fmt.Fprintf(con, "%s %s\n", sender, msg.text)
		if err != nil {
			debugLog.Printf("error writing to %v: %v", con.netConn, err)
			removeConnCh <- con
			continue
		}
		if finalBroadcast {
			debugLog.Printf("disconnecting connection %s", con.UniqueID())
			removeConnCh <- con
		}
	}
}

// processCommands handles the specified string as a chat-server command. If
// the command would cause this connection to exit, clientIsLeaving will be
// set to true.
func processCommands(input string, con *connection) (clientIsLeaving bool) {
	fields := strings.Fields(input)
	switch strings.ToLower(fields[0][1:]) { // first word minus its first character(/)
	case "quit", "exit", "leave":
		fmt.Fprintf(con, "You're leaving? Ok - have a nice day. :)\n")
		debugLog.Printf("client %s has signed off", con.UniqueID())
		return true
	case "nickname", "nick":
		if len(fields) > 1 && fields[1] != "" {
			debugLog.Printf("changing nickname from %q to %q", con.Nickname(), fields[1])
			fmt.Fprintf(con, "You are now known as %q instead of %q\n", fields[1], con.Nickname())
			con.nickname = fields[1]
		}
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

// createSignalHandler returns a channel that will be closed when SIGTERM
// and SIGINT signals are received by a goroutine.
// When the returned channel is readable, it means goroutines should start cleaning
// up, then exit.
func createSignalHandler() (stopChannel <-chan struct{}) {
	stop := make(chan struct{})
	// Create another channel that receives SIGTERM and SIGINT signals and
	// closes the above channel.
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-ch
		debugLog.Printf("received signal %s, triggering cleanup and exit...\n", sig)
		close(stop)
	}()
	return stop
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
	exitWG := &sync.WaitGroup{}
	stopCh := createSignalHandler()
	addConnCh := make(chan *connection)
	removeConnCh := make(chan *connection)
	addMessageCh := make(chan message)
	exitWG.Add(1)
	startConnectionAccepter(l, exitWG, stopCh, addConnCh, removeConnCh, addMessageCh)
	exitWG.Add(1)
	startConnectionAndMessageManager(l, exitWG, stopCh, addConnCh, removeConnCh, addMessageCh)
	exitWG.Wait()
	debugLog.Println("all cleanup is done, program exiting")
}
