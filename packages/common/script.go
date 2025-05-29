package common

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

//go:embed install_bash.sh
var bashInstallScript string

//go:embed install_sh.sh
var shInstallScript string

func installScript(logger *zap.Logger) error {
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
	err := os.WriteFile("/bin/install_bash.sh", []byte(scriptContent), 0755)
	if err != nil {
		logger.Error("Failed to write install script to file", zap.Error(err))
		return fmt.Errorf("failed to write install script to file: %v", err)
	}

	// Execute the script
	cmd := exec.Command("/bin/install_bash.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("Error executing install script", zap.Error(err))
		return fmt.Errorf("failed to execute install script: %v", err)
	}
	logger.Info("Script output", zap.String("output", string(output)))

	if strings.Contains(string(output), "Could not connect to") {
		logger.Error("Failed to install script because of network issues", zap.String("output", string(output)))
		cmd := exec.Command("/bin/install_bash.sh")
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("Error executing install script", zap.Error(err))
			return fmt.Errorf("failed to execute install script: %v", err)
		}
		logger.Info("Script output", zap.String("output", string(output)))
	}

	return nil
}
