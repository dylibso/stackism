package main

import (
	pdk "github.com/extism/go-pdk"
)

//export after_file_put
func AfterFilePut() int32 {
	s := pdk.InputString()

	var reversed string
	for _, r := range []rune(s) {
		reversed = string(r) + reversed
	}

	pdk.OutputString(reversed)

	return 0
}

func main() {}
