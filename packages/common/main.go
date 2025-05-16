package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"sls-local-server/packages/vars"
	"strings"
	"sync"
	"time"

	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var Mutex = &sync.Mutex{}
var GqlMutex = &sync.Mutex{}

var (
	log        *zap.Logger
	testConfig []Test
)

func init() {
	var err error
	if os.Getenv("ENV") == "local" {
		log = prettyconsole.NewLogger(zap.DebugLevel)
	} else {
		log, err = zap.NewProduction()
		if err != nil {
			panic("Failed to initialize logger: " + err.Error())
		}
	}
}

func SendResultsToGraphQL(status string, errorReason *string, log *zap.Logger, results []Result) {
	GqlMutex.Lock()
	defer GqlMutex.Unlock()

	runpodPodId := os.Getenv("RUNPOD_POD_ID")
	jwtToken := os.Getenv("RUNPOD_JWT_TOKEN")
	runpodTestId := os.Getenv("RUNPOD_TEST_ID")
	webhookUrl := os.Getenv("RUNPOD_TEST_WEBHOOK_URL")

	if webhookUrl == "" {
		log.Error("RUNPOD_TEST_WEBHOOK_URL not set")
		return
	}

	time.Sleep(time.Duration(40) * time.Second)

	jsonData, err := json.Marshal(map[string]interface{}{
		"podId":   runpodPodId,
		"testId":  runpodTestId,
		"results": results,
		"status":  status,
		"error":   errorReason,
	})
	if err != nil {
		log.Error("Failed to marshal results", zap.Error(err))
		return
	}

	req, err := http.NewRequest("POST", webhookUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error("Failed to create request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)

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

	time.Sleep(30 * time.Second)
	log.Info("Results sent to GraphQL", zap.Any("results", results))
}

func SendLogsToTinyBird(logBuffer chan string, log *zap.Logger) {
	buffer := make([]map[string]interface{}, 0)
	tinybirdToken := os.Getenv("RUNPOD_TINYBIRD_TOKEN")
	runpodPodId := os.Getenv("RUNPOD_POD_ID")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var testNumber = vars.CURRENT_TEST_ID

	for {
		select {
		case logMsg, ok := <-logBuffer:
			if !ok {
				log.Error("log buffer closed")
				continue
			}

			if logMsg == "" {
				continue
			}

			testNumber = vars.CURRENT_TEST_ID
			level := "info"
			logMessageList := strings.Split(logMsg, "\n")

			for _, logMessage := range logMessageList {
				fmt.Println("logMsg: ### ", logMessage)
				if logMessage == "" {
					continue
				}
				if strings.HasPrefix(logMessage, "#ERROR:") {
					level = "error"
					logMessage = strings.TrimPrefix(logMessage, "#ERROR:")
				}
				logEntry := map[string]interface{}{
					"testId":     os.Getenv("RUNPOD_TEST_ID"),
					"level":      level,
					"podId":      runpodPodId,
					"testNumber": testNumber,
					"message":    logMessage,
					"timestamp":  time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
				}

				buffer = append(buffer, logEntry)
			}

			if len(buffer) >= 16 {
				sendLogs(buffer, tinybirdToken, log)
				buffer = make([]map[string]interface{}, 0)
			}
		case <-ticker.C:
			if len(buffer) > 0 {
				sendLogs(buffer, tinybirdToken, log)
				buffer = make([]map[string]interface{}, 0)
			}
		}
	}
}

func sendLogs(buffer []map[string]interface{}, token string, log *zap.Logger) {
	url := "https://api.us-east.tinybird.co/v0/events?wait=true&name=sls_test_logs_v1"

	var records []string
	for _, entry := range buffer {
		jsonBytes, err := json.Marshal(entry)
		if err == nil {
			records = append(records, string(jsonBytes))
		}
	}
	payload := strings.Join(records, "\n")

	defer func() {
		if r := recover(); r != nil {
			log.Error("Recovered from panic in log sending",
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())))
		}
	}()

	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		log.Error("Failed to create request", zap.Error(err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 2 * time.Second}
	_, err = client.Do(req)
	if err != nil {
		log.Error("Failed to send logs to tinybird", zap.Error(err))
	}
}
