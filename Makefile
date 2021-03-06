SOURCES = $(shell find . -name '*.go')

default: build

build: $(SOURCES)
	go build ./...

install: $(SOURCES)
	go install ./...

check: build
	go test -race ./...

shortcheck: build
	go test ./... -test.short -run ^Test

gencover: build
	go test -coverprofile cover.out

cover: gencover
	go tool cover -func cover.out

showcover: gencover
	go tool cover -html cover.out

fmt: $(SOURCES)
	gofmt -w -s ./*.go

vet: $(SOURCES)
	go vet

check-ineffassign: $(SOURCES)
	ineffassign .

check-spell: $(SOURCES) README.md Makefile
	misspell -error README.md Makefile *.go

lint: $(SOURCES)
	golint -set_exit_status -min_confidence 0.9

precommit: build check fmt cover vet check-ineffassign check-spell lint
	# ok
