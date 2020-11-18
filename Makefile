.PHONY: test
test:
	go test ./...

.PHONY: pb
pb:
	protoc --go_out=. payloads/*.proto
