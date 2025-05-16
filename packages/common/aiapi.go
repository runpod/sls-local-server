package common

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

func InstallAndRunAiApi(logger *zap.Logger) error {
	if err := installScript(logger); err != nil {
		logger.Error("Failed to install script", zap.Error(err))
		return fmt.Errorf("failed to install script: %v", err)
	}

	go func() {
		isDev := os.Getenv("RUNPOD_API_URL") == "https://api.runpod.dev/graphql"
		aiApiS3URL := "https://local-sls-server-runpodinc.s3.us-east-1.amazonaws.com/aiapi"
		if isDev {
			aiApiS3URL = "https://rutvik-test-script.s3.us-east-1.amazonaws.com/aiapi-test"
		}

		curlCmd := exec.Command("which", "curl")
		if err := curlCmd.Run(); err == nil {
			aiApiInstallCmd := exec.Command("curl", "-fsSL", aiApiS3URL, "-o", "/bin/aiapi")
			if err := aiApiInstallCmd.Run(); err != nil {
				logger.Error("Failed to download aiapi", zap.Error(err))
				return
			}
		} else {
			aiApiInstallCmd := exec.Command("wget", "-O", "/bin/aiapi", aiApiS3URL)
			if err := aiApiInstallCmd.Run(); err != nil {
				logger.Error("Failed to download aiapi", zap.Error(err))
				return
			}
		}

		// bashCmd := exec.Command("which", "bash")
		// shellType := "sh"
		// if err := bashCmd.Run(); err == nil {
		// 	shellType = "bash"
		// }
		// if shellType == "sh" {
		// 	redisCmd := exec.Command("redis-server", "--daemonize", "yes")
		// 	redisOutput, err := redisCmd.CombinedOutput()
		// 	if err != nil {
		// 		logger.Error("Failed to start Redis server", zap.Error(err), zap.String("output", string(redisOutput)))
		// 		return
		// 	}
		// 	logger.Info("Redis server started in daemonized mode", zap.String("output", string(redisOutput)))
		// } else {
		// 	// Start Redis server in daemonized mode
		// 	redisCmd := exec.Command("sh", "-c", "redis-server --daemonize yes")
		// 	redisOutput, err := redisCmd.CombinedOutput()
		// 	if err != nil {
		// 		logger.Error("Failed to start Redis server", zap.Error(err), zap.String("output", string(redisOutput)))
		// 		return
		// 	}
		// 	logger.Info("Redis server started in daemonized mode", zap.String("output", string(redisOutput)))
		// }

		redisCmd := exec.Command("redis-server", "--daemonize", "yes")
		redisOutput, err := redisCmd.CombinedOutput()
		if err != nil {
			logger.Error("Failed to start Redis server", zap.Error(err), zap.String("output", string(redisOutput)))
			return
		}
		logger.Info("Redis server started in daemonized mode", zap.String("output", string(redisOutput)))

		filePath := "/bin/aiapi" // Replace with your file path

		// Retrieve the file information to get its current permissions.
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			fmt.Printf("Error retrieving file info: %v\n", err)
			return
		}

		// Retrieve the current mode and add the executable bits (owner, group, others)
		newMode := fileInfo.Mode() | 0111

		// Apply the new permissions to the file.
		err = os.Chmod(filePath, newMode)
		if err != nil {
			logger.Error(fmt.Sprintf("Error changing file permissions: %v\n", err))
			return
		}

		logger.Info("File permissions updated, the file is now executable.")

		RunAiApiCommand("/bin/aiapi", false, logger)
	}()

	time.Sleep(time.Duration(10) * time.Second)
	return nil
}
