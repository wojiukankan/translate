@main
struct GestureChecks {
    static func main() {
        CopyChecks.run()
        var shortPress = GestureState()
        let shortTimer = shortPress.begin()
        precondition(!shortPress.end(), "a short press must be replayed")
        precondition(!shortPress.fire(generation: shortTimer), "a released press cannot trigger")

        var longPress = GestureState()
        let longTimer = longPress.begin()
        precondition(longPress.fire(generation: longTimer), "a held press must trigger")
        precondition(!longPress.fire(generation: longTimer), "a hold must trigger only once")
        precondition(longPress.end(), "a long press must not be replayed")

        var successive = GestureState()
        let oldTimer = successive.begin()
        _ = successive.end()
        let newTimer = successive.begin()
        precondition(!successive.fire(generation: oldTimer), "an old timer cannot trigger a new press")
        precondition(successive.fire(generation: newTimer), "the current timer must trigger")
        precondition(successive.end())
    }
}
