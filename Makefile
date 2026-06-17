BINARY := gozik
CMD := ./cmd/gozik
PREFIX ?= /usr/local
BINDIR := $(DESTDIR)$(PREFIX)/bin
DATADIR := $(DESTDIR)$(PREFIX)/share
ICONDIR := $(DATADIR)/icons/hicolor
APPLICATIONSDIR := $(DATADIR)/applications

ICON_SIZES := 16 22 24 32 48 64 128 256 512

LDFLAGS := -ldflags "-X github.com/gg582/gozik/internal/config.Prefix=$(PREFIX)"

.PHONY: build run clean deps test icons install uninstall proto installer installer-all

build:
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf assets/icons
	rm -rf installer/dist

deps:
	go mod tidy
	cd installer && go mod tidy

installer:
	cd installer && CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/gozik-installer-$(shell go env GOOS)-$(shell go env GOARCH) .

installer-all:
	./installer/scripts/build.sh

test:
	go test ./...

PROTOC := protoc
PROTO_DIRS := api/music/v1 api/player/v1

proto:
	@for dir in $(PROTO_DIRS); do \
		for file in $$dir/*.proto; do \
			$(PROTOC) --go_out=. --go_opt=paths=source_relative \
				--go-grpc_out=. --go-grpc_opt=paths=source_relative \
				$$file; \
		done; \
	done

icons:
	@mkdir -p assets/icons/hicolor
	@for size in $(ICON_SIZES); do \
		dir=assets/icons/hicolor/$${size}x$${size}/apps; \
		mkdir -p $$dir; \
		convert assets/ui/hi-res/icon.png -resize $${size}x$${size} $$dir/gozik.png; \
	done

install: icons
	@test -f $(BINARY) || $(MAKE) build
	install -Dm755 $(BINARY) $(BINDIR)/$(BINARY)
	install -Dm644 assets/ui/gozik-main-window.glade $(DATADIR)/gozik/ui/gozik-main-window.glade
	install -Dm644 assets/ui/gozik.css $(DATADIR)/gozik/ui/gozik.css
	install -Dm644 assets/ui/stars.png $(DATADIR)/gozik/ui/stars.png
	install -Dm644 assets/gozik.desktop $(APPLICATIONSDIR)/gozik.desktop
	@for size in $(ICON_SIZES); do \
		install -Dm644 assets/icons/hicolor/$${size}x$${size}/apps/gozik.png $(ICONDIR)/$${size}x$${size}/apps/gozik.png; \
	done
	touch $(ICONDIR) || true
	gtk-update-icon-cache -f -t $(ICONDIR) || true
	update-desktop-database -q $(APPLICATIONSDIR) || true

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -rf $(DATADIR)/gozik
	rm -f $(APPLICATIONSDIR)/gozik.desktop
	@for size in $(ICON_SIZES); do \
		rm -f $(ICONDIR)/$${size}x$${size}/apps/gozik.png; \
	done
	touch $(ICONDIR) || true
	gtk-update-icon-cache -f -t $(ICONDIR) || true
	update-desktop-database -q $(APPLICATIONSDIR) || true
