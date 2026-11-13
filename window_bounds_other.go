//go:build !windows

package main

import "github.com/egoist/mygo"

func selectionWindowBounds(w *mygo.Window) mygo.Rectangle { return w.Bounds() }
