package utils

import (
	"log"
	"strings"
)

const (
	Debug   = 3
	Test    = 2
	Release = 1
)

func ErrorHandler(e error, logMsg string, logLevel uint8, opt string) {
	if e == nil {
		return
	}
	if strings.Compare(opt, "warn") == 0 && logLevel > 1 {
		log.Println("Warning: error while "+logMsg, "\nError Code: ", e)
	}
	if strings.Compare(opt, "info") == 0 && logLevel > 2 {
		log.Println("INFO: " + logMsg)
	}
	if strings.Compare(opt, "error") == 0 {
		log.Println("Panic while " + logMsg)
		panic(e)
	}
}
