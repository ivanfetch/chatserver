# Chat Server - A Go Concurrency Learning Project

This chat server accepts TCP connections, and allows users to broadcast messages, change their nickname, and of course, disconnect. You can connect to the chat server using a utility like [netcat](https://en.wikipedia.org/wiki/Netcat) or telnet.

## Usage

### One-time Setup

Install the chat server using one of these methods:

* Run `go install github.com/ivanfetch/chatserver/cmd/chatserver@latest`
* Directly [downloading a release](https://github.com/ivanfetch/chatserver/releases)
* Building from source, after downloading or cloning this repository, by running `make build`

### Example

* Run the chat server:

```bash
$ chatserver
```

```
INFO   [0000] listening for connections on :40001 - press CTRL-c to stop the chat   server
```

* Connect to the chat server, and set your nickname:

```bash
$ nc localhost 40001
```

```
Well hello there!

Anything you type will be sent to all other users of this chat server.
A line that begins with a slash (/) is considered a command - enter /help for a list of valid commands. 
-> [::1]:57715 has joined the chat
/nickname Ivan
-> "[::1]:57715" is now known as "Ivan"
Hello, to anyone else who is connected!
> Hello, to anyone else who is connected!
/quit
You're leaving? Ok - have a nice day. :)
```

When you are done using it, exit the chat server by pressing CTRL-c.
