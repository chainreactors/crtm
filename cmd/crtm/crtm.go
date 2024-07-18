package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/chainreactors/crtm/internal/runner"
	"github.com/projectdiscovery/gologger"
)

func main() {
	options := runner.ParseOptions()
	runner, err := runner.NewRunner(options)
	if err != nil {
		gologger.Fatal().Msgf("Could not create runner: %s\n", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	// Setup close handler
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal, Exiting...")
		runner.Close()
		os.Exit(0)
	}()

	err = runner.Run()
	if err != nil {
		gologger.Fatal().Msgf("Could not run crtm: %s\n", err)
	}
}
