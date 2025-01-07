VERSION ?= dev

.PHONY: build
build:
	go build -ldflags="-X 'main.Version=$(VERSION)'" -o main main.go

.PHONY: build-arm64
build-arm64:
	env GOARCH=arm64 go build -ldflags="-X 'main.Version=$(VERSION)'" -o main main.go


# sudo docker run -e RUNPOD_TEST=true -e RUNPOD_TEST_FILE=runpod.tests.json -e RUNPOD_ENDPOINT_BASE_URL=http://0.0.0.0:19981//v2 -e RUNPOD_WEBHOOK_GET_JOB=http://0.0.0.0:19981/v2//job-take/$RUNPOD_POD_ID -e RUNPOD_WEBHOOK_POST_OUTPUT=http://0.0.0.0:19981/v2//job-done/$RUNPOD_POD_ID/$ID -e RUNPOD_POD_ID=1234 pierre781/simple:tag