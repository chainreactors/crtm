package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/crtm/pkg/utils"
	"github.com/projectdiscovery/goflags"
)

type options struct {
	DisableUpdateCheck bool
}

func main() {
	options := &options{}
	flagSet := goflags.NewFlagSet()
	toolName := "nuclei"

	flagSet.CreateGroup("update", "Update",
		flagSet.CallbackVarP(utils.GetUpdaterCallback(toolName), "update", "up", fmt.Sprintf("update %v to the latest released version", toolName)),
		flagSet.BoolVarP(&options.DisableUpdateCheck, "disable-update-check", "duc", false, "disable automatic update check"),
	)

	if err := flagSet.Parse(); err != nil {
		panic(err)
	}

	if !options.DisableUpdateCheck {
		home, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		basePath := filepath.Join(home, ".crtm/go/bin")
		msg := utils.GetVersionCheckCallback(toolName, basePath)()
		fmt.Println(msg)
	}
}
