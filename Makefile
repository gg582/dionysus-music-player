BINARY := gozik
CMD := ./cmd/gozik
PREFIX ?= /usr/local
BINDIR := $(DESTDIR)$(PREFIX)/bin
DATADIR := $(DESTDIR)$(PREFIX)/share
ICONDIR := $(DATADIR)/icons/hicolor
APPLICATIONSDIR := $(DATADIR)/applications

ICON_SIZES := 16 22 24 32 48 64 128 256 512

LDFLAGS := -ldflags "-X github.com/gg582/gozik/internal/config.Prefix=$(PREFIX)"

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
		convert assets/ui/hi-res/icon.png -resize $${size}x$${size} $$dir/gozik.png; \
	done

install: icons
	@test -f $(BINARY) || { echo "$(BINARY) not found. Run 'make build' first."; exit 1; }
	install -Dm755 $(BINARY) $(BINDIR)/$(BINARY)
	install -Dm644 assets/ui/gozik-main-window.glade $(DATADIR)/gozik/ui/gozik-main-window.glade
	install -Dm644 assets/ui/gozik.css $(DATADIR)/gozik/ui/gozik.css
	install -Dm644 assets/ui/stars.png $(DATADIR)/gozik/ui/stars.png
	install -Dm644 assets/gozik.desktop $(APPLICATIONSDIR)/gozik.desktop
	@for size in $(ICON_SIZES); do \
		install -Dm644 assets/icons/hicolor/$${size}x$${size}/apps/gozik.png $(ICONDIR)/$${size}x$${size}/apps/gozik.png; \
	done
	gtk-update-icon-cache -q $(ICONDIR) || true
	update-desktop-database $(APPLICATIONSDIR) || true

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -rf $(DATADIR)/gozik
	rm -f $(APPLICATIONSDIR)/gozik.desktop
	@for size in $(ICON_SIZES); do \
		rm -f $(ICONDIR)/$${size}x$${size}/apps/gozik.png; \
	done
	gtk-update-icon-cache -q $(ICONDIR) || true
	update-desktop-database $(APPLICATIONSDIR) || true
