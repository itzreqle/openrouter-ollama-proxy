APP      := orollama
BIN_DIR  := bin
MODULE   := github.com/itzreqle/openrouter-ollama-proxy

.PHONY: build run test tidy clean install

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP) ./cmd/orollama

run: build
	./$(BIN_DIR)/$(APP)

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

install: build
	install -m 755 $(BIN_DIR)/$(APP) $(HOME)/.local/bin/$(APP)
