.PHONY: install build clean

install: build
	@mkdir -p ~/.local/bin
	@cp editor-bin ~/.local/bin/aln
	@echo "Installed to ~/.local/bin/aln"

build:
	@go build -o editor-bin .

clean:
	@rm -f editor-bin
