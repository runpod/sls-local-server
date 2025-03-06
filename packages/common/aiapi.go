package common

import (
	"fmt"
	"os"
	"os/exec"

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
			aiApiInstallCmd := exec.Command("curl", "-fsSL", aiApiS3URL, "-o", "/aiapi")
			if err := aiApiInstallCmd.Run(); err != nil {
				logger.Error("Failed to download aiapi", zap.Error(err))
				return
			}
		} else {
			aiApiInstallCmd := exec.Command("wget", "-O", "/aiapi", aiApiS3URL)
			if err := aiApiInstallCmd.Run(); err != nil {
				logger.Error("Failed to download aiapi", zap.Error(err))
				return
			}
		}

		// Start Redis server in daemonized mode
		redisCmd := exec.Command("sh", "-c", "redis-server --daemonize yes")
		redisOutput, err := redisCmd.CombinedOutput()
		if err != nil {
			logger.Error("Failed to start Redis server", zap.Error(err), zap.String("output", string(redisOutput)))
			return
		}
		logger.Info("Redis server started in daemonized mode", zap.String("output", string(redisOutput)))

		RunCommand("chmod +x /aiapi && /aiapi", false, logger)
	}()

	return nil
}
