APP      := orollama
BIN_DIR  := bin
MODULE   := github.com/itzreqle/openrouter-ollama-proxy
ZSHRC    := $(HOME)/.zshrc
BASHRC   := $(HOME)/.bashrc
HOOK_MARK := orollama-claude shell hook

# Prefer PATH go; fall back to mise (common when mise isn't activated in the shell).
GO ?= $(shell command -v go 2>/dev/null)
ifeq ($(GO),)
  GO := mise exec -- go
endif

.PHONY: build run test tidy clean install install-cli claude

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/$(APP) ./cmd/orollama

run: build
	./$(BIN_DIR)/$(APP)

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR)

install: build
	install -m 755 $(BIN_DIR)/$(APP) $(HOME)/.local/bin/$(APP)

# Symlink CLI + wire plain `claude` through the proxy when it is running.
install-cli:
	@mkdir -p $(HOME)/.local/bin
	chmod +x scripts/claude-orollama
	ln -sfn "$(CURDIR)/scripts/claude-orollama" $(HOME)/.local/bin/claude-orollama
	@echo "Linked $(HOME)/.local/bin/claude-orollama"
	@# zsh
	@if [ -f "$(ZSHRC)" ] && grep -qF "$(HOOK_MARK)" "$(ZSHRC)"; then \
		echo "zsh hook already present in $(ZSHRC)"; \
	else \
		printf '\n# %s\n[[ -f "%s/scripts/shell/orollama-claude.zsh" ]] && source "%s/scripts/shell/orollama-claude.zsh"\n' \
			"$(HOOK_MARK)" "$(CURDIR)" "$(CURDIR)" >> "$(ZSHRC)"; \
		echo "Added zsh hook to $(ZSHRC)"; \
	fi
	@# bash (optional)
	@if [ -f "$(BASHRC)" ] && grep -qF "$(HOOK_MARK)" "$(BASHRC)"; then \
		echo "bash hook already present in $(BASHRC)"; \
	elif [ -f "$(BASHRC)" ]; then \
		printf '\n# %s\n[ -f "%s/scripts/shell/orollama-claude.bash" ] && . "%s/scripts/shell/orollama-claude.bash"\n' \
			"$(HOOK_MARK)" "$(CURDIR)" "$(CURDIR)" >> "$(BASHRC)"; \
		echo "Added bash hook to $(BASHRC)"; \
	fi
	@echo "Reload your shell:  source ~/.zshrc"
	@echo "Then:  make run   # terminal 1"
	@echo "       claude     # terminal 2 (auto-uses proxy when up)"

# From this repo: make claude   (or make claude ARGS='--help')
claude:
	./scripts/claude-orollama $(ARGS)
