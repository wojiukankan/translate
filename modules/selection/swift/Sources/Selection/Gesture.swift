// Tracks one right-button press. The caller schedules fire after 500 ms.
struct GestureState {
    private(set) var active = false
    private(set) var triggered = false
    private var generation = 0

    mutating func begin() -> Int {
        generation += 1
        active = true
        triggered = false
        return generation
    }

    mutating func fire(generation: Int) -> Bool {
        guard active, !triggered, self.generation == generation else { return false }
        triggered = true
        return true
    }

    // Returns true when the long press was already delivered.
    mutating func end() -> Bool {
        let result = triggered
        reset()
        return result
    }

    mutating func reset() {
        generation += 1
        active = false
        triggered = false
    }
}
