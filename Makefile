PACKAGE := srl-timeline
VERSION := 0.0.4
ARCH := amd64
DEB_FILE := dist/$(PACKAGE)_$(VERSION)_$(ARCH).deb

.PHONY: all build test deb clean

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -ldflags="-s -w" -o bin/timeline .

test:
	go test -v -race ./...

deb: build
	@rm -rf build/deb && mkdir -p dist
	@mkdir -p build/deb/DEBIAN
	@mkdir -p build/deb/usr/local/bin
	@mkdir -p build/deb/etc/opt/srlinux/appmgr
	@cp bin/timeline build/deb/usr/local/bin/timeline
	@chmod 0755 build/deb/usr/local/bin/timeline
	@cp timeline_config.yml build/deb/etc/opt/srlinux/appmgr/timeline_config.yml
	@chmod 0644 build/deb/etc/opt/srlinux/appmgr/timeline_config.yml
	@printf 'Package: %s\nVersion: %s\nSection: net\nPriority: optional\nArchitecture: %s\nRecommends: git\nMaintainer: Andy Whitaker <32944047+andywhitaker@users.noreply.github.com>\nDescription: Configuration timeline and version history tool for Nokia SR Linux\n' "$(PACKAGE)" "$(VERSION)" "$(ARCH)" > build/deb/DEBIAN/control
	@printf '#!/bin/sh\nset -e\nrm -f /etc/sudoers.d/srl-timeline 2>/dev/null || true\nmkdir -p /etc/opt/srlinux/timeline\nchgrp -R ntwkadmin /etc/opt/srlinux/timeline 2>/dev/null || chgrp -R srlinux /etc/opt/srlinux/timeline 2>/dev/null || true\nchmod 02775 /etc/opt/srlinux/timeline || true\nchmod -R g+rwX /etc/opt/srlinux/timeline 2>/dev/null || true\nif command -v setfacl >/dev/null 2>&1; then\n    setfacl -R -m g::rwX,g:ntwkuser:rwX,g:ntwkadmin:rwX,g:srlinux:rwX /etc/opt/srlinux/timeline 2>/dev/null || true\n    find /etc/opt/srlinux/timeline -type d -exec setfacl -d -m g::rwx,g:ntwkuser:rwx,g:ntwkadmin:rwx,g:srlinux:rwx {} + 2>/dev/null || true\n    if [ -d /opt/srlinux/var/run ]; then\n        setfacl -m g:ntwkuser:rwx,g:ntwkadmin:rwx,g:srlinux:rwx,o::rwx /opt/srlinux/var/run 2>/dev/null || true\n        setfacl -d -m g:ntwkuser:rwx,g:ntwkadmin:rwx,g:srlinux:rwx,o::rwx /opt/srlinux/var/run 2>/dev/null || true\n    fi\nfi\n/usr/local/bin/timeline daemon bootstrap >/dev/null 2>&1 || true\nchmod 0777 /opt/srlinux/var/run/sr_json_rpc 2>/dev/null || true\nexit 0\n' > build/deb/DEBIAN/postinst
	@chmod 0755 build/deb/DEBIAN/postinst
	dpkg-deb --root-owner-group --build build/deb $(DEB_FILE)
	@rm -rf build/deb

clean:
	rm -rf bin/ build/ dist/
