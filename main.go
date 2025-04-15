package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sls-local-server/packages/common"
	"sls-local-server/packages/ide"
	"sls-local-server/packages/testbeds"
	"strings"
	"syscall"
	"time"

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

	if _, err := os.Stat("/bin"); os.IsNotExist(err) {
		log.Info("Creating bin directory")
		if err := os.Mkdir("/bin", 0755); err != nil {
			log.Error("Failed to create bin directory", zap.Error(err))
		}
	}

	if aiApiIde != nil && *aiApiIde == "true" {
		go func() {
			ide.RunHealthServer(log)
		}()

		var initializeIDE bool = true
		if os.Getenv("RUNPOD_INITIALIZE_IDE") == "false" {
			initializeIDE = false
		}

		err := ide.DownloadIde(log, initializeIDE)
		if err != nil {
			log.Error("Failed to download ide", zap.Error(err))
			ide.TerminateIdePod(log)
			return
		}

		if initializeIDE {
			ide.SYSTEM_INITIALIZED = true
			cmd := "cd /bin/openvscode-server-v1.98.2-linux-x64 && ./bin/openvscode-server --connection-token 1234 --host 0.0.0.0 --port 8080 --enable-remote-auto-shutdown --install-extension RunPod.runpod-build"
			err = common.RunCommand(cmd, true, log, nil)
			if err != nil {
				log.Error("Failed to run command", zap.Error(err))
				ide.TerminateIdePod(log)
				return
			}
			ide.TerminateIdePod(log)
		} else {
			// Create a blocking channel to prevent the program from exiting
			log.Info("IDE initialization skipped, creating blocking channel")
			blockingChannel := make(chan struct{})

			// Start a goroutine to handle signals for graceful shutdown
			go func() {
				sigChan := make(chan os.Signal, 1)
				signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

				// Wait for termination signal
				sig := <-sigChan
				log.Info("Received signal, shutting down", zap.String("signal", sig.String()))

				// Close the blocking channel to allow program to exit
				close(blockingChannel)
			}()

			// Block until channel is closed
			<-blockingChannel
			log.Info("Exiting program")
		}
	} else {
		testNumberChannel := make(chan int)
		go func(testNumberChannel chan int) {
			fmt.Println("Running tests")
			testbeds.RunTests(log, testNumberChannel)
		}(testNumberChannel)

		for {
			time.Sleep(time.Duration(1) * time.Second)
			aiApiStatus, err := http.Get("http://localhost:80/ping")
			if err != nil {
				continue
			}
			if aiApiStatus.StatusCode == 200 {
				break
			}
		}

		var modifiedCommand string
		if command != nil {
			modifiedCommand = *command
			modifiedCommand = strings.Replace(modifiedCommand, "/bin/sh -c ", "", 1)
			modifiedCommand = strings.Replace(modifiedCommand, "/bin/bash -o pipefail -c ", "", 1)
		}
		fmt.Println("Running command", modifiedCommand)
		common.RunCommand(modifiedCommand, false, log, testNumberChannel)
	}
}
