all: saltctl

saltctl: client.go
	go get code.google.com/p/go.crypto/ssh/terminal
	go build
