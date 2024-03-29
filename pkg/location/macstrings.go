//go:build darwin && !skipnative

package location

// #cgo CFLAGS: -x objective-c -Wno-incompatible-pointer-types
// #cgo LDFLAGS: -framework Foundation
// #include "macstrings.h"
import "C"
import "unsafe"

func NSArrayLength(arr *C.NSArray) uint {
	return uint(C.nsarray_length(unsafe.Pointer(arr)))
}

func NSArrayObjectAtIndex(arr *C.NSArray, idx uint) unsafe.Pointer {
	return C.nsarray_object_at_index(unsafe.Pointer(arr), C.ulong(idx))
}

func NSArrayNSStringToGoStringSlice(arr *C.NSArray) []string {
	res := []string{}
	for i := uint(0); i < NSArrayLength(arr); i++ {
		str := (*C.NSString)(NSArrayObjectAtIndex(arr, i))
		res = append(res, NSStringToGoString(str))
	}
	return res
}

func NSStringToGoString(str *C.NSString) string {
	return C.GoString(C.nsstring_to_charstar(unsafe.Pointer(str)))
}
