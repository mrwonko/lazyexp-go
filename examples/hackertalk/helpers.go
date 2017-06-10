package main

import (
	"context"
	"os"
	"os/exec"
	"strings"

	lazyexp "github.com/mrwonko/lazyexp-go"
)

func ensureExtension(filename, extension string) string {
	if len(filename) < len(extension) || strings.ToLower(filename[len(filename)-len(extension):]) != strings.ToLower(extension) {
		return filename + extension
	}
	return filename
}

func writeGraph(n lazyexp.Node, filename string) error {
	filename = ensureExtension(filename, ".png")
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "dot", "-Tpng")
	cmd.Stdout = f

	w, err := cmd.StdinPipe()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}

	err = cmd.Start()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}

	err = lazyexp.Dot(w, lazyexp.Flatten(n))
	if err != nil {
		cancel()
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}

	err = w.Close()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}

	err = cmd.Wait()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}

	err = f.Close()
	if err != nil {
		_ = os.Remove(filename)
		return err
	}
	return nil
}

func writeTimeline(n lazyexp.Node, filename string) error {
	filename = ensureExtension(filename, ".html")
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	err = lazyexp.Timeline(f, lazyexp.Flatten(n))
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
