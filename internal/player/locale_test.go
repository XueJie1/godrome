//go:build cgo

package player

import "testing"

func TestOpenNativeWithNonCNumericLocale(t *testing.T) {
	if !setNumericLocale("zh_CN.UTF-8") {
		t.Skip("zh_CN.UTF-8 is not installed")
	}
	defer setNumericLocale("C")

	n, err := openNative("null")
	if err != nil {
		t.Fatal(err)
	}
	n.close()
}
