all: saltctl

saltctl: client.go util.go
	go get code.google.com/p/go.crypto/ssh/terminal
	go build

install:
	go get -u github.com/hdonnay/saltctl
	go install github.com/hdonnay/saltctl
