package main

import (
	"fmt"
	"runtime/debug"

	ChariotContainer "github.com/imwux/chariot/container"
)

func main() {
	ChariotContainer.HostInit()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Chariot vUNKNOWN")
	} else {
		fmt.Printf("Chariot v%s\n", buildInfo.Main.Version)
	}
}
