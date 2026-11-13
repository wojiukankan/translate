package selectionmonitor

import (
	"reflect"
	"testing"
)

func TestGesture(t *testing.T) {
	t.Run("short press replays down, drag and release", func(t *testing.T) {
		var g gesture
		generation := g.begin(point{10, 20})
		if !g.move(point{11, 21}) {
			t.Fatal("active drag was not intercepted")
		}
		intercepted, replay := g.end(point{12, 22})
		want := []point{{10, 20}, {11, 21}, {12, 22}}
		if !intercepted || !reflect.DeepEqual(replay, want) {
			t.Fatalf("release = %v, replay = %v; want %v", intercepted, replay, want)
		}
		if g.fire(generation) {
			t.Fatal("released press triggered its timer")
		}
	})
	t.Run("long press fires once and does not replay", func(t *testing.T) {
		var g gesture
		generation := g.begin(point{-5, 12})
		if !g.fire(generation) || g.fire(generation) {
			t.Fatal("hold should fire exactly once")
		}
		if !g.move(point{3, 14}) {
			t.Fatal("long-press drag was not intercepted")
		}
		intercepted, replay := g.end(point{4, 15})
		if !intercepted || len(replay) != 0 {
			t.Fatalf("long-press release = %v, replay = %v", intercepted, replay)
		}
	})
	t.Run("old timers and idle events have no effect", func(t *testing.T) {
		var g gesture
		if g.move(point{}) {
			t.Fatal("idle move was intercepted")
		}
		if intercepted, _ := g.end(point{}); intercepted {
			t.Fatal("idle release was intercepted")
		}
		old := g.begin(point{1, 1})
		g.end(point{1, 1})
		current := g.begin(point{2, 2})
		if g.fire(old) || !g.fire(current) {
			t.Fatal("old timer affected the next press")
		}
	})
}
