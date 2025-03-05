package main

import (
	"flag"
	"fmt"
	"sls-local-server/packages/common"
	"sls-local-server/packages/ide"
	"sls-local-server/packages/testbeds"
	"strings"

	"go.uber.org/zap"
)

var Version = "dev"
var folder *string

func main() {
	command := flag.String("command", "python3 handler.py", "the user command to run")
	check := flag.String("check", "null", "the version of the server to run")
	aiApiIde := flag.String("ai-api-ide", "null", "should the binary server an ide")
	folder = flag.String("folder", "/", "the folder to run the command in")

	flag.Parse()

	if check != nil && *check == "version" {
		fmt.Println(Version)
		return
	}

	log, err := zap.NewProduction()
	if err != nil {
		log.Error("Failed to initialize logger", zap.Error(err))
		return
	}
	defer log.Sync()

	if aiApiIde != nil && *aiApiIde == "true" {
		go func() {
			ide.RunHealthServer(log)
		}()

		err := ide.DownloadIde(log)
		if err != nil {
			log.Error("Failed to download ide", zap.Error(err))
			ide.TerminateIdePod(log)
			return
		}

		ide.SYSTEM_INITIALIZED = true
		cmd := fmt.Sprintf("code-server --bind-addr 0.0.0.0:8080 --auth password --welcome-text \"RunpodIDE\" --app-name \"RunpodIDE\" %s", *folder)
		err = common.RunCommand(cmd, true, log)
		if err != nil {
			log.Error("Failed to run command", zap.Error(err))
			ide.TerminateIdePod(log)
			return
		}
	} else {
		go func() {
			fmt.Println("Running tests")
			testbeds.RunTests(log)
		}()

		var modifiedCommand string
		if command != nil {
			modifiedCommand = *command
			modifiedCommand = strings.Replace(modifiedCommand, "/bin/sh -c ", "", 1)
			modifiedCommand = strings.Replace(modifiedCommand, "/bin/bash -o pipefail -c ", "", 1)
		}
		fmt.Println("Running command", modifiedCommand)
		common.RunCommand(modifiedCommand, false, log)
	}
}
