package ide

import (
	"fmt"
	"os/exec"

	"go.uber.org/zap"
)

func DownloadIde(logger *zap.Logger) error {
	// First install curl
	url := "https://code-server.dev/install.sh"

	// Check if bash is available
	bashCmd := exec.Command("which", "bash")
	shellType := "sh"
	if err := bashCmd.Run(); err == nil {
		shellType = "bash"
	}

	if shellType == "bash" {
		bashCmd := exec.Command("/bin/bash", "-c", "chmod +x shell/install_bash.sh && ./shell/install_bash.sh")
		if err := bashCmd.Run(); err != nil {
			logger.Error("Failed to run install_bash.sh", zap.Error(err))
			return fmt.Errorf("failed to run install_bash.sh: %v", err)
		}
	} else {
		shCmd := exec.Command("/bin/sh", "-c", "chmod +x shell/install_sh.sh && ./shell/install_sh.sh")
		if err := shCmd.Run(); err != nil {
			logger.Error("Failed to run install_sh.sh", zap.Error(err))
			return fmt.Errorf("failed to run install_sh.sh: %v", err)
		}
	}

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

	// Then install code-server
	cmd := exec.Command("sh", "-c", "chmod +x install.sh && ./install.sh")
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
