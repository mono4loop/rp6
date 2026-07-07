BINARY_NAME := rp6

# Build tags:
#   capture         live USB-audio VU metering via malgo/miniaudio
#   wayland         build Fyne's glfw driver against native Wayland
#   migrated_fynedo opt into Fyne 2.8's fyne.Do threading model (all our
#                   background->UI updates already go through fyne.Do), which
#                   silences the startup migration warning
# All three are on by default. Override for a different combination, e.g.
# `make run TAGS=capture` (X11 driver) or `make run TAGS=wayland` (no audio).
# Tests deliberately run without tags so Fyne's thread-safety checks stay on.
TAGS ?= capture wayland migrated_fynedo

.PHONY: build install run test fmt vet check clean serve web android android-release

build:
	go build -tags "$(TAGS)" -ldflags "-s -w" -o build/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

install:
	go install -tags "$(TAGS)" ./cmd/$(BINARY_NAME)

run:
	go run -tags "$(TAGS)" ./cmd/$(BINARY_NAME)

# --- Web (WebAssembly) ------------------------------------------------------
# The browser build is emulator-only (no USB/ALSA in a browser): the built-in
# "modular-hits" kit plays through Web Audio. No capture/wayland tags (cgo isn't
# available on js/wasm; audio is the syscall/js sink, not malgo).
WEB_TAGS ?= migrated_fynedo

# serve: build the web bundle and serve it locally on http://localhost:8080 with
# the cross-origin isolation headers the AudioWorklet audio path needs (glitch-
# free audio). Falls back to the ScriptProcessor sink automatically if a host
# doesn't set those headers.
serve: web
	go run ./cmd/webserve build/web

# web: produce a static bundle in build/web (rp6.wasm + wasm_exec.js + index.html)
# for hosting anywhere. Audio needs a user gesture (tap a pad) to start.
web:
	mkdir -p build/web
	GOOS=js GOARCH=wasm go build -tags "$(WEB_TAGS)" -ldflags "-s -w" -o build/web/rp6.wasm ./cmd/$(BINARY_NAME)
	cp "$(shell go env GOROOT)/lib/wasm/wasm_exec.js" build/web/
	cp web/index.html build/web/
	@echo "built build/web — serve it, e.g.: (cd build/web && python3 -m http.server 8080)"

# --- Android ----------------------------------------------------------------
# Build an APK with the built-in emulator (audio via malgo/miniaudio -> AAudio).
# Needs the fyne tool and the Android SDK + NDK (set ANDROID_HOME/ANDROID_NDK_HOME).
# ANDROID_ABI selects the target ABI: android/amd64 for the x86_64 emulator,
# android/arm64 for phones, or `android` for all ABIs.
ANDROID_ABI ?= android/amd64

# Signing/versioning for android-release. Override KEYSTORE/STOREPASS/KEYPASS
# with your own for a real distribution; the defaults generate a throwaway dev
# keystore (fine for sideloading, not for the Play Store).
# Signing/versioning for android-release. Override KEYSTORE/STOREPASS/KEYPASS/
# KEY_ALIAS via the environment (e.g. in CI, from secrets) to sign with your own
# key. The defaults generate a throwaway dev keystore (fine for local sideloading,
# not for a real release). Use the SAME keystore locally and in CI so upgrades
# install over each other (see .github/workflows/android.yml).
KEYSTORE  ?= rp6-release.keystore
KEY_ALIAS ?= rp6
STOREPASS ?= android
KEYPASS   ?= android
APP_VERSION ?= 0.1.0
APP_BUILD   ?= 1
# Absolute keystore path (works from repo root and from cmd/rp6).
KEYSTORE_PATH := $(abspath $(KEYSTORE))

# _android_ndk / _android_bt resolve the NDK and build-tools dirs.
_ANDROID_SDK = $${ANDROID_HOME:-$$HOME/Android/Sdk}

android:
	@ndk="$${ANDROID_NDK_HOME:-$$(ls -d $(_ANDROID_SDK)/ndk/* 2>/dev/null | sort -V | tail -n1)}"; \
	if [ -z "$$ndk" ] || [ ! -d "$$ndk" ]; then \
		echo "error: no Android NDK found — set ANDROID_NDK_HOME or install one under \$$ANDROID_HOME/ndk"; exit 1; \
	fi; \
	echo "using NDK: $$ndk"; \
	mkdir -p build/android; \
	cd cmd/$(BINARY_NAME) && ANDROID_NDK_HOME="$$ndk" fyne package -os $(ANDROID_ABI) \
		--tags "capture,migrated_fynedo" \
		--icon ../../web/icon.png --app-id io.github.mono4loop.rp6 --name RP6 && \
	mv -f RP6.apk ../../build/android/RP6.apk && \
	echo "built build/android/RP6.apk"

# android-release: a signed, modern-target (targetSdkVersion 35) APK for
# distribution/sideloading. `fyne package` (make android) only targets API 29
# for debug builds, which newer Android warns about; `fyne release` targets 35
# but builds a Play-Store AAB needing bundletool — so we take the target-35 APK
# it produces and re-sign it (zipalign + apksigner v2/v3) ourselves. A throwaway
# dev keystore is generated on first use (override KEYSTORE/STOREPASS/KEYPASS).
android-release:
	@set -e; \
	ndk="$${ANDROID_NDK_HOME:-$$(ls -d $(_ANDROID_SDK)/ndk/* 2>/dev/null | sort -V | tail -n1)}"; \
	[ -d "$$ndk" ] || { echo "error: no Android NDK found"; exit 1; }; \
	bt="$$(ls -d $(_ANDROID_SDK)/build-tools/* 2>/dev/null | sort -V | tail -n1)"; \
	[ -d "$$bt" ] || { echo "error: no Android build-tools found"; exit 1; }; \
	if [ ! -f "$(KEYSTORE_PATH)" ]; then \
		echo "generating dev keystore $(KEYSTORE_PATH)"; \
		keytool -genkeypair -v -keystore "$(KEYSTORE_PATH)" -alias "$(KEY_ALIAS)" -keyalg RSA \
			-keysize 2048 -validity 10000 -storepass "$(STOREPASS)" -keypass "$(KEYPASS)" -dname "CN=rp6"; \
	fi; \
	echo "using NDK: $$ndk"; echo "using build-tools: $$bt"; \
	echo "note: 'fyne release' may report a missing 'bundletool' while trying to build a"; \
	echo "      Play Store AAB — that is expected and harmless; we produce and sign the"; \
	echo "      target-35 APK ourselves below. bundletool is NOT required for the APK."; \
	mkdir -p build/android; \
	( cd cmd/$(BINARY_NAME) && ANDROID_NDK_HOME="$$ndk" fyne release -os $(ANDROID_ABI) \
		--tags "capture,migrated_fynedo" --icon ../../web/icon.png \
		--app-id io.github.mono4loop.rp6 --name RP6 --app-version $(APP_VERSION) --app-build $(APP_BUILD) \
		--keystore "$(KEYSTORE_PATH)" --keystore-pass $(STOREPASS) --key-name $(KEY_ALIAS) --key-pass $(KEYPASS) ) || true; \
	[ -f cmd/$(BINARY_NAME)/RP6.apk ] || { echo "error: fyne release produced no APK"; exit 1; }; \
	"$$bt/zipalign" -f -p 4 cmd/$(BINARY_NAME)/RP6.apk build/android/RP6-aligned.apk; \
	"$$bt/apksigner" sign --ks "$(KEYSTORE_PATH)" --ks-pass pass:$(STOREPASS) --ks-key-alias $(KEY_ALIAS) \
		--key-pass pass:$(KEYPASS) --out build/android/RP6.apk build/android/RP6-aligned.apk; \
	rm -f build/android/RP6-aligned.apk cmd/$(BINARY_NAME)/RP6.apk; \
	"$$bt/apksigner" verify build/android/RP6.apk >/dev/null && \
	echo "built + signed build/android/RP6.apk (targetSdkVersion 35, $(ANDROID_ABI))"


# Tests run without build tags so they need no audio backend.
test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test
	staticcheck ./...
	staticcheck -tags "$(TAGS)" ./internal/audio/...

clean:
	rm -rf build
