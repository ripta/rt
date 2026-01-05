generate-proto:
	protoc --go_out=. --go_opt=paths=source_relative ./samples/data/v1/data.proto

imports:
	goimports -w -l -local github.com/ripta/rt .

install:
	go install -v ./cmd/...

install-protoc:
	go install -v google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1

hyper:
	go install -v ./hypercmd/rt

fast-test:
	go test -v -tags skipnative ./...

test:
	go test -v ./...

samples:
	echo ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghijklmnopqrstuvwxyz 0123456789 | go run ./cmd/uni map -a > samples/uni.txt

.PHONY: generate-proto imports install install-protoc hyper fast-test test samples
