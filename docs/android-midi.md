# Android USB MIDI (P-6 + MacroPad)

Status: **Implemented and building (APK via `make android`); needs on-device
validation with real USB-MIDI hardware.** See kata `cr6e`.

rp6 reads USB-MIDI on Android **entirely from Go** — no Gradle project and no
custom Java. This keeps the existing `make android` / `fyne package` flow and
the full Fyne UI, while adding USB-MIDI input from plugged-in gear (an Adafruit
MacroPad, a P-6, …).

---

## Why not the Gradle/MidiManager route?

Android forbids `/dev/snd`, so desktop rp6's ALSA path can't work. The obvious
route (Java `MidiManager`) needs a `MidiReceiver` **subclass** and USB
`res/xml`, neither of which the `fyne package` toolchain can add (it bakes a
prebuilt `classes.dex` and only a minimal resource table). A full Gradle rewrite
would fix that but is a large change with an unproven "Fyne inside a custom
Activity" step.

Instead we use two facts:

- The phone in **USB-host** mode exposes plugged-in USB-MIDI gear through
  `android.hardware.usb` (`UsbManager`, `UsbDeviceConnection.bulkTransfer`, …) —
  all **concrete** Java methods.
- Fyne exposes **`fyne.io/fyne/v2/driver.RunNative`**, giving Go the JVM +
  Context so it can call those methods over **JNI**.

So Go drives USB directly: enumerate devices → request USB permission (a runtime
dialog, no manifest `res/xml` needed) → open the MIDI-streaming interface's bulk
**IN** endpoint → read 32-bit USB-MIDI packets → decode to a raw MIDI stream →
feed the `midibridge` package. The existing `p6` / `midiin` backends consume the
bridge unchanged.

```
   USB-MIDI device (MacroPad / P-6)
            │  USB bulk IN (USB-MIDI event packets) ▲ USB bulk OUT
            ▼                                        │
   internal/androidusb  (//go:build android, cgo/JNI via driver.RunNative)
     • UsbManager.getDeviceList / requestPermission / openDevice
     • bulkTransfer IN loop → goUSBData → DecodeUSBMIDI (pure Go)
     • rp6_usb_send ← EncodeUSBMIDI ← midibridge OutputPort (P-6 output)
            │  midibridge.AddDevice + PushInput + SetOutput
            ▼
   midibridge  ──►  internal/midiin/macropad (android)  ──►  pads / transport
               └►  p6/device_android.go (P-6 in + out)
```

## Implemented pieces

- **`internal/androidusb/`**
  - `usbmidi.go` — `DecodeUSBMIDI`, the pure-Go USB-MIDI→raw-MIDI packet decoder
    (CIN length table per the USB MIDI class spec). Unit-tested on the host.
  - `androidusb_android.go` (`//go:build android`) — the cgo/JNI reader:
    `Start()` grabs the JVM/Context via `driver.RunNative`, then a background
    thread (attached to the JVM) scans for a MIDI device, requests permission,
    and pumps its bulk-IN endpoint into Go.
  - `androidusb_cb.go` — the `//export` callbacks (`goUSBDevice/goUSBData/
    goUSBRemove/goUSBLog`) that decode + feed `midibridge`.
  - `androidusb_stub.go` (`!android`) — `Start` is a no-op elsewhere.
- **`midibridge/`** — the transport bridge (unchanged; here it's fed by Go, not
  Java). `AddDevice`/`PushInput`/`RemoveDevice` + `OpenReader`/`Writer`.
- **`p6/device_android.go`** — P-6 backend over the bridge (input wired; output
  is the remaining TODO, see below).
- **`internal/midiin/macropad/macropad_android.go`** — MacroPad driver reads the
  bridge instead of an ALSA node; shared MIDI→Handlers mapping.
- **`cmd/rp6/android.go`** — `startAndroidMIDI()`: starts the USB reader, marks
  the emulator start as a fallback, runs the device watcher, and **retries the
  input-controller attach** every 2 s (USB devices appear asynchronously, after
  the permission dialog).

Everything builds: `go test ./...` (host, incl. the decoder), a full
`CGO_ENABLED=1 GOOS=android` build, and `make android ANDROID_ABI=android/arm64`
(produces `build/android/RP6.apk` with the arm64 lib).

## Build & install

```bash
make android ANDROID_ABI=android/arm64     # phone; -> build/android/RP6.apk
adb install -r build/android/RP6.apk
adb logcat -s rp6usb GoLog Fyne            # watch the USB-MIDI reader logs
```

(Use `android/amd64` for the x86_64 emulator, but the emulator can't pass
through real USB MIDI — validate on a physical phone.)

## Validating with a MacroPad

1. Plug the MacroPad into the phone (USB-C OTG / hub). A USB-permission dialog
   should appear — tap **OK/Always**. `logcat` should show
   `rp6usb: requested USB permission` then `rp6usb: reading MIDI`.
2. Within ~2 s the status bar should read `Adafruit MacroPad RP2040 connected`.
3. Press a MacroPad pad → the matching rp6 pad triggers (plays the built-in
   emulator) and flashes; the rotary-encoder press toggles Play/Stop.
4. Unplug → `rp6usb: device closed`; the app keeps running on the emulator and
   re-attaches if you plug back in.

If the permission dialog never appears, some devices need
`<uses-feature android:name="android.hardware.usb.host"/>` in the manifest —
see "Custom manifest" below.

## Known limitations / remaining work

- **One device at a time.** The scan attaches the first MIDI device it finds;
  multiple simultaneous controllers is a future enhancement.
- **Custom manifest (optional).** If `getDeviceList` is empty / no permission
  dialog on a given phone, add USB host to the manifest. `fyne package` reads an
  `AndroidManifest.xml` placed next to `cmd/rp6/main.go` (it then owns the whole
  manifest, so copy Fyne's generated one first: `fyne package -os android` with
  `-v` prints it). Add `<uses-feature android:name="android.hardware.usb.host"/>`.
- **Cannot be validated in CI / sandbox** (no phone, no USB passthrough). The
  pure-Go decoder is unit-tested; the JNI path is compile-verified only and
  needs on-device iteration — `adb logcat -s rp6usb` is the main tool.

## JNI notes (for future edits)

- `driver.RunNative(func(ctx any) error)` yields `*driver.AndroidContext{VM, Env,
  Ctx}` (all `uintptr`) on a thread with a valid `JNIEnv`. The `Env` is only
  valid on that thread, so `androidusb` makes a **global ref** of the Context
  there and the reader thread does its own `AttachCurrentThread`.
- The reader thread stays attached for its lifetime and wraps each scan in
  `PushLocalFrame/PopLocalFrame` to bound local-ref growth.
- cgo rule: the file with the big C **definitions** (`androidusb_android.go`)
  can't also hold `//export` funcs, so those live in `androidusb_cb.go` with a
  declaration-only preamble.
