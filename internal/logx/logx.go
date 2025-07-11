package logx

import (
	"log"
	"os"
)

var debug = os.Getenv("GW_DEBUG") == "1"

// D = debug
func D(format string, v ...any) {
	if debug {
		log.Printf(format, v...)
	}
}

// I = info (always)
func I(format string, v ...any) {
	log.Printf(format, v...)
}
