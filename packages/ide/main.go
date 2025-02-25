package ide

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
)

//go:embed install_bash.sh
var bashInstallScript string

//go:embed install_sh.sh
var shInstallScript string

func DownloadIde(logger *zap.Logger) error {
	// First install curl
	url := "https://code-server.dev/install.sh"

	// Check if bash is available
	bashCmd := exec.Command("which", "bash")
	shellType := "sh"
	if err := bashCmd.Run(); err == nil {
		shellType = "bash"
	}

	// Determine which script to use based on shell type
	scriptContent := shInstallScript
	if shellType == "bash" {
		scriptContent = bashInstallScript
	}

	// Write the script to a file
	err := os.WriteFile("install_bash.sh", []byte(scriptContent), 0755)
	if err != nil {
		logger.Error("Failed to write install script to file", zap.Error(err))
		return fmt.Errorf("failed to write install script to file: %v", err)
	}

	// Execute the script
	cmd := exec.Command("./install_bash.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("Error executing install script", zap.Error(err))
		return fmt.Errorf("failed to execute install script: %v", err)
	}
	logger.Info("Script output", zap.String("output", string(output)))

	// Check if curl is available
	curlCmd := exec.Command("which", "curl")
	if err := curlCmd.Run(); err == nil {
		// Use curl to download
		cmd := exec.Command("curl", "-fsSL", url, "-o", "install.sh")
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to download script using curl", zap.Error(err))
			return fmt.Errorf("failed to download script with curl: %v", err)
		}
	} else {
		// Try wget if curl not available
		wgetCmd := exec.Command("which", "wget")
		if err := wgetCmd.Run(); err == nil {
			cmd := exec.Command("wget", "-O", "install.sh", url)
			if err := cmd.Run(); err != nil {
				logger.Error("Failed to download script using wget", zap.Error(err))
				return fmt.Errorf("failed to download script with wget: %v", err)
			}
		} else {
			logger.Error("Neither curl nor wget is available")
			return fmt.Errorf("neither curl nor wget is available to download script")
		}
	}

	// Start Redis server in daemonized mode
	redisCmd := exec.Command("sh", "-c", "redis-server --daemonize yes")
	redisOutput, err := redisCmd.CombinedOutput()
	if err != nil {
		logger.Error("Failed to start Redis server", zap.Error(err), zap.String("output", string(redisOutput)))
		return fmt.Errorf("failed to start Redis server: %v", err)
	}
	logger.Info("Redis server started in daemonized mode", zap.String("output", string(redisOutput)))

	// Then install code-server
	cmd = exec.Command("sh", "-c", "chmod +x install.sh && ./install.sh")
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
