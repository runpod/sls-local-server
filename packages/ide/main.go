package ide

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sls-local-server/packages/common"
	"sls-local-server/packages/testbeds"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var slash = "/"
var folder = &slash
var SYSTEM_INITIALIZED = false

type Handler struct {
	log *zap.Logger
}

func NewHandler(log *zap.Logger) *Handler {
	return &Handler{
		log: log,
	}
}

func RunHealthServer(log *zap.Logger) {
	gin.SetMode(gin.ReleaseMode)
	h := NewHandler(log)

	r := gin.New()
	// Add recovery middleware
	r.Use(gin.Recovery())
	// Add logging middleware
	r.Use(testbeds.LoggerMiddleware(log))

	r.GET("/health", h.Health)

	if err := r.Run(":" + "8079"); err != nil {
		log.Fatal("Failed to start server", zap.Error(err))
	}
}

func (h *Handler) Health(c *gin.Context) {
	if !SYSTEM_INITIALIZED {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
		})
	}

	// Check the heartbeat file for code-server
	heartbeatFile := "/root/.local/share/code-server/heartbeat"
	fileInfo, err := os.Stat(heartbeatFile)

	var heartbeat time.Time
	if err == nil {
		// Store the last modified time if file exists
		heartbeat = fileInfo.ModTime()
		h.log.Info("Code-server heartbeat found", zap.Time("lastModified", heartbeat))
	} else {
		// If file doesn't exist or can't be accessed
		h.log.Warn("Could not access code-server heartbeat file", zap.Error(err))
		heartbeat = time.Time{} // Zero time
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"folder":    *folder,
		"heartbeat": heartbeat,
	})
}

func RunCommand(command string, log *zap.Logger) error {
	// Create a buffered channel for logs
	logBuffer := make(chan string, 16)
	logBuffer <- fmt.Sprintf("Running command: %s", command)

	log.Info("Running command", zap.String("command", command))
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "RUNPOD_LOG_LEVEL=INFO")

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
		log.Error("Failed to start command", zap.Error(err))
		return err
	}

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

	cmd.Wait()

	close(logBuffer)
	return fmt.Errorf("Command closed")
}

func DownloadIde(logger *zap.Logger, initializeIDE bool) error {
	// First install curl
	url := "https://github.com/gitpod-io/openvscode-server/releases/download/openvscode-server-v1.98.2/openvscode-server-v1.98.2-linux-x64.tar.gz"
	extensionURL := "https://dev-runpod-lambda-testbucketsbucketccd5c433-xrjvi7bexnjp.s3.us-east-1.amazonaws.com/runpod-build-0.0.6.vsix"

	if err := common.InstallAndRunAiApi(logger); err != nil {
		logger.Error("Failed to install aiapi plus install script", zap.Error(err))
		return fmt.Errorf("failed to install aiapi: %v", err)
	}

	if !initializeIDE {
		return nil
	}

	// Check if curl is available
	curlCmd := exec.Command("which", "curl")
	if err := curlCmd.Run(); err == nil {
		// Use curl to download
		cmd := exec.Command("curl", "-L", "-o", "/bin/openvscode-server.tar.gz", url)
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to download script using curl", zap.Error(err))
			return fmt.Errorf("failed to download script with curl: %v", err)
		}

		extensionCommand := exec.Command("curl", "-L", "-o", "/bin/runpod-build-0.0.6.vsix", extensionURL)
		if err := extensionCommand.Run(); err != nil {
			logger.Error("Failed to download extension using curl", zap.Error(err))
			return fmt.Errorf("failed to download extension with curl: %v", err)
		}
	} else {
		// Try wget if curl not available
		wgetCmd := exec.Command("which", "wget")
		if err := wgetCmd.Run(); err == nil {
			cmd := exec.Command("wget", "-O", "/bin/openvscode-server.tar.gz", url)
			if err := cmd.Run(); err != nil {
				logger.Error("Failed to download script using wget", zap.Error(err))
				return fmt.Errorf("failed to download script with wget: %v", err)
			}

			extensionCommand := exec.Command("wget", "-O", "/bin/runpod-build-0.0.6.vsix", extensionURL)
			if err := extensionCommand.Run(); err != nil {
				logger.Error("Failed to download extension using wget", zap.Error(err))
				return fmt.Errorf("failed to download extension with wget: %v", err)
			}
		} else {
			logger.Error("Neither curl nor wget is available")
			return fmt.Errorf("neither curl nor wget is available to download script")
		}
	}

	// Then install code-server
	cmd := exec.Command("tar", "-xzf", "/bin/openvscode-server.tar.gz", "-C", "/bin")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("Failed to create stdout pipe", zap.Error(err))
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Error("Failed to create stderr pipe", zap.Error(err))
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Error("Failed to start code-server installation", zap.Error(err))
		return fmt.Errorf("failed to start code-server installation: %v", err)
	}

	// Stream stdout in real time
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				logger.Info(string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	// Stream stderr in real time
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				logger.Error(string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		logger.Error("Failed to install code-server", zap.Error(err))
		return fmt.Errorf("failed to install code-server: %v", err)
	}

	return nil
}

func TerminateIdePod(log *zap.Logger) {
	runpodPodIDEJwt := os.Getenv("RUNPOD_IDE_POD_JWT")
	webhookUrl := os.Getenv("RUNPOD_IDE_POD_WEBHOOK_URL")

	if runpodPodIDEJwt == "" || webhookUrl == "" {
		log.Error("RUNPOD_IDE_POD_JWT or RUNPOD_IDE_POD_WEBHOOK_URL not set")
		return
	}

	req, err := http.NewRequest("POST", webhookUrl, nil)
	if err != nil {
		log.Error("Failed to create request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runpodPodIDEJwt)

	// send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("Failed to send request", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error("Request failed", zap.Int("status", resp.StatusCode))
		return
	}
}
