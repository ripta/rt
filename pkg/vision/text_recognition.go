//go:build darwin && !skipnative

package vision

// #cgo CFLAGS: -I./include
// #cgo LDFLAGS: -L.build/debug/ -lvision
// #include <stdlib.h>
// #include "vision.h"
import "C"
import "unsafe"

func RecognizeText(img []byte) string {
	cimg := C.CBytes(img)
	defer C.free(cimg)

	cstr := C.recognizeText((*C.char)(cimg), C.int(len(img)))
	defer C.free(unsafe.Pointer(cstr))

	return C.GoString(cstr)
}
