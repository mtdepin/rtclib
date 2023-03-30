

signal-server:
	rm -rf build/signal-server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/signal-server examples/signal-server/main.go