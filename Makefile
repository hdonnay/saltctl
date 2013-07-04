GOFILES = client.go util.go
all: saltctl

saltctl: $(GOFILES)
	go get code.google.com/p/go.crypto/ssh/terminal
	go build

install:
	go get -u github.com/hdonnay/saltctl
	go install github.com/hdonnay/saltctl

doc:
	mango-doc -name saltctl . | nroff -man > saltctl.1

.PHONY: doc install
