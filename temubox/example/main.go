package main

import (
	"context"
	"os"

	"golang.org/x/term"

	"github.com/sorins/tinyemu-go/images"
	"github.com/sorins/tinyemu-go/temubox"
)

func main() {
	e, err := temubox.NewEmulator(temubox.Config{
		ConsoleOut: os.Stdout,
		ConsoleIn:  os.Stdin,
		BIOS:       images.BIOS,
		RootFS:     images.MustXZDecompress(images.RootFSMinimal_XZ),
		Kernel:     images.MustXZDecompress(images.KernelRiscv64_XZ),
	})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	defer term.Restore(int(os.Stdin.Fd()), state)

	err = e.Run(ctx)
	if err != nil {
		panic(err)
	}
}
