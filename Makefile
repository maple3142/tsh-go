GOFLAGS_LINUX=-trimpath -ldflags "-s -w"
GOFLAGS_WINDOWS=-trimpath -ldflags "-s -w" #-H=windowsgui"
GOOS ?= linux
GOARCH ?= amd64

DEFAULT_ENV ?= CGO_ENABLED=0

all:
	@echo
	@echo "Please specify one of these targets:"
	@echo "	make linux"
	@echo "	make windows"
	@echo
	@echo "It can be compiled to other unix-like platforms supported by go compiler:"
	@echo "	GOOS=freebsd GOARCH=386 make unix"
	@echo
	@echo "Get more with:"
	@echo "	go tool dist list"
	@echo

./cmd/secret.txt: ./scripts/gen-secret/main.go
	go run ./scripts/gen-secret/main.go > ./cmd/secret.txt

windows: ./cmd/secret.txt
	env ${DEFAULT_ENV} GOOS=windows GOARCH=amd64 go build ${GOFLAGS_WINDOWS} -ldflags "-X main.variant=tshd" -o ./build/tshd_windows_amd64.exe main.go
	env ${DEFAULT_ENV} GOOS=windows GOARCH=amd64 go build ${GOFLAGS_WINDOWS} -o ./build/tsh_windows_amd64.exe main.go

linux: ./cmd/secret.txt
	env ${DEFAULT_ENV} GOOS=linux GOARCH=amd64 go build ${GOFLAGS_LINUX} -ldflags "-X main.variant=tshd" -o ./build/tshd_linux_amd64 main.go
	env ${DEFAULT_ENV} GOOS=linux GOARCH=amd64 go build ${GOFLAGS_LINUX} -o ./build/tsh_linux_amd64 main.go

unix: ./cmd/secret.txt
	env ${DEFAULT_ENV} GOOS=${GOOS} GOARCH=${GOARCH} go build ${GOFLAGS_LINUX} -ldflags "-X main.variant=tshd" -o ./build/tshd_${GOOS}_${GOARCH} main.go
	env ${DEFAULT_ENV} GOOS=${GOOS} GOARCH=${GOARCH} go build ${GOFLAGS_LINUX} -o ./build/tsh_${GOOS}_${GOARCH} main.go

clean:
	rm -f ./build/*

clean-secret:
	rm -f ./cmd/secret.txt

.PHONY: all clean windows linux unix
