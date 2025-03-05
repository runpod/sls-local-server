package common

import (
	"os"
	"os/exec"

	"go.uber.org/zap"
)

func InstallAndRunAiApi(logger *zap.Logger, test bool) error {
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
			return err
		}
	} else {
		aiApiInstallCmd := exec.Command("wget", "-O", "/aiapi", aiApiS3URL)
		if err := aiApiInstallCmd.Run(); err != nil {
			logger.Error("Failed to download aiapi", zap.Error(err))
			return err
		}
	}

	cmd := "chmod +x /aiapi && AI_API_REDIS_ADDR=127.0.0.1:6379 AGENT_REDIS_ADDR=127.0.0.1:6379 AI_API_REDIS_ADDR=127.0.0.1:6379 AI_API_REDIS_PASS= HOST_ACCESS_TOKEN=test ENV=local /aiapi"
	// err := exec.Command("sh", "-c", cmd).Run()
	if !test {
		go RunCommand(cmd, false, logger)
	} else {
		RunCommand(cmd, false, logger)
	}

	return nil
}
