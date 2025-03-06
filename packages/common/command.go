package common

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

func RunCommand(command string, ide bool, log *zap.Logger) error {
	// Create a buffered channel for logs
	logBuffer := make(chan string, 16)
	logBuffer <- fmt.Sprintf("Running command: %s", command)

	log.Info("Running command", zap.String("command", command))
	// Split the command string into command and arguments
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "RUNPOD_LOG_LEVEL=INFO")
	if ide {
		cmd.Env = append(cmd.Env, "PASSWORD=runpod")
		cmd.Env = append(cmd.Env, "AI_API_REDIS_ADDR=127.0.0.1:6379")
		cmd.Env = append(cmd.Env, "AGENT_REDIS_ADDR=127.0.0.1:6379")
		cmd.Env = append(cmd.Env, "AI_API_REDIS_PASS=")
		cmd.Env = append(cmd.Env, "HOST_ACCESS_TOKEN=test")
		cmd.Env = append(cmd.Env, "ENV=local")
	} else {
		cmd.Env = append(cmd.Env, "RUNPOD_ENDPOINT_BASE_URL=http://0.0.0.0:80/v2/IDE/v2")
		cmd.Env = append(cmd.Env, "RUNPOD_WEBHOOK_GET_JOB=http://0.0.0.0:80/v2/IDE/job-take/$RUNPOD_POD_ID")
		cmd.Env = append(cmd.Env, "RUNPOD_WEBHOOK_POST_OUTPUT=http://0.0.0.0:80/v2/IDE/job-done/$RUNPOD_POD_ID/$ID")
		cmd.Env = append(cmd.Env, "AI_API_REDIS_ADDR=127.0.0.1:6379")
		cmd.Env = append(cmd.Env, "AGENT_REDIS_ADDR=127.0.0.1:6379")
		cmd.Env = append(cmd.Env, "AI_API_REDIS_PASS=")
		cmd.Env = append(cmd.Env, "HOST_ACCESS_TOKEN=test")
		cmd.Env = append(cmd.Env, "ENV=local")
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to create stdout pipe: %s", err.Error())
		log.Error("Failed to create stdout pipe", zap.Error(err))
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to create stderr pipe: %s", err.Error())
		log.Error("Failed to create stderr pipe", zap.Error(err))
		return err
	}

	err = cmd.Start()
	if err != nil {
		logBuffer <- fmt.Sprintf("Failed to start command: %s", err.Error())
		errorMsg := fmt.Sprintf("Failed to start command: %s", err.Error())
		SendResultsToGraphQL("FAILED", &errorMsg, log, []Result{})
		fmt.Println("Failed to start command: ", err.Error())
		log.Error("Failed to start command", zap.Error(err))
		return err
	}

	testNumberChannel := make(chan int)
	go SendLogsToTinyBird(logBuffer, testNumberChannel, log)

	// Start goroutines to continuously read from pipes
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				log.Info("Command stdout", zap.ByteString("output", buf[:n]))

				// Add log to buffer channel
				select {
				case logBuffer <- string(buf[:n]):
					// Log added to buffer
				default:
					// Channel full, log discarded
					log.Warn("Log buffer full, discarding log")
				}

			}
			if err != nil {
				logBuffer <- fmt.Sprintf("Failed to read stdout: %s", err.Error())
				break
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Info("Command stderr", zap.String("output", string(buf[:n])))

				// Add log to buffer channel
				select {
				case logBuffer <- fmt.Sprintf("#ERROR: %s", string(buf[:n])):
					// Log added to buffer
				default:
					// Channel full, log discarded
					log.Warn("Log buffer full, discarding log")
				}

			}
			if err != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		errorMsg := fmt.Sprintf("Command closed: %s", err.Error())
		fmt.Println("Command closed: ", errorMsg)
		SendResultsToGraphQL("FAILED", &errorMsg, log, []Result{})
		return nil
	}

	time.Sleep(time.Duration(10) * time.Second)
	close(logBuffer)
	errorMsg := "Command closed. Please view the logs for more information."
	SendResultsToGraphQL("FAILED", &errorMsg, log, []Result{})

	return nil
}
