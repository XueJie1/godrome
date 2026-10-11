//go:build cgo

package player

/*
#include <locale.h>
#include <stdlib.h>
*/
import "C"
import "unsafe"

func setNumericLocale(name string) bool {
	locale := C.CString(name)
	defer C.free(unsafe.Pointer(locale))
	return C.setlocale(C.LC_NUMERIC, locale) != nil
}
