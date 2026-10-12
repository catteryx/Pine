//go:build darwin

package main

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
)

// The native file icon comes from NSWorkspace, called through the
// Objective-C runtime with purego, without cgo or a helper process.
var (
	iconMsgSend  uintptr
	iconPoolPush uintptr
	iconPoolPop  uintptr
)

func init() {
	libObjC, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return
	}
	if iconMsgSend, err = purego.Dlsym(libObjC, "objc_msgSend"); err != nil {
		iconMsgSend = 0
		return
	}
	iconPoolPush, _ = purego.Dlsym(libObjC, "objc_autoreleasePoolPush")
	iconPoolPop, _ = purego.Dlsym(libObjC, "objc_autoreleasePoolPop")
}

// iconSend sends a message whose arguments and result are integers or
// pointers.
func iconSend(obj objc.ID, selector string, args ...uintptr) objc.ID {
	var a [8]uintptr
	a[0], a[1] = uintptr(obj), uintptr(objc.RegisterName(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(iconMsgSend, a[:n+2]...)
	return objc.ID(r)
}

func iconInt(obj objc.ID, selector string) int { return int(iconSend(obj, selector)) }

func iconResponds(obj objc.ID, selector string) bool {
	return obj != 0 && byte(iconSend(obj, "respondsToSelector:", uintptr(objc.RegisterName(selector)))) != 0
}

func iconClass(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// iconString returns an autoreleased NSString for s.
func iconString(s string) objc.ID {
	b := unsafe.StringData(s)
	str := iconSend(iconClass("NSString"), "alloc")
	str = iconSend(str, "initWithBytes:length:encoding:", uintptr(unsafe.Pointer(b)), uintptr(len(s)), 4)
	runtime.KeepAlive(s)
	return iconSend(str, "autorelease")
}

// iconBytes copies the contents of an NSData.
func iconBytes(data objc.ID) []byte {
	if data == 0 {
		return nil
	}
	n := iconInt(data, "length")
	if n == 0 {
		return []byte{}
	}
	p := iconSend(data, "bytes")
	return append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)...)
}

// osFileIcon returns a file's icon as a bitmap, through NSWorkspace. It runs
// on the main thread.
func osFileIcon(path string) *ui.Bitmap {
	if iconMsgSend == 0 {
		return nil
	}
	var bm *ui.Bitmap
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if iconPoolPush != 0 {
		pool, _, _ := purego.SyscallN(iconPoolPush)
		defer purego.SyscallN(iconPoolPop, pool)
	}

	ws := iconSend(iconClass("NSWorkspace"), "sharedWorkspace")
	if ws == 0 {
		return nil
	}
	img := iconSend(ws, "iconForFile:", uintptr(iconString(path)))
	if img == 0 {
		return nil
	}

	// Prefer the 128px representation; its PNG is small.
	var data objc.ID
	reps := iconSend(img, "representations")
	for i, n := 0, iconInt(reps, "count"); i < n; i++ {
		r := iconSend(reps, "objectAtIndex:", uintptr(i))
		if iconInt(r, "pixelsWide") == 128 && iconResponds(r, "representationUsingType:properties:") {
			data = iconSend(r, "representationUsingType:properties:", 4, 0)
			break
		}
	}
	if data == 0 {
		tiff := iconSend(img, "TIFFRepresentation")
		rep := iconSend(iconClass("NSBitmapImageRep"), "imageRepWithData:", uintptr(tiff))
		data = iconSend(rep, "representationUsingType:properties:", 4, 0)
	}
	if data == 0 {
		return nil
	}
	if b, err := ui.DecodeBitmap(iconBytes(data)); err == nil {
		bm = b
	}
	return bm
}
