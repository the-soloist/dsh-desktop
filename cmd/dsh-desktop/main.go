package main

import (
	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/desktop"
)

func main() {
	if _, err := dshdesktop.CurrentVersion(); err != nil {
		panic(err)
	}
	desktop.Main()
}
