//go:build android

package androidusb

/*
#cgo LDFLAGS: -llog
#include <jni.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pthread.h>
#include <android/log.h>

// Exported Go callbacks (defined in androidusb_cb.go). Declared here (not
// defined) so this file — which carries the C definitions — can be compiled
// alongside the //export file without violating cgo's "preamble may not contain
// definitions when //export is used" rule.
extern void goUSBDevice(char* id, char* name, int hasIn, int hasOut);
extern void goUSBData(char* id, unsigned char* data, int n);
extern void goUSBRemove(char* id);
extern void goUSBOutputReady(char* id);
extern void goUSBLog(char* msg);

#define TAG "rp6usb"
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, TAG, __VA_ARGS__)

// Android USB / MIDI constants.
#define USB_CLASS_AUDIO      1
#define USB_SUBCLASS_MIDI    3
#define USB_DIR_IN        0x80
#define USB_XFER_BULK        2
#define PI_FLAG_IMMUTABLE 0x04000000 // PendingIntent.FLAG_IMMUTABLE

static JavaVM*  g_vm;
static jobject  g_ctx;      // global ref to the Android Context
static jobject  g_usbmgr;   // global ref to UsbManager

// Output side (rp6 -> device). g_conn / g_out_ep are global refs to the active
// device's connection + bulk-OUT endpoint, guarded by g_out_mu so the app's
// send path (rp6_usb_send, any goroutine/thread) can't race the reader thread
// tearing the device down.
static jobject         g_conn;
static jobject         g_out_ep;
static pthread_mutex_t g_out_mu = PTHREAD_MUTEX_INITIALIZER;

// Cached classes (global refs) + method IDs, initialised once per process.
static int      g_ready;
static jclass   c_ctx, c_mgr, c_map, c_coll, c_iter, c_dev, c_intf, c_ep, c_conn, c_pi, c_intent;
static jmethodID m_getService, m_getDeviceList, m_hasPermission, m_requestPermission, m_openDevice;
static jmethodID m_values, m_iterator, m_hasNext, m_next;
static jmethodID m_ifCount, m_getIf, m_devName, m_prodName;
static jmethodID m_ifClass, m_ifSub, m_epCount, m_getEp;
static jmethodID m_epDir, m_epType;
static jmethodID m_claim, m_release, m_bulk, m_close;
static jmethodID m_piGetBroadcast, m_intentInit;

static void log_str(const char* m) { goUSBLog((char*)m); }

// jstr converts a jstring to a freshly-strdup'd C string (caller frees), or NULL.
static char* jstr(JNIEnv* env, jstring s) {
    if (s == NULL) return NULL;
    const char* c = (*env)->GetStringUTFChars(env, s, NULL);
    if (c == NULL) return NULL;
    char* out = strdup(c);
    (*env)->ReleaseStringUTFChars(env, s, c);
    return out;
}

// rp6_usb_setup captures the JavaVM + a global ref to the Context. It MUST run
// on a thread with a valid JNIEnv (i.e. inside Fyne's driver.RunNative).
void rp6_usb_setup(uintptr_t jvm, uintptr_t env_, uintptr_t ctx) {
    g_vm = (JavaVM*)jvm;
    JNIEnv* env = (JNIEnv*)env_;
    g_ctx = (*env)->NewGlobalRef(env, (jobject)ctx);
}

static jclass gclass(JNIEnv* env, const char* name) {
    jclass local = (*env)->FindClass(env, name);
    if (local == NULL) { (*env)->ExceptionClear(env); return NULL; }
    jclass g = (jclass)(*env)->NewGlobalRef(env, local);
    (*env)->DeleteLocalRef(env, local);
    return g;
}

// init_ids resolves every class + method id once. Returns 1 on success.
static int init_ids(JNIEnv* env) {
    if (g_ready) return 1;

    c_ctx    = gclass(env, "android/content/Context");
    c_mgr    = gclass(env, "android/hardware/usb/UsbManager");
    c_map    = gclass(env, "java/util/HashMap");
    c_coll   = gclass(env, "java/util/Collection");
    c_iter   = gclass(env, "java/util/Iterator");
    c_dev    = gclass(env, "android/hardware/usb/UsbDevice");
    c_intf   = gclass(env, "android/hardware/usb/UsbInterface");
    c_ep     = gclass(env, "android/hardware/usb/UsbEndpoint");
    c_conn   = gclass(env, "android/hardware/usb/UsbDeviceConnection");
    c_pi     = gclass(env, "android/app/PendingIntent");
    c_intent = gclass(env, "android/content/Intent");
    if (!c_ctx||!c_mgr||!c_map||!c_coll||!c_iter||!c_dev||!c_intf||!c_ep||!c_conn||!c_pi||!c_intent) {
        log_str("rp6usb: FindClass failed");
        return 0;
    }

    m_getService  = (*env)->GetMethodID(env, c_ctx, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
    m_getDeviceList = (*env)->GetMethodID(env, c_mgr, "getDeviceList", "()Ljava/util/HashMap;");
    m_hasPermission = (*env)->GetMethodID(env, c_mgr, "hasPermission", "(Landroid/hardware/usb/UsbDevice;)Z");
    m_requestPermission = (*env)->GetMethodID(env, c_mgr, "requestPermission", "(Landroid/hardware/usb/UsbDevice;Landroid/app/PendingIntent;)V");
    m_openDevice  = (*env)->GetMethodID(env, c_mgr, "openDevice", "(Landroid/hardware/usb/UsbDevice;)Landroid/hardware/usb/UsbDeviceConnection;");

    m_values   = (*env)->GetMethodID(env, c_map, "values", "()Ljava/util/Collection;");
    m_iterator = (*env)->GetMethodID(env, c_coll, "iterator", "()Ljava/util/Iterator;");
    m_hasNext  = (*env)->GetMethodID(env, c_iter, "hasNext", "()Z");
    m_next     = (*env)->GetMethodID(env, c_iter, "next", "()Ljava/lang/Object;");

    m_ifCount  = (*env)->GetMethodID(env, c_dev, "getInterfaceCount", "()I");
    m_getIf    = (*env)->GetMethodID(env, c_dev, "getInterface", "(I)Landroid/hardware/usb/UsbInterface;");
    m_devName  = (*env)->GetMethodID(env, c_dev, "getDeviceName", "()Ljava/lang/String;");
    m_prodName = (*env)->GetMethodID(env, c_dev, "getProductName", "()Ljava/lang/String;");

    m_ifClass  = (*env)->GetMethodID(env, c_intf, "getInterfaceClass", "()I");
    m_ifSub    = (*env)->GetMethodID(env, c_intf, "getInterfaceSubclass", "()I");
    m_epCount  = (*env)->GetMethodID(env, c_intf, "getEndpointCount", "()I");
    m_getEp    = (*env)->GetMethodID(env, c_intf, "getEndpoint", "(I)Landroid/hardware/usb/UsbEndpoint;");

    m_epDir    = (*env)->GetMethodID(env, c_ep, "getDirection", "()I");
    m_epType   = (*env)->GetMethodID(env, c_ep, "getType", "()I");

    m_claim    = (*env)->GetMethodID(env, c_conn, "claimInterface", "(Landroid/hardware/usb/UsbInterface;Z)Z");
    m_release  = (*env)->GetMethodID(env, c_conn, "releaseInterface", "(Landroid/hardware/usb/UsbInterface;)Z");
    m_bulk     = (*env)->GetMethodID(env, c_conn, "bulkTransfer", "(Landroid/hardware/usb/UsbEndpoint;[BII)I");
    m_close    = (*env)->GetMethodID(env, c_conn, "close", "()V");

    m_piGetBroadcast = (*env)->GetStaticMethodID(env, c_pi, "getBroadcast", "(Landroid/content/Context;ILandroid/content/Intent;I)Landroid/app/PendingIntent;");
    m_intentInit     = (*env)->GetMethodID(env, c_intent, "<init>", "(Ljava/lang/String;)V");

    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); log_str("rp6usb: GetMethodID failed"); return 0; }
    g_ready = 1;
    return 1;
}

// find_midi_iface locates the device's first MIDI-streaming interface and its
// bulk IN / OUT endpoints. Returns the interface (local ref) with *inEp/*outEp
// set to the endpoints found (local refs, or NULL), or NULL if the device has
// no MIDI interface. Endpoints the caller doesn't keep must be freed by it.
static jobject find_midi_iface(JNIEnv* env, jobject dev, jobject* inEp, jobject* outEp) {
    *inEp = NULL;
    *outEp = NULL;
    jint nif = (*env)->CallIntMethod(env, dev, m_ifCount);
    for (jint i = 0; i < nif; i++) {
        jobject intf = (*env)->CallObjectMethod(env, dev, m_getIf, i);
        if (intf == NULL) continue;
        jint cls = (*env)->CallIntMethod(env, intf, m_ifClass);
        jint sub = (*env)->CallIntMethod(env, intf, m_ifSub);
        if (cls == USB_CLASS_AUDIO && sub == USB_SUBCLASS_MIDI) {
            jint nep = (*env)->CallIntMethod(env, intf, m_epCount);
            for (jint e = 0; e < nep; e++) {
                jobject ep = (*env)->CallObjectMethod(env, intf, m_getEp, e);
                if (ep == NULL) continue;
                jint dir  = (*env)->CallIntMethod(env, ep, m_epDir);
                jint type = (*env)->CallIntMethod(env, ep, m_epType);
                if (type == USB_XFER_BULK && dir == USB_DIR_IN && *inEp == NULL) {
                    *inEp = ep;
                } else if (type == USB_XFER_BULK && dir == 0 && *outEp == NULL) {
                    *outEp = ep; // dir 0 == USB_DIR_OUT
                } else {
                    (*env)->DeleteLocalRef(env, ep);
                }
            }
            return intf;
        }
        (*env)->DeleteLocalRef(env, intf);
    }
    return NULL;
}

// rp6_usb_send transmits raw USB-MIDI event packets to the active device's
// bulk-OUT endpoint. Called from Go's OutputPort.Send on whatever goroutine
// fired the MIDI (attaches that thread to the JVM on first use). No-op when no
// output device is connected.
void rp6_usb_send(unsigned char* data, int n) {
    pthread_mutex_lock(&g_out_mu);
    if (g_conn == NULL || g_out_ep == NULL) { pthread_mutex_unlock(&g_out_mu); return; }
    JNIEnv* env = NULL;
    jint st = (*g_vm)->GetEnv(g_vm, (void**)&env, JNI_VERSION_1_6);
    if (st == JNI_EDETACHED) {
        if ((*g_vm)->AttachCurrentThread(g_vm, &env, NULL) != JNI_OK) {
            pthread_mutex_unlock(&g_out_mu);
            return;
        }
    }
    jbyteArray arr = (*env)->NewByteArray(env, n);
    if (arr != NULL) {
        (*env)->SetByteArrayRegion(env, arr, 0, n, (jbyte*)data);
        (*env)->CallIntMethod(env, g_conn, m_bulk, g_out_ep, arr, n, 50);
        (*env)->DeleteLocalRef(env, arr);
        (*env)->ExceptionClear(env);
    }
    pthread_mutex_unlock(&g_out_mu);
}

// request_permission builds a PendingIntent and asks for USB permission. The
// grant returns asynchronously; the caller polls hasPermission on later scans.
static void request_permission(JNIEnv* env, jobject dev) {
    jstring action = (*env)->NewStringUTF(env, "com.mono4loop.rp6.USB_PERMISSION");
    jobject intent = (*env)->NewObject(env, c_intent, m_intentInit, action);
    jobject pi = (*env)->CallStaticObjectMethod(env, c_pi, m_piGetBroadcast, g_ctx, 0, intent, PI_FLAG_IMMUTABLE);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); log_str("rp6usb: PendingIntent failed"); return; }
    (*env)->CallVoidMethod(env, g_usbmgr, m_requestPermission, dev, pi);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); log_str("rp6usb: requestPermission threw"); }
    log_str("rp6usb: requested USB permission (grant it, then it connects)");
}

// read_device opens the device and pumps its bulk-IN MIDI into Go until the
// device disappears. If the device also has a bulk-OUT endpoint (a P-6), it is
// published for output too. Blocks for the life of the connection.
static void read_device(JNIEnv* env, jobject dev, char* id, char* name) {
    jobject inEp = NULL, outEp = NULL;
    jobject intf = find_midi_iface(env, dev, &inEp, &outEp);
    if (intf == NULL || inEp == NULL) { log_str("rp6usb: no bulk-IN MIDI endpoint"); return; }

    jobject conn = (*env)->CallObjectMethod(env, g_usbmgr, m_openDevice, dev);
    if (conn == NULL) { log_str("rp6usb: openDevice returned null"); return; }
    if (!(*env)->CallBooleanMethod(env, conn, m_claim, intf, JNI_TRUE)) {
        log_str("rp6usb: claimInterface failed");
        (*env)->CallVoidMethod(env, conn, m_close);
        return;
    }

    int hasOut = (outEp != NULL);
    if (hasOut) {
        pthread_mutex_lock(&g_out_mu);
        g_conn   = (*env)->NewGlobalRef(env, conn);
        g_out_ep = (*env)->NewGlobalRef(env, outEp);
        pthread_mutex_unlock(&g_out_mu);
    }

    goUSBDevice(id, name ? name : id, 1, hasOut);
    if (hasOut) goUSBOutputReady(id); // let Go register the bridge OutputPort
    log_str("rp6usb: reading MIDI");

    const int bufLen = 512;
    jbyteArray arr = (*env)->NewByteArray(env, bufLen);
    unsigned char cbuf[512];
    int idleScans = 0;

    for (;;) {
        // 100ms timeout: bulkTransfer returns <0 when nothing arrived.
        jint n = (*env)->CallIntMethod(env, conn, m_bulk, inEp, arr, bufLen, 100);
        if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); break; }
        if (n > 0) {
            idleScans = 0;
            (*env)->GetByteArrayRegion(env, arr, 0, n, (jbyte*)cbuf);
            goUSBData(id, cbuf, (int)n);
            continue;
        }
        // No data. Every ~5s, verify the device is still attached (hasPermission
        // throws/false once it's gone) so we notice an unplug and rescan.
        if (++idleScans >= 50) {
            idleScans = 0;
            jboolean ok = (*env)->CallBooleanMethod(env, g_usbmgr, m_hasPermission, dev);
            if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); break; }
            if (!ok) break;
        }
    }

    // Tear down output first (so a concurrent send stops touching this conn),
    // then release + close.
    pthread_mutex_lock(&g_out_mu);
    if (g_out_ep) { (*env)->DeleteGlobalRef(env, g_out_ep); g_out_ep = NULL; }
    if (g_conn)   { (*env)->DeleteGlobalRef(env, g_conn);   g_conn = NULL; }
    pthread_mutex_unlock(&g_out_mu);

    (*env)->DeleteLocalRef(env, arr);
    (*env)->CallBooleanMethod(env, conn, m_release, intf);
    (*env)->CallVoidMethod(env, conn, m_close);
    (*env)->ExceptionClear(env);
    goUSBRemove(id);
    log_str("rp6usb: device closed");
}

// scan_once looks for one MIDI-capable device. If it needs permission, it asks
// and returns; otherwise it reads it (blocking) until disconnect.
static void scan_once(JNIEnv* env) {
    jobject list = (*env)->CallObjectMethod(env, g_usbmgr, m_getDeviceList);
    if (list == NULL) return;
    jobject vals = (*env)->CallObjectMethod(env, list, m_values);
    jobject it = (*env)->CallObjectMethod(env, vals, m_iterator);

    while ((*env)->CallBooleanMethod(env, it, m_hasNext)) {
        jobject dev = (*env)->CallObjectMethod(env, it, m_next);
        if (dev == NULL) continue;

        jobject inEp = NULL, outEp = NULL;
        jobject intf = find_midi_iface(env, dev, &inEp, &outEp);
        int isMidi = (intf != NULL && inEp != NULL);
        if (inEp)  (*env)->DeleteLocalRef(env, inEp);
        if (outEp) (*env)->DeleteLocalRef(env, outEp);
        if (intf)  (*env)->DeleteLocalRef(env, intf);
        if (!isMidi) { (*env)->DeleteLocalRef(env, dev); continue; }

        jboolean granted = (*env)->CallBooleanMethod(env, g_usbmgr, m_hasPermission, dev);
        if (!granted) {
            request_permission(env, dev);
            (*env)->DeleteLocalRef(env, dev);
            break; // wait for the grant; picked up on the next scan
        }

        char* id   = jstr(env, (jstring)(*env)->CallObjectMethod(env, dev, m_devName));
        char* name = jstr(env, (jstring)(*env)->CallObjectMethod(env, dev, m_prodName));
        (*env)->ExceptionClear(env);
        read_device(env, dev, id ? id : (char*)"usbmidi", name);
        free(id);
        free(name);
        (*env)->DeleteLocalRef(env, dev);
        break; // one device at a time
    }
    (*env)->DeleteLocalRef(env, it);
    (*env)->DeleteLocalRef(env, vals);
    (*env)->DeleteLocalRef(env, list);
}

// rp6_usb_run attaches this thread to the JVM and scans forever. Call it from a
// dedicated, OS-locked goroutine after rp6_usb_setup.
void rp6_usb_run(void) {
    JNIEnv* env = NULL;
    if ((*g_vm)->AttachCurrentThread(g_vm, &env, NULL) != JNI_OK) {
        log_str("rp6usb: AttachCurrentThread failed");
        return;
    }
    if (!init_ids(env)) { (*g_vm)->DetachCurrentThread(g_vm); return; }

    // Resolve UsbManager once: ctx.getSystemService("usb").
    jstring svc = (*env)->NewStringUTF(env, "usb");
    jobject mgr = (*env)->CallObjectMethod(env, g_ctx, m_getService, svc);
    (*env)->DeleteLocalRef(env, svc);
    if (mgr == NULL) { log_str("rp6usb: no UsbManager"); (*g_vm)->DetachCurrentThread(g_vm); return; }
    g_usbmgr = (*env)->NewGlobalRef(env, mgr);
    (*env)->DeleteLocalRef(env, mgr);

    log_str("rp6usb: watching for USB-MIDI devices");
    for (;;) {
        (*env)->PushLocalFrame(env, 64);
        scan_once(env);
        (*env)->PopLocalFrame(env, NULL);
        (*env)->ExceptionClear(env);
        usleep(2 * 1000 * 1000); // 2s between scans
    }
}
*/
import "C"

import (
	"runtime"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

// usbOutPort implements midibridge.OutputPort for a USB device: it encodes the
// raw MIDI message to USB-MIDI packets and writes them to the device's bulk-OUT
// endpoint (rp6_usb_send attaches the calling goroutine's thread to the JVM).
type usbOutPort struct{ id string }

func (usbOutPort) Send(data []byte) error {
	pkts := EncodeUSBMIDI(data)
	if len(pkts) == 0 {
		return nil
	}
	C.rp6_usb_send((*C.uchar)(unsafe.Pointer(&pkts[0])), C.int(len(pkts)))
	return nil
}

// Start launches the Android USB-MIDI reader. It uses Fyne's driver.RunNative to
// grab the JVM + Context, then runs a background thread that watches for
// USB-MIDI devices (P-6, MacroPad, …), requests USB permission, and streams
// their MIDI into the midibridge package. A device with a bulk-OUT endpoint (a
// P-6) is also published for output, so the app can trigger its pads/CC/PC/
// clock. logf, if non-nil, receives progress and error lines (wire it to
// log.Printf / the status bar). Safe to call once at launch; it returns
// immediately.
func Start(logf func(string)) {
	setLogger(logf)
	go func() {
		// RunNative gives us a valid JNIEnv on its thread; capture the VM +
		// Context there (as global refs) so the reader thread can attach itself.
		err := driver.RunNative(func(ctx any) error {
			ac, ok := ctx.(*driver.AndroidContext)
			if !ok {
				return nil
			}
			C.rp6_usb_setup(C.uintptr_t(ac.VM), C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx))
			return nil
		})
		if err != nil {
			logMsg("rp6usb: RunNative failed: " + err.Error())
			return
		}
		// The scan/read loop blocks forever on its own OS thread (it stays
		// attached to the JVM for its lifetime).
		runtime.LockOSThread()
		C.rp6_usb_run()
	}()
}
