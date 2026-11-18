//go:build darwin

package main

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/monoedge-labs/zmail/internal/mailbox"
)

const (
	nsUTF8StringEncoding   = 4
	kvStoreEntitlementKey  = "com.apple.developer.ubiquity-kvstore-identifier"
	kvStoreChangedNotifSym = "NSUbiquitousKeyValueStoreDidChangeExternallyNotification"
)

var kvsRuntime struct {
	once          sync.Once
	ok            bool
	libFoundation uintptr
	msgSendAddr   uintptr
	poolPushFn    uintptr
	poolPopFn     uintptr
	cfRelease     func(uintptr)
	secTaskSelf   func(uintptr) uintptr
	secTaskCopy   func(uintptr, objc.ID, uintptr) objc.ID
}

var (
	kvsSelMu sync.RWMutex
	kvsSels  = map[string]objc.SEL{}

	kvsObsOnce sync.Once
	kvsObsInst objc.ID
	kvsObsMu   sync.Mutex
	kvsObsCBs  []func()
)

func loadKVSRuntime() bool {
	kvsRuntime.once.Do(func() {
		libObjC, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		libCF, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		libFoundation, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		libSecurity, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			return
		}
		msgSend, err := purego.Dlsym(libObjC, "objc_msgSend")
		if err != nil {
			return
		}
		poolPush, err := purego.Dlsym(libObjC, "objc_autoreleasePoolPush")
		if err != nil {
			return
		}
		poolPop, err := purego.Dlsym(libObjC, "objc_autoreleasePoolPop")
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&kvsRuntime.cfRelease, libCF, "CFRelease")
		purego.RegisterLibFunc(&kvsRuntime.secTaskSelf, libSecurity, "SecTaskCreateFromSelf")
		purego.RegisterLibFunc(&kvsRuntime.secTaskCopy, libSecurity, "SecTaskCopyValueForEntitlement")
		kvsRuntime.libFoundation = libFoundation
		kvsRuntime.msgSendAddr = msgSend
		kvsRuntime.poolPushFn = poolPush
		kvsRuntime.poolPopFn = poolPop
		kvsRuntime.ok = true
	})
	return kvsRuntime.ok
}

func kvsSel(name string) objc.SEL {
	kvsSelMu.RLock()
	s, ok := kvsSels[name]
	kvsSelMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	kvsSelMu.Lock()
	kvsSels[name] = s
	kvsSelMu.Unlock()
	return s
}

//go:uintptrescapes
func kvsSend(obj objc.ID, selector string, args ...uintptr) objc.ID {
	var a [8]uintptr
	a[0], a[1] = uintptr(obj), uintptr(kvsSel(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(kvsRuntime.msgSendAddr, a[:n+2]...)
	return objc.ID(r)
}

//go:uintptrescapes
func kvsSendBool(obj objc.ID, selector string, args ...uintptr) bool {
	return byte(kvsSend(obj, selector, args...)) != 0
}

func kvsWithPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(kvsRuntime.poolPushFn)
	defer purego.SyscallN(kvsRuntime.poolPopFn, pool)
	fn()
}

func kvsNSString(s string) objc.ID {
	var p unsafe.Pointer
	if len(s) > 0 {
		p = unsafe.Pointer(unsafe.StringData(s))
	}
	str := kvsSend(kvsSend(objc.ID(objc.GetClass("NSString")), "alloc"), "initWithBytes:length:encoding:",
		uintptr(p), uintptr(len(s)), nsUTF8StringEncoding)
	runtime.KeepAlive(s)
	return kvsSend(str, "autorelease")
}

func kvsGoString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	p := kvsSend(str, "UTF8String")
	if p == 0 {
		return ""
	}
	n := int(kvsSend(str, "lengthOfBytesUsingEncoding:", nsUTF8StringEncoding))
	if n <= 0 {
		return ""
	}
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
}

func hasUbiquityKVStoreEntitlement() bool {
	task := kvsRuntime.secTaskSelf(0)
	if task == 0 {
		return false
	}
	defer kvsRuntime.cfRelease(task)

	var ok bool
	kvsWithPool(func() {
		val := kvsRuntime.secTaskCopy(task, kvsNSString(kvStoreEntitlementKey), 0)
		if val == 0 {
			return
		}
		defer kvsRuntime.cfRelease(uintptr(val))
		if !kvsSendBool(val, "respondsToSelector:", uintptr(kvsSel("UTF8String"))) {
			return
		}
		ok = kvsGoString(val) != ""
	})
	return ok
}

type icloudKVS struct {
	store objc.ID
}

// openICloudKVS opens NSUbiquitousKeyValueStore when the app carries the
// ubiquity kvstore entitlement, and subscribes onChange to external changes.
func openICloudKVS(onChange func()) mailbox.KVStore {
	if !loadKVSRuntime() || !hasUbiquityKVStoreEntitlement() {
		return nil
	}
	cls := objc.GetClass("NSUbiquitousKeyValueStore")
	if cls == 0 {
		return nil
	}
	var store objc.ID
	kvsWithPool(func() {
		s := kvsSend(objc.ID(cls), "defaultStore")
		if s != 0 {
			store = kvsSend(s, "retain")
		}
	})
	if store == 0 {
		return nil
	}
	if onChange != nil {
		registerKVSObserver(store, onChange)
	}
	return &icloudKVS{store: store}
}

func registerKVSObserver(store objc.ID, onChange func()) {
	kvsObsMu.Lock()
	kvsObsCBs = append(kvsObsCBs, onChange)
	kvsObsMu.Unlock()

	kvsObsOnce.Do(func() {
		kvsWithPool(func() {
			cls, err := objc.RegisterClass("ZmailKVSObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
				{
					Cmd: kvsSel("kvsDidChange:"),
					Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
						kvsObsMu.Lock()
						cbs := append([]func(){}, kvsObsCBs...)
						kvsObsMu.Unlock()
						for _, cb := range cbs {
							go cb()
						}
					},
				},
			})
			if err != nil {
				return
			}
			obs := kvsSend(kvsSend(objc.ID(cls), "alloc"), "init")
			if obs == 0 {
				return
			}
			kvsObsInst = obs

			notifName := kvsNSString(kvStoreChangedNotifSym)
			if sym, err := purego.Dlsym(kvsRuntime.libFoundation, kvStoreChangedNotifSym); err == nil {
				if val := **(**objc.ID)(unsafe.Pointer(&sym)); val != 0 {
					notifName = val
				}
			}
			center := kvsSend(objc.ID(objc.GetClass("NSNotificationCenter")), "defaultCenter")
			kvsSend(center, "addObserver:selector:name:object:",
				uintptr(obs), uintptr(kvsSel("kvsDidChange:")), uintptr(notifName), uintptr(store))
		})
	})
}

func (k *icloudKVS) Get(key string) (string, bool) {
	var val string
	var ok bool
	kvsWithPool(func() {
		s := kvsSend(k.store, "stringForKey:", uintptr(kvsNSString(key)))
		if s == 0 {
			return
		}
		val, ok = kvsGoString(s), true
	})
	return val, ok
}

func (k *icloudKVS) Set(key, value string) error {
	kvsWithPool(func() {
		kvsSend(k.store, "setString:forKey:", uintptr(kvsNSString(value)), uintptr(kvsNSString(key)))
	})
	return nil
}

func (k *icloudKVS) Synchronize() bool {
	var ok bool
	kvsWithPool(func() {
		ok = kvsSendBool(k.store, "synchronize")
	})
	return ok
}
