package chat

// This multi-user chat server helps me learn concurrency.
// This is a learning project, please don't count on it improving, or even
// working entirely well.
// You can use nc or telnet to connect to localhost port 40001,
// and chat with this server.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var Version string = "development" // Populated at build time
var GitCommit string = "unknown"   // Populated at build time

// connection holds information about a connection to the chat server.
type connection struct {
	netConn          net.Conn // TCP connection
	isClosed         bool     // See the Read and Write receivers
	nickname         string   // optional nickname, accompanying chat messages
	receiveMessageCh chan *message
	server           *Server
}

// newConnection accepts a net.Conn and returns a type connection.
func (s *Server) newConnection(netConn net.Conn) *connection {
	return &connection{
		netConn:          netConn,
		receiveMessageCh: make(chan *message),
		server:           s,
	}
}

// Close wraps the Close method of the member net.Conn type and registers that
// this type connection is closed for the Read and Write receivers.
func (c *connection) Close() {
	c.server.log.Debugf("closing connection %s", c.UniqueID())
	c.isClosed = true
	c.netConn.Close()
}

// Read wraps netCOnn.Read, only reading if isClosed == false
func (c *connection) Read(b []byte) (int, error) {
	if c.isClosed {
		c.server.log.Debugf("not reading from this closed connection %s\n", c.UniqueID())
		return 0, nil
	}
	return c.netConn.Read(b)
}

// Write wraps netCOnn.Write, only writing if isClosed == false
func (c *connection) Write(b []byte) (int, error) {
	if c.isClosed {
		c.server.log.Debugf("not writing to this closed connection %s: %s\n", c.UniqueID(), string(b))
		return 0, nil
	}
	return c.netConn.Write(b)
}

// GetNickname returns the nickname for the chat connection if it has been set,
// otherwise the TCP IP address and port are returned.
func (c connection) GetNickname() string {
	if c.nickname != "" {
		return c.nickname
	}
	if c.netConn == nil {
		return "never connected"
	}
	return c.netConn.RemoteAddr().String()
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

// processCommand handles the specified string as a chat-server command. If
// the command would cause this connection to exit, clientIsLeaving will be
// set to true.
func (c *connection) processCommand(input string) (clientIsLeaving bool) {
	fields := strings.Fields(input)
	switch strings.ToLower(fields[0][1:]) { // first word minus its first character(/)
	case "quit", "exit", "leave":
		fmt.Fprintf(c, "You're leaving? Ok - have a nice day. :)\n")
		c.server.log.Debugf("client %s has signed off", c.UniqueID())
		return true
	case "nickname", "nick":
		if len(fields) > 1 && fields[1] != "" {
			c.server.log.Debugf("changing nickname from %q to %q", c.GetNickname(), fields[1])
			c.server.sendSystemMessage(fmt.Sprintf("%q is now known as %q", c.GetNickname(), fields[1]))
			c.nickname = fields[1]
		}
	case "help":
		fmt.Fprintf(c, `Available commands are:
/quit|leave|exit - Sign off and disconnect from chat.
/nick|nickname - Set your nickname, to be displayed with your messages.
`)
	default:
		fmt.Fprintf(c, "%q is an invalid command, please use /help for a list of commands.\n", fields[0])
	}
	return false
}

// processInput expects to have been run as a goroutine. It says hello on
// behalf of the chat server, then parses input to hands server commands or messages to other
// functions.
func (c *connection) processInput() {
	c.server.exitWG.Add(1)
	defer c.server.exitWG.Done()
	c.server.log.Debugf("saying hello then reading input from connection %s", c.UniqueID())
	fmt.Fprintln(c, `Well hello there!

Anything you type will be sent to all other users of this chat server.
A line that begins with a slash (/) is considered a command - enter /help for a list of valid commands. `)
	scanner := bufio.NewScanner(c)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		c.server.log.Debugf("received from %s: %s", c.UniqueID(), line)
		if strings.HasPrefix(line, "/") {
			exiting := c.processCommand(line)
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
		c.server.log.Debugf("while reading from %s: %v", c.UniqueID(), err)
	}
	c.server.log.Debugf("input processing is exiting for connection %s", c.UniqueID())
}

// receiveMessages starts a goroutine that accepts a message type via the
// connection.receiveMessageCh channel, and writes them to connection.netConn.
// After writing each message, if the chat server has begun shutting down,
// this connection will be closed.
func (c *connection) processReceivedMessages() {
	c.server.exitWG.Add(1)
	defer c.server.exitWG.Done()
	c.server.log.Debugf("starting processing of messages received by connection %s\n", c.UniqueID())
	for {
		select {
		default:
			if c.isClosed {
				c.server.log.Debugf("exiting processing of messages received by connection %s\n", c.UniqueID())
				return
			}
		case newMessage := <-c.receiveMessageCh:
			var sender string
			if newMessage.connection != nil && c.UniqueID() == newMessage.connection.UniqueID() {
				sender = ">" // this recipient is the message-sender
				c.server.log.Debugf("showing %s their own message: %s\n", newMessage.connection.GetNickname(), newMessage.text)
			} else {
				sender = fmt.Sprintf("%s>", newMessage.connection.nickname)
				c.server.log.Debugf("sending %s a message from %s: %s\n", c.GetNickname(), newMessage.connection.GetNickname(), newMessage.text)
			}
			_, err := fmt.Fprintf(c.netConn, "%s %s\n", sender, newMessage.text)
			if err != nil {
				c.server.log.Debugf("error writing to %v: %v", c.UniqueID(), err)
				c.server.log.Debugf("removing the errored connection: %v\n", c.UniqueID())
				c.server.removeConnCh <- c
				return
			}
			if c.server.shuttingDown {
				c.server.log.Debugf("removing connection since the chat server is shutting down: %s\n", c.UniqueID())
				c.server.removeConnCh <- c
			}
		}
	}
}

// message represents a chat message.
type message struct {
	connection *connection // who/what originated the message
	text       string
}

// Server holds the TCP listener, configuration, and communication
// channels used by goroutines.
type Server struct {
	listenAddress           string // host:port or :port
	listener                net.Listener
	log                     *logrus.Logger
	addConnCh, removeConnCh chan *connection
	addMessageCh            chan message
	openForBusiness         context.Context    // Still accepting connections and messages, not shutting down
	numConnections          int                // Populated by the connection and message manager
	stopReceivingSignals    context.CancelFunc // Stop receiving notifications for OS signals
	exitWG                  *sync.WaitGroup    // How many goroutines are started?
	shuttingDown            bool               // cleanup / shutdown is in-process, do not accept new connections or messages.
	hasExited               bool               // Cleanup has finished>
}

// ServerOption uses a function  to set fields on a type Server by operating on
// that type as an argument.
// This provides optional configuration and minimizes required parameters for
// the constructor.
type ServerOption func(*Server) error

// WithListenAddress sets the corresponding field in a type Server.
func WithListenAddress(l string) ServerOption {
	return func(s *Server) error {
		if l == "" || !strings.Contains(l, ":") {
			return errors.New("please specify the listen address as host:port or :port")
		}
		s.listenAddress = l
		return nil
	}
}

// WithDebugLogging outputs debug logs to standard error. By default, minimal
// informative log messages are output.
func WithDebugLogging() ServerOption {
	return func(s *Server) error {
		if s.log == nil {
			s.createLog()
		}
		s.log.SetLevel(logrus.DebugLevel)
		return nil
	}
}

// WithLogWriter sets the io.Writer where log output is written.
func WithLogWriter(w io.Writer) ServerOption {
	return func(s *Server) error {
		if s.log == nil {
			s.createLog()
		}
		s.log.SetOutput(w)
		return nil
	}
}

// NewServer returns a *Server, including optionally specified configuration.
// optional parameters can be specified via With*()
// functional options.
func NewServer(options ...ServerOption) (*Server, error) {
	openForBusiness, stopReceivingSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	s := &Server{
		listenAddress:        ":40001",
		openForBusiness:      openForBusiness,
		stopReceivingSignals: stopReceivingSignals,
		addConnCh:            make(chan *connection),
		removeConnCh:         make(chan *connection),
		addMessageCh:         make(chan message),
		exitWG:               &sync.WaitGroup{},
	}
	s.createLog()
	for _, option := range options {
		err := option(s)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// createLog configures the logger, which only outputs info level messages by
// default.
func (s *Server) createLog() {
	var log *logrus.Logger = logrus.New()
	// Output relative time offsets instead of full timestamps.
	log.SetFormatter(&logrus.TextFormatter{
		PadLevelText: true,
	})
	s.log = log
}

// GetListenAddress returns the listen address of the chat server, of the form
// host:port or :port.
func (s Server) GetListenAddress() string {
	return s.listenAddress
}

// startConnectionAccepter starts a goroutine that accepts connections to the
// chat server, and adds them to the connection-and-message-manager.
// Additional goroutines are started for each received connection, to process
// their input and output.
func (s *Server) startConnectionAccepter() {
	s.exitWG.Add(1)
	go func() {
		s.log.Debugln("starting connection accepter")
		defer s.exitWG.Done()
		for !s.shuttingDown {
			netConn, err := s.listener.Accept()
			if s.shuttingDown && err != nil {
				break // Ignore Accept() errors while shutting down, the listener was likely closed by us.
			}
			if err != nil {
				s.log.Debugf("while accepting a connection: %v", err)
				continue
			}
			s.log.Debugf("accepted connection from %s", netConn.RemoteAddr())
			conn := s.newConnection(netConn)
			s.addConnCh <- conn
			go conn.processInput()
			go conn.processReceivedMessages()
		}
		s.log.Debugln("connection accepter exiting")
	}()
}

// startConnectionAndMessageManager starts a goroutine that tracks connections to the chat
// server, and broadcasts chat messages to all connections.
func (s *Server) startConnectionAndMessageManager() {
	var currentConnections []*connection
	s.exitWG.Add(1)
	go func() {
		s.log.Debugln("starting connection manager")
		defer s.exitWG.Done()
		for {
			select {
			case <-s.openForBusiness.Done():
				s.InitiateShutdown()
			case newConn := <-s.addConnCh:
				s.log.Debugf("adding connection from %s", newConn.UniqueID())
				currentConnections = append(currentConnections, newConn)
				s.sendSystemMessage(fmt.Sprintf("%s has joined the chat", newConn.GetNickname()))
				s.numConnections = len(currentConnections)
			case removeConn := <-s.removeConnCh:
				s.log.Debugf("removing connection %s", removeConn.UniqueID())
				currentConnections = removeConnection(currentConnections, removeConn)
				removeConn.Close()
				if !s.shuttingDown { // Avoid writes to a blocking channel if we're trying to clean up
					s.sendSystemMessage(fmt.Sprintf("%s has left the chat", removeConn.GetNickname()))
				}
				s.numConnections = len(currentConnections)
			case newMessage := <-s.addMessageCh:
				s.log.Debugf("broadcasting a new message from %s to %d connections: %s", newMessage.connection.GetNickname(), len(currentConnections), newMessage.text)
				for _, conn := range currentConnections {
					s.send(newMessage, conn)
				}
			default:
				// Avoid blocking thecontaining loop
			}
			if s.shuttingDown && len(currentConnections) == 0 {
				break
			}
		}
		s.log.Debugln("connection manager exiting")
	}()
}

// removeConnection accepts a slice of *connection, and removes the specified
// connection. This is meant to only be called from within the
// connection-and-message-manager.
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

// send starts a goroutine to write the specified message to the specified
// connection receiveMessageCh channel. This avoids blocking
// connection-manager.
func (s *Server) send(msg message, conn *connection) {
	s.exitWG.Add(1)
	go func() {
		defer s.exitWG.Done()
		conn.receiveMessageCh <- &msg
	}()
}

// sendSystemMessage starts a goroutine to submit the specified text as a chat
// message, from a "system" connection.
func (s *Server) sendSystemMessage(messageText string) {
	s.exitWG.Add(1)
	go func() {
		defer s.exitWG.Done()
		s.log.Debugf("sending a system message: %s\n", messageText)
		s.addMessageCh <- message{
			text:       messageText,
			connection: &connection{nickname: "-"},
		}
	}()
}

// InitiateShutdown starts shutting down goroutines for the chat server.
func (s *Server) InitiateShutdown() {
	if !s.shuttingDown {
		s.log.Infoln("starting chat server shutdown. . .")
		s.shuttingDown = true
		s.stopReceivingSignals()
		if s.numConnections > 0 {
			s.sendSystemMessage("the chat server is shutting down - goodbye!")
		}
		s.listener.Close() // will unblock listener.Accept()
	}
}

// HasExited returns true if the chat-server goroutines have all finished and
// exited.
// This is useful to verify cleanup is complete in a non-blocking way.
func (s *Server) HasExited() bool {
	return s.hasExited
}

// WaitForExit waits for the chat server goroutines to finish.
func (s *Server) WaitForExit() {
	s.log.Debugln("waiting for go routines. . .")
	s.exitWG.Wait()
	s.hasExited = true
	s.log.Debugln("all cleanup is done")
}

// ListenAndServe begins listening for new connections, and starts the
// connection-and-message-manager.
func (s *Server) ListenAndServe() error {
	var err error
	s.listener, err = net.Listen("tcp", s.listenAddress)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v", s.listenAddress, err)
	}
	s.log.Infof("listening for connections on %s", s.listenAddress)
	s.startConnectionAccepter()
	s.startConnectionAndMessageManager()
	return nil
}
