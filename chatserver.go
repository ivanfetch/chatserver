package chatserver

// This multi-user chat server helps me learn concurrency.
// This is a learning project, please don't count on it improving, or even
// working entirely well.
// You can use nc or telnet to connect to localhost port 8080,
// and chat with this server.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

// connection holds information about a connection to the chat server.
type connection struct {
	netConn          net.Conn // TCP connection
	isClosed         bool     // if netConn has been closed by us
	nickname         string   // optional nickname, accompanying chat messages
	receiveMessageCh chan *message
	server           *Server
}

// newConnection accepts a net.Conn and returns a type connection.
func (s *Server) newConnection(netConn net.Conn) *connection {
	receiveMessageCh := make(chan *message)
	return &connection{
		netConn:          netConn,
		receiveMessageCh: receiveMessageCh,
		server:           s,
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

// GetNickname returns the nickname for the chat connection if it has been set,
// otherwise the TCP IP address and port are returned.
func (c connection) GetNickname() string {
	if c.nickname != "" {
		return c.nickname
	}
	return c.netConn.RemoteAddr().String()
}

// receiveMessages accepts type message via connection.receiveMessageCh, and
// writes them to connection.netConn.
func (c *connection) processReceivedMessages() {
	c.server.exitWG.Add(1)
	defer c.server.exitWG.Done()
	debugLog.Printf("starting processing of messages received by connection %s\n", c.UniqueID())
	for {
		select {
		default:
			if c.isClosed {
				debugLog.Printf("exiting processing of messages received by connection %s\n", c.UniqueID())
				return
			}
		case newMessage := <-c.receiveMessageCh:
			var sender string
			if newMessage.connection != nil && c.UniqueID() == newMessage.connection.UniqueID() {
				sender = ">" // this recipient is the message-sender
				debugLog.Printf("showing %s their own message: %s\n", newMessage.connection.GetNickname(), newMessage.text)
			} else {
				sender = fmt.Sprintf("%s:", newMessage.connection.nickname)
				debugLog.Printf("sending %s a message from %s: %s\n", c.GetNickname(), newMessage.connection.GetNickname(), newMessage.text)
			}
			_, err := fmt.Fprintf(c.netConn, "%s %s\n", sender, newMessage.text)
			if err != nil {
				debugLog.Printf("error writing to %v: %v", c.netConn, err)
				debugLog.Printf("removing the errored connection: %v\n", c.UniqueID())
				c.server.removeConnCh <- c
				return
			}
			if c.server.shuttingDown {
				debugLog.Printf("removing connection after sending message, as we are shutting down: %s\n", c.UniqueID())
				c.server.removeConnCh <- c
			}
		}
	}
}

// message represents a message sent by a chat connection.
type message struct {
	connection *connection // who/what originated the message
	text       string
}

// String formats the message sender and text.
func (m message) String() string {
	return fmt.Sprintf("%s: %s\n", m.connection.GetNickname(), m.text)
}

// Server holds the TCP listener, configuration, and communication
// channels used by goroutines.
type Server struct {
	listenAddress           string // host:port or :port
	listener                net.Listener
	addConnCh, removeConnCh chan *connection
	addMessageCh            chan message
	openForBusiness         context.Context    // Still accepting connections and messages, not shutting down
	stopReceivingSignals    context.CancelFunc // Stop receiving notifications for OS signals
	exitWG                  *sync.WaitGroup
	shuttingDown            bool // cleanup / shutdown is in-process, do not accept new connections or messages.
}

// ServerOption uses a function  to set fields on a type Server by operating on
// that type as an argument.
// This provides optional configuration and minimizes required parameters for
// the constructor.
type ServerOption func(*Server) error

// WithListenAddress sets the corresponding field in a Server type.
func WithListenAddress(l string) ServerOption {
	return func(s *Server) error {
		if l == "" || !strings.Contains(l, ":") {
			return errors.New("please specify the listen address as host:port or :port")
		}
		s.listenAddress = l
		return nil
	}
}

// NewServer returns *Server, accepting optional parameters via With*()
// functional options.
func NewServer(options ...ServerOption) (*Server, error) {
	openForBusiness, stopReceivingSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	addConnCh := make(chan *connection)
	removeConnCh := make(chan *connection)
	addMessageCh := make(chan message)
	exitWG := &sync.WaitGroup{}
	s := &Server{
		listenAddress:        ":8080",
		openForBusiness:      openForBusiness,
		stopReceivingSignals: stopReceivingSignals,
		addConnCh:            addConnCh,
		removeConnCh:         removeConnCh,
		addMessageCh:         addMessageCh,
		exitWG:               exitWG,
	}
	for _, option := range options {
		err := option(s)
		if err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %v", s.listenAddress, err)
	}
	s.listener = listener
	debugLog.Printf("listening for connections on %s", s.listenAddress)
	return s, nil
}

// GetListenAddress returns the listen address of the chat server, of the form
// host:port or :port.
func (s Server) GetListenAddress() string {
	return s.listenAddress
}

var debugLog *log.Logger = log.New()

// startConnectionAccepter runs a goroutine that accepts connections to the
// chat server, and adds them to the connection-manager. A goroutine is also
// spawned to process any input from the connection.
func (s *Server) startConnectionAccepter() {
	s.exitWG.Add(1)
	go func() {
		debugLog.Println("starting connection accepter")
		defer s.exitWG.Done()
		for !s.shuttingDown {
			netConn, err := s.listener.Accept()
			if s.shuttingDown && err != nil {
				break // Ignore Accept() errors while shutting down, the listener was likely closed by us.
			}
			if err != nil {
				debugLog.Printf("while accepting a connection: %v", err)
				continue
			}
			debugLog.Printf("accepted connection from %s", netConn.RemoteAddr())
			conn := s.newConnection(netConn)
			s.addConnCh <- conn
			go conn.processInput()
			go conn.processReceivedMessages()
		}
		debugLog.Println("connection accepter exiting")
	}()
}

// startConnectionAndMessageManager runs a goroutine that tracks connections to the chat
// server, and broadcasts chat messages to all connections.
func (s *Server) startConnectionAndMessageManager() {
	var currentConnections []*connection
	s.exitWG.Add(1)
	go func() {
		debugLog.Println("starting connection manager")
		defer s.exitWG.Done()
		for {
			select {
			case newConn := <-s.addConnCh:
				debugLog.Printf("adding connection from %s", newConn.UniqueID())
				currentConnections = append(currentConnections, newConn)
			case removeConn := <-s.removeConnCh:
				debugLog.Printf("removing connection %s", removeConn.UniqueID())
				currentConnections = removeConnection(currentConnections, removeConn)
				removeConn.Close()
			case newMessage := <-s.addMessageCh:
				debugLog.Printf("broadcasting a new message from %s to %d connections: %s", newMessage.connection.GetNickname(), len(currentConnections), newMessage.text)
				for _, conn := range currentConnections {
					s.send(newMessage, conn)
				}
			case <-s.openForBusiness.Done():
				s.InitiateShutdown(len(currentConnections) > 0)
			default:
				// Avoid blocking thecontaining loop
			}
			if s.shuttingDown && len(currentConnections) == 0 {
				break
			}
		}
		debugLog.Println("connection manager exiting")
	}()
}

// processInput scans a chat connection for text, and hands chat commands or
// messages.
func (c *connection) processInput() {
	c.server.exitWG.Add(1)
	defer c.server.exitWG.Done()
	debugLog.Printf("saying hello then reading input from connection %s", c.UniqueID())
	fmt.Fprintln(c, `Well hello there!

Anything you type will be sent to all other users of this chat server.
A line that begins with a slash (/) is considered a command - enter /help for a list of valid commands. `)
	scanner := bufio.NewScanner(c)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		debugLog.Printf("received from %s: %s", c.UniqueID(), line)
		if strings.HasPrefix(line, "/") {
			exiting := processCommands(line, c)
			if exiting {
				c.server.removeConnCh <- c
				return
			}
			continue
		}
		c.server.addMessageCh <- message{
			text:       line,
			connection: c,
		}
	}
	err := scanner.Err()
	if !c.isClosed && err != nil {
		debugLog.Printf("while reading from %s: %v", c.UniqueID(), err)
	}
	debugLog.Printf("input processing is exiting for connection %s", c.UniqueID())
}

// send spawns a goroutine to write the specified message to the specified
// connection.
func (s *Server) send(msg message, conn *connection) {
	s.exitWG.Add(1)
	go func() {
		defer s.exitWG.Done()
		conn.receiveMessageCh <- &msg
	}()
}

// sendSystemMessage spawns a goroutine to submit the specified text as a chat
// message, from a "system" connection.
func (s *Server) sendSystemMessage(messageText string) {
	s.exitWG.Add(1)
	go func() {
		defer s.exitWG.Done()
		debugLog.Printf("sending a system message: %s\n", messageText)
		s.addMessageCh <- message{
			text:       messageText,
			connection: &connection{nickname: "system"},
		}
	}()
}

// InitiateShutdown starts shutting down goroutines for the chat server.
func (s *Server) InitiateShutdown(thereAreConnections bool) {
	if !s.shuttingDown {
		debugLog.Println("starting chat server clean up. . .")
		s.shuttingDown = true
		s.stopReceivingSignals()
		if thereAreConnections {
			s.sendSystemMessage("the chat server is shutting down - goodbye!")
		}

		s.listener.Close() // will unblock listener.Accept()
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
			debugLog.Printf("changing nickname from %q to %q", con.GetNickname(), fields[1])
			fmt.Fprintf(con, "You are now known as %q instead of %q\n", fields[1], con.GetNickname())
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

// WaitForExit waits for the chat server goroutines to finiss.
func (s *Server) WaitForExit() {
	debugLog.Println("waiting for go routines. . .")
	s.exitWG.Wait()
	debugLog.Println("all cleanup is done, program exiting")
}

func removeConnection(currentConnections []*connection, toRemove *connection) []*connection {
	if toRemove == nil {
		return currentConnections
	}
	newConnections := make([]*connection, len(currentConnections)-1)
	newI := 0
	for _, conn := range currentConnections {
		if conn.UniqueID() != toRemove.UniqueID() {
			newConnections[newI] = conn
			newI++
		}
	}
	return newConnections
}

func RunWithoutWaitingForExit() (*Server, error) {
	debugLog.SetFormatter(&log.TextFormatter{
		PadLevelText: true,
	})
	server, err := NewServer()
	if err != nil {
		return nil, err
	}
	server.startConnectionAccepter()
	server.startConnectionAndMessageManager()
	return server, nil
}

func Run() error {
	server, err := RunWithoutWaitingForExit()
	if err != nil {
		return err
	}
	server.WaitForExit()
	return nil
}
