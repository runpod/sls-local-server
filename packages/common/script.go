package common

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
	err = RunCommand("/bin/install_bash.sh", false, logger)
	if err != nil {
		logger.Error("Error executing install script", zap.Error(err))
		return fmt.Errorf("failed to execute install script: %v", err)
	}

	return nil
}
