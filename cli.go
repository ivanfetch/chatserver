package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/profile"
	flag "github.com/spf13/pflag"
)

// RunCLI processes command-line arguments, instantiates a new chat server,
// calls ListenAndServe, waits for chat server routines to exit,
// then returns an exit status code.
func RunCLI() int {
	server, err := NewServerFromArgs(os.Args[1:])
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if server.enableProfiling {
		defer profile.Start(profile.GoroutineProfile, profile.ProfilePath(".")).Stop()
	}
	err = server.ListenAndServe()
	if err != nil {
		fmt.Println(err)
		return 1
	}
	server.WaitForExit()
	return 0
}

// NewServerFromArgs returns a type *Server after processing command-line
// arguments.
func NewServerFromArgs(args []string) (*Server, error) {
	fs := flag.NewFlagSet("chatserver", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Printf(`This chat server operates like a very basic version of Internet Relay Chat (IRC).

Use a program like telnet, or nc (netcat), to connect to the chat server. Anything you type is sent to all other connected users.

Usage: %s [-d|--debug-logging] [-l|--listen-address [<IP address>]:<port>] [-v|--version]

`,
			filepath.Base(os.Args[0]))
		fs.PrintDefaults()
	}

	CLIDebugLogging := fs.BoolP("debug-logging", "d", false, "Enable debug logging. This can also be enabled by setting the CHATSERVER_DEBUG_LOGGING environment variable to any value.")
	CLIProfiling := fs.BoolP("enable-profiling", "P", false, "Enable goroutine profiling. The resulting goroutine.pprof file will be written to the current directory at the time the chat server was run. This can also be enabled by setting the CHATSERVER_ENABLE_PROFILING environment variable to any value.")
	CLIVersion := fs.BoolP("version", "v", false, "Display the version and git commit.")
	CLIListenAddress := fs.StringP("listen-address", "l", ":40001", "The TCP address the chat server should listen on, of the form IP:Port or :Port. For example, :9090 or 1.2.3.4:9090. This can also be set via the CHATSERVER_LISTEN_ADDRESS environment variable.")
	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}
	fs.VisitAll(setCLIFlagFromEnvVar)
	if *CLIVersion {
		return nil, fmt.Errorf("version %s, git commit %s", Version, GitCommit)
	}
	var optionalConfig []ServerOption
	if *CLIProfiling {
		optionalConfig = append(optionalConfig, WithProfiling())
	}
	if *CLIDebugLogging {
		optionalConfig = append(optionalConfig, WithDebugLogging())
	}
	if *CLIListenAddress != "" {
		optionalConfig = append(optionalConfig, WithListenAddress(*CLIListenAddress))
	}

	server, err := NewServer(optionalConfig...)
	if err != nil {
		return nil, err
	}
	return server, nil
}

// setCLIFlagFromEnvVar iterates the flags defined in a type Flag, and sets
// the values of any flags whos corresponding environment variable is set.
// Environment variable names use the form CHATSERVER_{flag name}, with any
// dashes replaced by underscores
// For example, flag debug-logging uses the environment variable
// CHATSERVER_DEBUG_LOGGING
func setCLIFlagFromEnvVar(f *flag.Flag) {
	envVarName := "CHATSERVER_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
	envVarValue := os.Getenv(envVarName)
	if envVarValue != "" && f.Value.String() == f.DefValue {
		err := f.Value.Set(envVarValue)
		if err != nil {
			// The error is not returned because FlagSet.VisitAll will not accept it.
			panic(fmt.Errorf("while setting value %q to flag %v: %w", envVarValue, f, err))
		}
	}
}
