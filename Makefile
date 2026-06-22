.PHONY: build test lint install clean

build:
	go build -o bin/garmin-connect-workout-cli ./cmd/garmin-connect-workout-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/garmin-connect-workout-cli

clean:
	rm -rf bin/
