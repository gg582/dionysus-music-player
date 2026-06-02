BINARY := dionysus
CMD := ./cmd/dionysus
PREFIX ?= /usr/local
BINDIR := $(DESTDIR)$(PREFIX)/bin
DATADIR := $(DESTDIR)$(PREFIX)/share
ICONDIR := $(DATADIR)/icons/hicolor
APPLICATIONSDIR := $(DATADIR)/applications

ICON_SIZES := 16 22 24 32 48 64 128 256 512

LDFLAGS := -ldflags "-X github.com/gg582/dionysus-music-player/internal/config.Prefix=$(PREFIX)"

.PHONY: build run clean deps test icons install uninstall

build:
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf assets/icons

deps:
	go mod tidy

test:
	go test ./...

icons:
	@mkdir -p assets/icons/hicolor
	@for size in $(ICON_SIZES); do \
		dir=assets/icons/hicolor/$${size}x$${size}/apps; \
		mkdir -p $$dir; \
		convert assets/ui/hi-res/icon.png -resize $${size}x$${size} $$dir/dionysus.png; \
	done

install: icons
	@test -f $(BINARY) || { echo "$(BINARY) not found. Run 'make build' first."; exit 1; }
	install -Dm755 $(BINARY) $(BINDIR)/$(BINARY)
	install -Dm644 assets/ui/dionysus-main-window.glade $(DATADIR)/dionysus/ui/dionysus-main-window.glade
	install -Dm644 assets/ui/dionysus.css $(DATADIR)/dionysus/ui/dionysus.css
	install -Dm644 assets/dionysus.desktop $(APPLICATIONSDIR)/dionysus.desktop
	@for size in $(ICON_SIZES); do \
		install -Dm644 assets/icons/hicolor/$${size}x$${size}/apps/dionysus.png $(ICONDIR)/$${size}x$${size}/apps/dionysus.png; \
	done
	gtk-update-icon-cache -q $(ICONDIR) || true
	update-desktop-database $(APPLICATIONSDIR) || true

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -rf $(DATADIR)/dionysus
	rm -f $(APPLICATIONSDIR)/dionysus.desktop
	@for size in $(ICON_SIZES); do \
		rm -f $(ICONDIR)/$${size}x$${size}/apps/dionysus.png; \
	done
	gtk-update-icon-cache -q $(ICONDIR) || true
	update-desktop-database $(APPLICATIONSDIR) || true
